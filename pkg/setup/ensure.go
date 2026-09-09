package setup

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/internal/config"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/engine"
)

// EnsureOnStart checks the configured inference engine before the dashboard.
// Interactive TTYs prompt to start it when it is down; otherwise auto_start
// decides. A spinner stays on stdout until the engine is serving.
func EnsureOnStart(cfg *config.Config, io Streams) error {
	ctx, cancel := context.WithTimeout(context.Background(), engine.DefaultDownloadTimeout)
	defer cancel()
	return ensureOnStart(ctx, cfg, io, interactiveReader(io.In))
}

func interactiveReader(in io.Reader) bool {
	f, ok := in.(*os.File)
	return ok && isTTY(f)
}

func ensureOnStart(ctx context.Context, cfg *config.Config, io Streams, interactive bool) error {
	if cfg == nil {
		return nil
	}
	if io.Out == nil {
		io.Out = os.Stdout
	}
	if io.Err == nil {
		io.Err = os.Stderr
	}
	if io.In == nil {
		io.In = os.Stdin
	}

	kind, ok := engine.ParseKind(cfg.Provider.Engine)
	if !ok {
		return nil
	}
	label := engine.Label(kind)
	url := cfg.Provider.LLMURL
	if url == "" {
		url = engine.DefaultURL(kind)
	}

	if engine.Healthy(ctx, kind, url) {
		fmt.Fprintf(io.Out, "%s is running at %s.\n", label, url)
		return nil
	}

	waitMsg := fmt.Sprintf("Waiting for %s to start serving at %s", label, url)
	if engine.PortOpen(ctx, url) {
		fmt.Fprintf(io.Out, "%s is already starting at %s.\n", label, url)
		err := SpinWhile(io.Out, waitMsg, func() error {
			return engine.WaitHealthy(ctx, kind, url, 2*time.Second, nil)
		})
		if err != nil {
			return fmt.Errorf("%w (see %s)", err, engine.LogPath(cfg.Logging.LogDir))
		}
		fmt.Fprintf(io.Out, "%s is ready at %s.\n", label, url)
		return nil
	}

	bin := engine.ResolveBin(kind, cfg.Provider.EngineBin)
	if bin == "" {
		detected := engine.Detect(kind)
		if detected.Unusable != "" {
			fmt.Fprintf(io.Out, "%s at %s cannot run (%s).\n", label, detected.Bin, detected.Unusable)
			fmt.Fprintln(io.Out, "Fix it with: inferoute-client setup")
			return nil
		}
		fmt.Fprintf(io.Out, "%s is not installed and is not running at %s.\n", label, url)
		fmt.Fprintln(io.Out, "Install it with: inferoute-client setup")
		return nil
	}

	startNow := cfg.Provider.AutoStart
	if interactive {
		ok, err := promptYes(io.In, io.Out, fmt.Sprintf("%s is not running at %s. Start it now?", label, url), true)
		if err != nil {
			return err
		}
		startNow = ok
	}
	if !startNow {
		fmt.Fprintf(io.Out, "Skipping %s start. Inference will fail until it is serving at %s.\n", label, url)
		return nil
	}

	err := SpinWhile(io.Out, waitMsg, func() error {
		return engine.StartAndWait(ctx, cfg, cfg.Logging.LogDir)
	})
	if err != nil {
		return fmt.Errorf("%w (see %s)", err, engine.LogPath(cfg.Logging.LogDir))
	}
	fmt.Fprintf(io.Out, "%s is ready at %s.\n", label, url)
	return nil
}
