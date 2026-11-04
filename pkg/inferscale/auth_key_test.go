package inferscale

import (
	"os"
	"runtime"
	"testing"
)

func TestAuthKeySurvivesEmptyRetry(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir) // Windows UserHomeDir ignores HOME.

	got, err := persistAuthKey("tskey-one")
	if err != nil || got != "tskey-one" {
		t.Fatalf("save = %q %v", got, err)
	}
	path, err := authKeyPath()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Windows only honors the read-only bit, so a writable file is always 0666.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}

	got, err = persistAuthKey("")
	if err != nil || got != "tskey-one" {
		t.Fatalf("empty retry = %q %v", got, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("empty retry removed the key: %v", err)
	}

	got, err = persistAuthKey("tskey-two")
	if err != nil || got != "tskey-two" {
		t.Fatalf("overwrite = %q %v", got, err)
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "tskey-two" {
		t.Fatalf("file = %q %v", body, err)
	}

	clearAuthKey()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("key still on disk: %v", err)
	}
	got, err = persistAuthKey("")
	if err != nil || got != "" {
		t.Fatalf("after clear = %q %v", got, err)
	}
}

func TestLeftoverKeyIsPendingJoinOnEmptyRetry(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)

	if _, err := persistAuthKey("tskey-one"); err != nil {
		t.Fatal(err)
	}
	// resumeWorker / refreshCluster call BringUp with "".
	got, err := persistAuthKey("")
	if err != nil || got != "tskey-one" {
		t.Fatalf("retry = %q %v", got, err)
	}
	if !pendingAuthKey("", got) {
		t.Fatal("empty retry must still treat the leftover key as a pending join")
	}
}

func TestAuthKeyRejected(t *testing.T) {
	if !authKeyRejected("", "Received error: register request: http 401: authkey already used") {
		t.Fatal("used key")
	}
	if !authKeyRejected("backend error: invalid pre auth key", "") {
		t.Fatal("unknown key")
	}
	if authKeyRejected("inferscale up: signal: interrupt", "Start inferscaled") {
		t.Fatal("unrelated failure")
	}
}
