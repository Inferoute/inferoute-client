package engine

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReplaceVenvDirRemovesExisting(t *testing.T) {
	dir := t.TempDir()
	venv := filepath.Join(dir, ".venv-vllm-metal")
	if err := os.Mkdir(venv, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(venv, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := replaceVenvDir(&out, venv); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(venv); !os.IsNotExist(err) {
		t.Fatalf("venv still exists: %v", err)
	}
	if !strings.Contains(out.String(), venv) {
		t.Fatalf("stdout = %q, want venv path", out.String())
	}
}

func TestCLIHasSubcommand(t *testing.T) {
	help := `usage: vllm [-h] [-v]
            {chat,complete,serve,launch,bench,collect-env,preload,run-batch,snapshot}
            ...`
	if cliHasSubcommand(help, "download-kernels") {
		t.Fatal("vLLM 0.33 help selected download-kernels")
	}
	if !cliHasSubcommand(help, "serve") {
		t.Fatal("serve missing from help")
	}
	newer := "{chat,complete,serve,download-kernels,snapshot}"
	if !cliHasSubcommand(newer, "download-kernels") {
		t.Fatal("download-kernels missing from help")
	}
}

func TestReplaceVenvDirMissingOK(t *testing.T) {
	if err := replaceVenvDir(io.Discard, filepath.Join(t.TempDir(), "nope")); err != nil {
		t.Fatal(err)
	}
}

func TestVLLMRequirement(t *testing.T) {
	if vllmRequirement("") != "vllm" || vllmRequirement("  ") != "vllm" {
		t.Fatal("empty pin should install latest")
	}
	if got := vllmRequirement("0.31.0"); got != "vllm==0.31.0" {
		t.Fatal(got)
	}
}

func TestInstalledVersion(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "vllm")
	body := "#!/bin/sh\necho vLLM version 0.31.0\n"
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := InstalledVersion(bin); got != "0.31.0" {
		t.Fatalf("version = %q", got)
	}
	if InstalledVersion("") != "" {
		t.Fatal("empty bin")
	}
}
