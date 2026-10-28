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
	return StartAndWait(ctx, cfg, logDir)
}

// StartAndWait starts the configured engine if llm_url is down and waits until it is serving.
// A multi-node master returns once its torch store is accepting other ranks.
func StartAndWait(ctx context.Context, cfg *config.Config, logDir string) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	kind, ok := ParseKind(cfg.Provider.Engine)
	if !ok {
		return fmt.Errorf("unknown engine %q", cfg.Provider.Engine)
	}
	url := cfg.Provider.LLMURL
	if Healthy(ctx, kind, url) {
		return nil
	}
	if PortOpen(ctx, url) {
		if err := WaitHealthy(ctx, kind, url, 2*time.Second, nil); err != nil {
			fmt.Fprintf(os.Stderr, "engine already bound at %s but not ready yet; check %s\n", url, LogPath(logDir))
			return err
		}
		return nil
	}
	// Rank 0 binds the torch store and blocks until the other nodes connect.
	// /v1/models does not come up before that, so an open store is the ready signal.
	join := MasterJoinAddr(cfg.Provider.EngineArgs)
	if join != "" && PortOpen(ctx, "http://"+join) {
		return nil
	}

	bin := ResolveBin(kind, cfg.Provider.EngineBin)
	if bin == "" {
		d := Detect(kind)
		if d.Unusable != "" {
			return fmt.Errorf("%s cannot run (%s); re-run inferoute-client setup", Label(kind), d.Unusable)
		}
		return fmt.Errorf("%s is not installed and is not running at %s", Label(kind), url)
	}

	hfRepo := firstNonEmpty(cfg.Provider.HFRepo, cfg.Provider.Model)
	opts := ServeOpts{
		ToolCallParser:     cfg.Provider.ToolCallParser,
		MaxModelLen:        cfg.Provider.MaxModelLen,
		RopeType:           cfg.Provider.RopeType,
		RopeBaseContextLen: cfg.Provider.RopeBaseContextLen,
		GGUFFile:           cfg.Provider.GGUFFile,
	}
	spec := ServeSpec(kind, bin, cfg.Provider.Model, hfRepo, opts)
	if len(cfg.Provider.EngineArgs) > 0 {
		spec.Args = cfg.Provider.EngineArgs
		spec.Env = cfg.Provider.EngineEnv
		if cfg.Provider.LLMURL != "" {
			spec.URL = cfg.Provider.LLMURL
		}
	}
	exited, err := StartDetached(spec, LogPath(logDir))
	if err != nil {
		return err
	}

	probeURL := url
	if probeURL == "" {
		probeURL = spec.URL
	}
	var errWait error
	if join != "" {
		errWait = WaitListen(ctx, join, 2*time.Second, exited)
	} else {
		errWait = WaitHealthy(ctx, kind, probeURL, 2*time.Second, exited)
	}
	if errWait != nil {
		fmt.Fprintf(os.Stderr, "engine started but not ready yet; check %s\n", LogPath(logDir))
		return errWait
	}
	return nil
}
