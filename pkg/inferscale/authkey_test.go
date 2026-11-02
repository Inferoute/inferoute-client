package inferscale

import (
	"os"
	"testing"
)

func TestResolveAuthKeySurvivesEmptyRetry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if got := resolveAuthKey("tskey-auth-once"); got != "tskey-auth-once" {
		t.Fatalf("save = %q", got)
	}
	path, err := authKeyPath()
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %o", st.Mode().Perm())
	}

	// resumeWorker / refreshCluster call BringUp with an empty key after
	// Exchange already spent the join code.
	if got := resolveAuthKey(""); got != "tskey-auth-once" {
		t.Fatalf("retry = %q", got)
	}

	clearAuthKey()
	if got := resolveAuthKey(""); got != "" {
		t.Fatalf("cleared = %q", got)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("auth-key still on disk: %v", err)
	}
}

func TestResolveAuthKeyOverwritesPrevious(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if got := resolveAuthKey("old"); got != "old" {
		t.Fatal(got)
	}
	if got := resolveAuthKey("new"); got != "new" {
		t.Fatal(got)
	}
	if got := resolveAuthKey(""); got != "new" {
		t.Fatalf("loaded %q", got)
	}
}
