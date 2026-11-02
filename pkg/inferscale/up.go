package inferscale

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// WithWait shows progress around a slow step. Setup replaces it with the spinner.
var WithWait = func(msg string, fn func() error) error {
	fmt.Fprintf(os.Stdout, "%s...\n", msg)
	return fn()
}

// BringUp installs the pinned binaries, starts inferscaled, and joins the tailnet.
// authKey is empty when this machine already joined.
func BringUp(ctx context.Context, loginServer, authKey string) (ip, nodeID string, err error) {
	if runtime.GOOS != "linux" {
		return "", "", fmt.Errorf("inferscale needs a Linux TUN; this machine is %s", runtime.GOOS)
	}
	var cli string
	if err := WithWait("Downloading inferscale", func() error {
		var derr error
		cli, derr = ensureBins(ctx)
		return derr
	}); err != nil {
		return "", "", err
	}
	if err := daemonReady(ctx, cli); err != nil {
		if err := ensureSudo(ctx); err != nil {
			return "", "", err
		}
		if err := WithWait("Starting inferscale", func() error {
			return ensureDaemon(ctx, cli)
		}); err != nil {
			return "", "", err
		}
	}
	// A saved address is not a connection. Offline still has the old IPv4.
	if st, err := readState(ctx, cli); err == nil && st.Online && st.IP != "" {
		fmt.Fprintf(os.Stdout, "Inferscale address: %s\n", st.IP)
		return st.IP, st.ID, nil
	}
	if loginServer == "" && authKey == "" {
		return "", "", fmt.Errorf("inferscale is not connected and the one-shot key was already used")
	}
	if err := ensureSudo(ctx); err != nil {
		return "", "", err
	}
	cmd := commandAsRoot(ctx, cli, upArgs(loginServer, authKey)...)
	if err := WithWait("Joining the tailnet", func() error {
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("inferscale up: %w", err)
		}
		return nil
	}); err != nil {
		return "", "", err
	}
	ip, nodeID, err = waitOnline(ctx, cli)
	if err == nil {
		fmt.Fprintf(os.Stdout, "Inferscale address: %s\n", ip)
	}
	return ip, nodeID, err
}

func upArgs(loginServer, authKey string) []string {
	args := []string{"up"}
	if loginServer != "" {
		args = append(args, "--login-server", loginServer)
	}
	if authKey != "" {
		args = append(args, "--auth-key", authKey)
	}
	return args
}

const TUN = "inferscale0"

// rootArgv is the argv that runs name as root. euid 0 runs it directly.
// Otherwise sudo -n, which uses the credential from ensureSudo.
func rootArgv(euid int, name string, args ...string) (string, []string) {
	if euid == 0 {
		return name, args
	}
	return "sudo", append([]string{"-n", "--", name}, args...)
}

func commandAsRoot(ctx context.Context, name string, args ...string) *exec.Cmd {
	bin, argv := rootArgv(euid(), name, args...)
	cmd := exec.CommandContext(ctx, bin, argv...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}

func startDaemon(ctx context.Context, daemon string) error {
	logf := openDaemonLog()
	defer logf.Close()
	if euid() == 0 {
		cmd := exec.Command(daemon)
		cmd.Stdout = logf
		cmd.Stderr = logf
		detach(cmd)
		return cmd.Start()
	}
	// -b asks for nothing (sudo -n) and backgrounds inferscaled after auth.
	cmd := exec.CommandContext(ctx, "sudo", "-n", "-b", "--", daemon)
	cmd.Stdin = os.Stdin
	cmd.Stdout = logf
	cmd.Stderr = logf
	return cmd.Run()
}

func daemonLogPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "inferoute", "log", "inferscaled.log")
}

func openDaemonLog() io.WriteCloser {
	path := daemonLogPath()
	if path == "" || os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return nopWriteCloser{io.Discard}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nopWriteCloser{io.Discard}
	}
	return f
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

func binDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".local", "share", "inferoute", "inferscale")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func ensureBins(ctx context.Context) (string, error) {
	cliName, daemonName, err := hostAsset()
	if err != nil {
		return "", err
	}
	dir, err := binDir()
	if err != nil {
		return "", err
	}
	cli := filepath.Join(dir, "inferscale")
	daemon := filepath.Join(dir, "inferscaled")
	if err := download(ctx, downloadURL(cliName), cli); err != nil {
		return "", err
	}
	if daemonName != "" {
		if err := download(ctx, downloadURL(daemonName), daemon); err != nil {
			return "", err
		}
	}
	return cli, nil
}

func download(ctx context.Context, url, dest string) error {
	if st, err := os.Stat(dest); err == nil && st.Size() > 0 {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", url, res.Status)
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(f, res.Body); err != nil {
		return err
	}
	return nil
}

func ensureDaemon(ctx context.Context, cli string) error {
	if daemonReady(ctx, cli) == nil {
		return nil
	}
	daemon := filepath.Join(filepath.Dir(cli), "inferscaled")
	if err := startDaemon(ctx, daemon); err != nil {
		return fmt.Errorf("start inferscaled: %w", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		if err := daemonReady(ctx, cli); err == nil {
			return nil
		} else {
			last = err
		}
		time.Sleep(200 * time.Millisecond)
	}
	msg := "inferscaled did not answer"
	if tail := tailFile(daemonLogPath(), 800); tail != "" {
		msg += ": " + tail
	} else if last != nil {
		msg += ": " + last.Error()
	}
	return fmt.Errorf("%s", msg)
}

// daemonReady is true once status returns JSON. An address shows up only after up.
func daemonReady(ctx context.Context, cli string) error {
	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, cli, "status", "--json").Output()
	if statusAnswered(out) {
		return nil
	}
	if err == nil {
		return fmt.Errorf("empty status")
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && len(exit.Stderr) > 0 {
		return fmt.Errorf("%s", strings.TrimSpace(string(exit.Stderr)))
	}
	return err
}

func statusAnswered(out []byte) bool {
	out = bytes.TrimSpace(out)
	return len(out) > 0 && json.Valid(out)
}

func tailFile(path string, n int) string {
	b, err := os.ReadFile(path)
	if err != nil || len(bytes.TrimSpace(b)) == 0 {
		return ""
	}
	b = bytes.TrimSpace(b)
	if len(b) > n {
		b = b[len(b)-n:]
	}
	return string(b)
}

type nodeState struct {
	IP     string
	ID     string
	Online bool
}

// Inspect reads the IPv4 address and the Headscale node id.
func Inspect(ctx context.Context, cli string) (ip, nodeID string, err error) {
	st, err := readState(ctx, cli)
	if err != nil {
		return "", "", err
	}
	if st.IP == "" {
		return "", st.ID, fmt.Errorf("inferscale has no ipv4")
	}
	return st.IP, st.ID, nil
}

func readState(ctx context.Context, cli string) (nodeState, error) {
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, cli, "status", "--json").Output()
	if err != nil {
		ip, ipErr := ipv4(ctx, cli)
		return nodeState{IP: ip}, ipErr
	}
	st, err := parseStatus(out)
	if err != nil {
		ip, ipErr := ipv4(ctx, cli)
		return nodeState{IP: ip}, ipErr
	}
	return st, nil
}

func parseStatus(out []byte) (nodeState, error) {
	var st struct {
		Self struct {
			ID           string   `json:"ID"`
			Online       bool     `json:"Online"`
			TailscaleIPs []string `json:"TailscaleIPs"`
		} `json:"Self"`
	}
	if err := json.Unmarshal(out, &st); err != nil {
		return nodeState{}, err
	}
	parsed := nodeState{ID: st.Self.ID, Online: st.Self.Online}
	for _, addr := range st.Self.TailscaleIPs {
		if strings.Contains(addr, ".") {
			parsed.IP = addr
			break
		}
	}
	return parsed, nil
}

// waitOnline polls until the daemon is connected to headscale.
// up can return while Self.Online is still false.
func waitOnline(ctx context.Context, cli string) (string, string, error) {
	deadline := time.Now().Add(15 * time.Second)
	var last error
	for {
		st, err := readState(ctx, cli)
		if err == nil && st.Online && st.IP != "" {
			return st.IP, st.ID, nil
		}
		if err != nil {
			last = err
		} else {
			last = fmt.Errorf("inferscale is offline")
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			if ctx.Err() != nil {
				return "", "", ctx.Err()
			}
			return "", "", last
		}
		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// CLI is the inferscale binary on PATH or the one setup downloaded.
func CLI() (string, error) {
	if path, err := exec.LookPath("inferscale"); err == nil {
		return path, nil
	}
	dir, err := binDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "inferscale")
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	return path, nil
}

func ipv4(ctx context.Context, cli string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, cli, "ip", "-4").Output()
	if err != nil {
		return "", err
	}
	ip := strings.TrimSpace(string(out))
	if ip == "" {
		return "", fmt.Errorf("inferscale has no ipv4")
	}
	return ip, nil
}

// LocalStatus is the tailnet address and a "N online" peer summary.
// A missing binary is an empty result.
func LocalStatus() (ip, peers string) {
	ip, n := Tailnet()
	if n > 0 {
		peers = fmt.Sprintf("%d online", n)
	}
	return ip, peers
}

// Tailnet is this machine's tailnet IPv4 and how many other nodes are online.
func Tailnet() (ip string, onlinePeers int) {
	cli, err := exec.LookPath("inferscale")
	if err != nil {
		if dir, derr := binDir(); derr == nil {
			candidate := filepath.Join(dir, "inferscale")
			if _, serr := os.Stat(candidate); serr == nil {
				cli = candidate
			}
		}
	}
	if cli == "" {
		return "", 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, cli, "status", "--json").Output()
	if err != nil {
		ip, _ = ipv4(ctx, cli)
		return ip, 0
	}
	var st struct {
		Self struct {
			TailscaleIPs []string `json:"TailscaleIPs"`
		} `json:"Self"`
		Peer map[string]struct {
			Online bool `json:"Online"`
		} `json:"Peer"`
	}
	if json.Unmarshal(out, &st) != nil {
		ip, _ = ipv4(ctx, cli)
		return ip, 0
	}
	for _, addr := range st.Self.TailscaleIPs {
		if strings.Contains(addr, ".") {
			ip = addr
			break
		}
	}
	for _, p := range st.Peer {
		if p.Online {
			onlinePeers++
		}
	}
	return ip, onlinePeers
}
