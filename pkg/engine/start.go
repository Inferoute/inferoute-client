package engine

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const (
	// DefaultStartTimeout is how long EnsureReady waits when weights should already be local.
	DefaultStartTimeout = 10 * time.Minute
	// DefaultDownloadTimeout is how long setup waits for first-run HuggingFace / Ollama fetch + load.
	DefaultDownloadTimeout = 2 * time.Hour
)

// LogPath is where detached engine stdout/stderr go.
func LogPath(logDir string) string {
	if logDir == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			logDir = filepath.Join(home, ".local", "state", "inferoute", "log")
		}
	}
	return filepath.Join(logDir, "engine.log")
}

// StartDetached launches spec so it survives inferoute-client exit.
// The returned channel receives the process exit error (or nil) once.
func StartDetached(spec Spec, logPath string) (<-chan error, error) {
	if spec.Bin == "" {
		return nil, fmt.Errorf("engine binary is empty")
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return nil, fmt.Errorf("create engine log directory: %w", err)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open engine log: %w", err)
	}
	cmd := exec.Command(spec.Bin, spec.Args...)
	cmd.Stdin = nil
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	detach(cmd)
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return nil, fmt.Errorf("start %s: %w", spec.CommandLine(), err)
	}
	_ = logFile.Close()
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	return exited, nil
}

// Run runs spec in the foreground, streaming output to w.
func Run(ctx context.Context, spec Spec, w io.Writer) error {
	if spec.Bin == "" {
		return fmt.Errorf("engine binary is empty")
	}
	cmd := exec.CommandContext(ctx, spec.Bin, spec.Args...)
	cmd.Stdout = w
	cmd.Stderr = w
	return cmd.Run()
}
