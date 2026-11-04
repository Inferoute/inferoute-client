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
	// Exchange already spent the join code. The key has to be on disk before
	// download or sudo can fail and the next run passes "".
	// A leftover from that failed up is still a pending join: resumeWorker
	// and refreshCluster both call BringUp with "". Treating it as "already
	// joined" reuses the old tailnet and clearAuthKey deletes the unused key.
	supplied := strings.TrimSpace(authKey)
	if authKey, err = persistAuthKey(authKey); err != nil {
		return "", "", err
	}
	freshKey := pendingAuthKey(supplied, authKey)
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
	// A new key is a new cluster. Drop the old node so up does not resume
	// it or refuse with "can't change --login-server". logout uses sudo -n,
	// so cache sudo first — otherwise logout is a no-op on an already-running
	// daemon and the online shortcut below would delete the unused key.
	if freshKey {
		if err := ensureSudo(ctx); err != nil {
			return "", "", err
		}
		logout, lerr := commandAsRoot(ctx, cli, "logout")
		if lerr != nil {
			return "", "", lerr
		}
		_ = logout.Run()
	}
	// A saved address is not a connection. Offline still has the old IPv4.
	if st, err := readState(ctx, cli); err == nil && reuseOnlineNode(freshKey, st) {
		clearAuthKey()
		fmt.Fprintf(os.Stdout, "Inferscale address: %s\n", st.IP)
		return st.IP, st.ID, nil
	}
	if loginServer == "" && authKey == "" {
		return "", "", fmt.Errorf("inferscale is not connected and the one-shot key was already used")
	}
	if err := ensureSudo(ctx); err != nil {
		return "", "", err
	}
	keyFile := ""
	if authKey != "" {
		keyFile, err = authKeyPath()
		if err != nil {
			return "", "", err
		}
	}
	cmd, err := commandAsRoot(ctx, cli, upArgs(loginServer, keyFile)...)
	if err != nil {
		return "", "", err
	}
	// up's exit status does not say whether Headscale spent the key.
	// The rejection text lands in the daemon log and, when the CLI prints it, on stderr.
	logAt := fileSize(daemonLogPath())
	var stderr bytes.Buffer
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)
	if err := WithWait("Joining the tailnet", func() error {
		if err := cmd.Run(); err != nil {
			if authKeyRejected(stderr.String(), readFrom(daemonLogPath(), logAt)) {
				clearAuthKey()
				return fmt.Errorf("inferscale up: one-shot key was rejected: %w", err)
			}
			return fmt.Errorf("inferscale up: %w", err)
		}
		clearAuthKey()
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

// pendingAuthKey is true when this process still has a one-shot key to spend.
// That includes a leftover from a failed `up` when the caller passed "".
func pendingAuthKey(supplied, persisted string) bool {
	return strings.TrimSpace(supplied) != "" || strings.TrimSpace(persisted) != ""
}

// reuseOnlineNode is the already-joined shortcut. A pending key (caller-supplied
// or leftover) is a cluster switch: keep the key and run `up` instead of
// returning the old IP.
func reuseOnlineNode(freshKey bool, st nodeState) bool {
	return !freshKey && st.Online && st.IP != ""
}

// upArgs names the 0600 key file. file: keeps the one-shot key out of argv.
func upArgs(loginServer, authKeyPath string) []string {
	args := []string{"up"}
	if loginServer != "" {
		args = append(args, "--login-server", loginServer)
	}
	if authKeyPath != "" {
		args = append(args, "--auth-key", "file:"+authKeyPath)
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

func commandAsRoot(ctx context.Context, name string, args ...string) (*exec.Cmd, error) {
	if err := rootOwned(name); err != nil {
		return nil, fmt.Errorf("refusing to run %s as root: %w", name, err)
	}
	bin, argv := rootArgv(euid(), name, args...)
	cmd := exec.CommandContext(ctx, bin, argv...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd, nil
}

func startDaemon(ctx context.Context, daemon string) error {
	if err := rootOwned(daemon); err != nil {
		return fmt.Errorf("refusing to run %s as root: %w", daemon, err)
	}
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

func authKeyPath() (string, error) {
	dir, err := stateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "auth-key"), nil
}

// persistAuthKey stores a new one-shot key. An empty key is the saved one.
func persistAuthKey(authKey string) (string, error) {
	authKey = strings.TrimSpace(authKey)
	if authKey == "" {
		return loadAuthKey(), nil
	}
	path, err := authKeyPath()
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(authKey), 0o600); err != nil {
		return "", err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return "", err
	}
	return authKey, nil
}

func loadAuthKey() string {
	path, err := authKeyPath()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func clearAuthKey() {
	path, err := authKeyPath()
	if err != nil {
		return
	}
	os.Remove(path)
}

// authKeyRejected is true when Headscale refused this key. Other up failures
// may be before the key was spent, so the file has to stay.
func authKeyRejected(stderr, daemonLog string) bool {
	text := stderr + "\n" + daemonLog
	for _, s := range []string{
		"authkey already used",
		"authkey expired",
		"authkey revoked",
		"invalid authkey",
		"invalid pre auth key",
	} {
		if strings.Contains(text, s) {
			return true
		}
	}
	return false
}

func fileSize(path string) int64 {
	if path == "" {
		return 0
	}
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.Size()
}

func readFrom(path string, off int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.Size() <= off {
		return ""
	}
	start := off
	const max = 32 << 10
	if st.Size()-start > max {
		start = st.Size() - max
	}
	b := make([]byte, st.Size()-start)
	n, err := f.ReadAt(b, start)
	if n == 0 || (err != nil && err != io.EOF) {
		return ""
	}
	return string(b[:n])
}

const libexecDir = "/usr/local/libexec/inferoute"

func binPaths() (cli, daemon string) {
	return filepath.Join(libexecDir, "inferscale"), filepath.Join(libexecDir, "inferscaled")
}

// stateDir holds the one-shot key. The binaries do not live here.
func stateDir() (string, error) {
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
	cli, daemon := binPaths()
	if rootOwned(cli) == nil && (daemonName == "" || rootOwned(daemon) == nil) {
		return cli, nil
	}
	if err := ensureSudo(ctx); err != nil {
		return "", err
	}
	if err := lockDownDir(ctx, libexecDir); err != nil {
		return "", err
	}
	if err := installBinary(ctx, downloadURL(cliName), cli); err != nil {
		return "", err
	}
	if daemonName != "" {
		if err := installBinary(ctx, downloadURL(daemonName), daemon); err != nil {
			return "", err
		}
	}
	if err := rootOwned(cli); err != nil {
		return "", fmt.Errorf("inferscale install: %w", err)
	}
	if daemonName != "" {
		if err := rootOwned(daemon); err != nil {
			return "", fmt.Errorf("inferscaled install: %w", err)
		}
	}
	return cli, nil
}

func installBinary(ctx context.Context, url, dest string) error {
	if err := rejectSymlink(dest); err != nil {
		return err
	}
	if euid() == 0 {
		if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := download(ctx, url, dest); err != nil {
			return err
		}
		return os.Chmod(dest, 0o755)
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
	tmp := dest + ".tmp"
	if err := runSudo(ctx, "rm", "-f", tmp); err != nil {
		return err
	}
	pr, pw := io.Pipe()
	cmd := exec.CommandContext(ctx, "sudo", "-n", "--", "tee", tmp)
	cmd.Stdin = pr
	cmd.Stdout = io.Discard
	cmd.Stderr = os.Stderr
	copyErrc := make(chan error, 1)
	go func() {
		n, copyErr := io.Copy(pw, res.Body)
		if copyErr == nil && n == 0 {
			copyErr = fmt.Errorf("download %s: empty", url)
		}
		closeErr := pw.CloseWithError(copyErr)
		if copyErr == nil {
			copyErr = closeErr
		}
		copyErrc <- copyErr
	}()
	runErr := cmd.Run()
	copyErr := <-copyErrc
	if copyErr != nil || runErr != nil {
		_ = runSudo(ctx, "rm", "-f", tmp)
		if copyErr != nil {
			return copyErr
		}
		return fmt.Errorf("install %s: %w", dest, runErr)
	}
	if err := runSudo(ctx, "mv", "-f", tmp, dest); err != nil {
		return err
	}
	if err := runSudo(ctx, "chmod", "0755", dest); err != nil {
		return err
	}
	return runSudo(ctx, "chown", "root:root", dest)
}

func runSudo(ctx context.Context, args ...string) error {
	bin, argv := rootArgv(euid(), args[0], args[1:]...)
	cmd := exec.CommandContext(ctx, bin, argv...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sudo %s: %w", args[0], err)
	}
	return nil
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
	tmp := dest + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(f, res.Body)
	if copyErr == nil && n > 0 {
		copyErr = f.Sync()
	}
	closeErr := f.Close()
	if copyErr != nil || n == 0 {
		os.Remove(tmp)
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		return fmt.Errorf("download %s: empty", url)
	}
	if closeErr != nil {
		os.Remove(tmp)
		return closeErr
	}
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
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
		BackendState string `json:"BackendState"`
		Self         struct {
			ID           string   `json:"ID"`
			Online       bool     `json:"Online"`
			TailscaleIPs []string `json:"TailscaleIPs"`
		} `json:"Self"`
	}
	if err := json.Unmarshal(out, &st); err != nil {
		return nodeState{}, err
	}
	// Self.Online stays true on an expired node that sits in NeedsLogin.
	parsed := nodeState{ID: st.Self.ID, Online: st.Self.Online && st.BackendState == "Running"}
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

// CLI is the root-owned inferscale binary. A user-writable PATH entry is ignored.
func CLI() (string, error) {
	cli, err := trustedCLI()
	if err != nil {
		return "", fmt.Errorf("inferscale is not installed: %w", err)
	}
	return cli, nil
}

func trustedCLI() (string, error) {
	cli, _ := binPaths()
	if err := rootOwned(cli); err == nil {
		return cli, nil
	}
	path, err := exec.LookPath("inferscale")
	if err != nil {
		return "", err
	}
	if err := rootOwned(path); err != nil {
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
	cli, err := trustedCLI()
	if err != nil {
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
