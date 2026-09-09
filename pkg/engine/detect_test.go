package engine

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunnableBinBrokenShebang(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shebang exec is Unix-only")
	}
	dir := t.TempDir()
	dead := filepath.Join(dir, "missing-python")
	script := filepath.Join(dir, "vllm")
	body := "#!" + dead + "\nfrom vllm.entrypoints.cli.main import main\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	if runnableBin(script) {
		t.Fatal("wrapper with missing interpreter must not be runnable")
	}
	got := brokenBinReason(script)
	if !strings.Contains(got, "missing-python") || !strings.Contains(got, "broken venv") {
		t.Fatalf("brokenBinReason = %q", got)
	}
	if usableEngineBin(KindVLLMMetal, script) {
		t.Fatal("usableEngineBin must reject broken shebang")
	}
}

func TestRunnableBinWorkingShebang(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shebang exec is Unix-only")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "ok")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !runnableBin(script) {
		t.Fatal("#!/bin/sh script should be runnable")
	}
}

func TestRunnableBinEnvShebang(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shebang exec is Unix-only")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "ok")
	if err := os.WriteFile(script, []byte("#!/usr/bin/env sh\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !runnableBin(script) {
		t.Fatal("#!/usr/bin/env sh should be runnable")
	}
}

func TestRunnableBinDanglingSymlinkInterpreter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shebang exec is Unix-only")
	}
	dir := t.TempDir()
	missing := filepath.Join(dir, "python3.12-gone")
	link := filepath.Join(dir, "python3")
	if err := os.Symlink(missing, link); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "vllm")
	if err := os.WriteFile(script, []byte("#!"+link+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if runnableBin(script) {
		t.Fatal("dangling interpreter symlink must not be runnable")
	}
}

func TestStartDetachedBrokenShebangMessage(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shebang exec is Unix-only")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "vllm")
	if err := os.WriteFile(script, []byte("#!/no/such/python\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := StartDetached(Spec{Bin: script, Args: []string{"serve"}}, filepath.Join(dir, "engine.log"))
	if err == nil {
		t.Fatal("StartDetached: want error")
	}
	if !strings.Contains(err.Error(), "broken venv") {
		t.Fatalf("StartDetached error = %v, want broken venv", err)
	}
}
