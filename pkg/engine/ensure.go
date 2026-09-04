package engine

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/internal/config"
)

// EnsureReady starts the configured engine if auto_start is on and llm_url is down.
// The process is detached and left running when inferoute-client exits.
func EnsureReady(ctx context.Context, cfg *config.Config, logDir string) error {
	if cfg == nil || !cfg.Provider.AutoStart {
		return nil
	}
	kind, ok := ParseKind(cfg.Provider.Engine)
	if !ok {
		return fmt.Errorf("unknown engine %q", cfg.Provider.Engine)
	}
	url := cfg.Provider.LLMURL
	if Healthy(ctx, kind, url) {
		return nil
	}

	bin := cfg.Provider.EngineBin
	if bin == "" {
		d := Detect(kind)
		if !d.Found {
			return fmt.Errorf("%s is not installed and is not running at %s", kind, url)
		}
		bin = d.Bin
	}

	hfRepo := cfg.Provider.Model
	spec := ServeSpec(kind, bin, cfg.Provider.Model, hfRepo)
	if err := StartDetached(spec, LogPath(logDir)); err != nil {
		return err
	}

	wait := DefaultStartTimeout
	if deadline, ok := ctx.Deadline(); ok {
		wait = time.Until(deadline)
	}
	waitCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	probeURL := url
	if probeURL == "" {
		probeURL = spec.URL
	}
	if err := WaitHealthy(waitCtx, kind, probeURL, 2*time.Second); err != nil {
		fmt.Fprintf(os.Stderr, "engine started but not ready yet; check %s\n", LogPath(logDir))
		return err
	}
	return nil
}
