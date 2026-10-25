package inferscale

import (
	"context"
	"encoding/json"
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

// BringUp installs the pinned binaries, starts inferscaled, and joins the tailnet.
// authKey is empty when this machine already joined.
func BringUp(ctx context.Context, loginServer, authKey string) (ip, nodeID string, err error) {
	if runtime.GOOS != "linux" {
		return "", "", fmt.Errorf("inferscale needs a Linux TUN; this machine is %s", runtime.GOOS)
	}
	if err := ensureSudo(ctx); err != nil {
		return "", "", err
	}
	cli, err := ensureBins(ctx)
	if err != nil {
		return "", "", err
	}
	if err := ensureDaemon(ctx, cli); err != nil {
		return "", "", err
	}
	if ip, nodeID, err = Inspect(ctx, cli); err == nil && ip != "" {
		return ip, nodeID, nil
	}
	if authKey == "" {
		return "", "", fmt.Errorf("inferscale is not up and the one-shot key was already used")
	}
	cmd := commandAsRoot(ctx, cli, "up", "--login-server", loginServer, "--auth-key", authKey)
	if err := cmd.Run(); err != nil {
		return "", "", fmt.Errorf("inferscale up: %w", err)
	}
	return Inspect(ctx, cli)
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
	if euid() == 0 {
		cmd := exec.Command(daemon)
		detach(cmd)
		return cmd.Start()
	}
	// -b asks for nothing (sudo -n) and backgrounds inferscaled after auth.
	cmd := exec.CommandContext(ctx, "sudo", "-n", "-b", "--", daemon)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

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
	if _, _, err := Inspect(ctx, cli); err == nil {
		return nil
	}
	daemon := filepath.Join(filepath.Dir(cli), "inferscaled")
	if err := startDaemon(ctx, daemon); err != nil {
		return fmt.Errorf("start inferscaled: %w", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, _, err := Inspect(ctx, cli); err == nil {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("inferscaled did not answer")
}

// Inspect reads the IPv4 address and the Headscale node id.
func Inspect(ctx context.Context, cli string) (ip, nodeID string, err error) {
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, cli, "status", "--json").Output()
	if err != nil {
		ip, err = ipv4(ctx, cli)
		return ip, "", err
	}
	var st struct {
		Self struct {
			ID           string   `json:"ID"`
			TailscaleIPs []string `json:"TailscaleIPs"`
		} `json:"Self"`
	}
	if err := json.Unmarshal(out, &st); err != nil {
		ip, err = ipv4(ctx, cli)
		return ip, "", err
	}
	for _, addr := range st.Self.TailscaleIPs {
		if strings.Contains(addr, ".") {
			return addr, st.Self.ID, nil
		}
	}
	return "", st.Self.ID, fmt.Errorf("inferscale has no ipv4")
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

// LocalStatus is the tailnet address and online peer count for the dashboard.
// A missing binary is an empty result.
func LocalStatus() (ip, peers string) {
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
		return "", ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, cli, "status", "--json").Output()
	if err != nil {
		ip, _ = ipv4(ctx, cli)
		return ip, ""
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
		return ip, ""
	}
	for _, addr := range st.Self.TailscaleIPs {
		if strings.Contains(addr, ".") {
			ip = addr
			break
		}
	}
	online := 0
	for _, p := range st.Peer {
		if p.Online {
			online++
		}
	}
	if online > 0 {
		peers = fmt.Sprintf("%d online", online)
	}
	return ip, peers
}
