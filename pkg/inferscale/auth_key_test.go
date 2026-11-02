package inferscale

import (
	"os"
	"testing"
)

func TestAuthKeySurvivesEmptyRetry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

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
	if info.Mode().Perm() != 0o600 {
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
