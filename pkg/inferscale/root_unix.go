//go:build !windows

package inferscale

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func euid() int { return os.Geteuid() }

// ensureSudo caches a sudo credential when this process is not root.
// Later inferscale commands use sudo -n and do not prompt again.
func ensureSudo(ctx context.Context) error {
	if os.Geteuid() == 0 {
		return nil
	}
	if _, err := exec.LookPath("sudo"); err != nil {
		return fmt.Errorf("inferscale needs root to create %s, and sudo is not installed", TUN)
	}
	fmt.Fprintf(os.Stderr, "inferscale needs root to create %s\n", TUN)
	cmd := exec.CommandContext(ctx, "sudo", "-v")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sudo: %w", err)
	}
	return nil
}

// rootOwned is the check before sudo. The file must be a root-owned regular
// file that group and other cannot write, and no ancestor directory can be
// writable by this user. Symlinks fail.
func rootOwned(path string) error {
	var st unix.Stat_t
	if err := unix.Lstat(path, &st); err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT == unix.S_IFLNK {
		return fmt.Errorf("%s is a symlink", path)
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG {
		return fmt.Errorf("%s is not a regular file", path)
	}
	if st.Uid != 0 {
		return fmt.Errorf("%s is not owned by root", path)
	}
	if st.Mode&0o022 != 0 {
		return fmt.Errorf("%s is writable by group or other", path)
	}
	if euid() == 0 {
		return nil
	}
	return ancestorsLocked(filepath.Dir(path))
}

func rejectSymlink(path string) error {
	var st unix.Stat_t
	err := unix.Lstat(path, &st)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		return err
	}
	if st.Mode&unix.S_IFMT == unix.S_IFLNK {
		return fmt.Errorf("%s is a symlink", path)
	}
	return nil
}

func lockDownDir(ctx context.Context, dir string) error {
	if err := rejectSymlink(dir); err != nil {
		return err
	}
	if err := runSudo(ctx, "install", "-d", "-o", "root", "-g", "root", "-m", "0755", dir); err != nil {
		return err
	}
	if euid() == 0 {
		return nil
	}
	return ancestorsLocked(dir)
}

func ancestorsLocked(dir string) error {
	for {
		var st unix.Stat_t
		if err := unix.Lstat(dir, &st); err != nil {
			return err
		}
		if st.Mode&unix.S_IFMT == unix.S_IFLNK {
			return fmt.Errorf("%s is a symlink", dir)
		}
		if st.Mode&unix.S_IFMT != unix.S_IFDIR {
			return fmt.Errorf("%s is not a directory", dir)
		}
		if userWritable(st) {
			return fmt.Errorf("%s is writable by this user", dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil
		}
		dir = parent
	}
}

func userWritable(st unix.Stat_t) bool {
	uid := uint32(os.Geteuid())
	if st.Uid == uid && st.Mode&0o200 != 0 {
		return true
	}
	if st.Mode&0o020 != 0 && sharesGroup(st.Gid) {
		return true
	}
	return st.Mode&0o002 != 0
}

func sharesGroup(gid uint32) bool {
	if uint32(os.Getegid()) == gid {
		return true
	}
	groups, err := os.Getgroups()
	if err != nil {
		return true
	}
	for _, g := range groups {
		if uint32(g) == gid {
			return true
		}
	}
	return false
}
