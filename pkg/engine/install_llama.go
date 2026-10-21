package engine

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// llamaCppRelease is the ggml-org/llama.cpp tag we install.
// shortcut: pin moves only when serve flags or the cache filename change.
const llamaCppRelease = "b11770"

const llamaReleaseBase = "https://github.com/ggml-org/llama.cpp/releases/download/" + llamaCppRelease

func installLlamaCpp(ctx context.Context, w io.Writer) error {
	switch runtime.GOOS {
	case "windows":
		return installLlamaWindows(ctx, w)
	case "darwin":
		if runtime.GOARCH != "arm64" {
			return fmt.Errorf("llama.cpp auto-install on macOS requires Apple Silicon")
		}
		return installLlamaDarwin(ctx, w)
	default:
		return fmt.Errorf("llama.cpp auto-install is only supported on Windows and macOS")
	}
}

func installLlamaWindows(ctx context.Context, w io.Writer) error {
	dir, err := llamaInstallDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	fmt.Fprintf(w, "Installing llama.cpp %s into %s...\n", llamaCppRelease, dir)
	binURL := llamaReleaseBase + "/llama-" + llamaCppRelease + "-bin-win-cuda-12.4-x64.zip"
	cudaURL := llamaReleaseBase + "/cudart-llama-bin-win-cuda-12.4-x64.zip"
	if err := downloadAndExtract(ctx, w, binURL, dir); err != nil {
		return err
	}
	return downloadAndExtract(ctx, w, cudaURL, dir)
}

func installLlamaDarwin(ctx context.Context, w io.Writer) error {
	dir, err := llamaInstallDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	fmt.Fprintf(w, "Installing llama.cpp %s into %s...\n", llamaCppRelease, dir)
	url := llamaReleaseBase + "/llama-" + llamaCppRelease + "-bin-macos-arm64.tar.gz"
	return downloadAndExtract(ctx, w, url, dir)
}

func llamaInstallDir() (string, error) {
	if runtime.GOOS == "windows" {
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			return "", fmt.Errorf("LOCALAPPDATA is unset")
		}
		return filepath.Join(base, "inferoute", "llama.cpp"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "inferoute", "llama.cpp"), nil
}

func downloadAndExtract(ctx context.Context, w io.Writer, url, dest string) error {
	tmp, err := os.CreateTemp("", "llama-cpp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Close(); err != nil {
		return err
	}
	fmt.Fprintf(w, "Downloading %s\n", url)
	if err := download(ctx, url, tmpName); err != nil {
		return err
	}
	if strings.HasSuffix(url, ".tar.gz") {
		return extractTarGz(tmpName, dest)
	}
	return extractZip(tmpName, dest)
}

func extractZip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if err := writeArchiveFile(dest, filepath.Base(f.Name), f.Mode(), func() (io.ReadCloser, error) {
			return f.Open()
		}); err != nil {
			return err
		}
	}
	return nil
}

func extractTarGz(src, dest string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if hdr.FileInfo().IsDir() {
			continue
		}
		name := filepath.Base(hdr.Name)
		mode := hdr.FileInfo().Mode()
		if err := writeArchiveFile(dest, name, mode, func() (io.ReadCloser, error) {
			return io.NopCloser(tr), nil
		}); err != nil {
			return err
		}
	}
}

func writeArchiveFile(dest, name string, mode os.FileMode, open func() (io.ReadCloser, error)) error {
	if name == "" || name == "." || strings.Contains(name, "..") {
		return nil
	}
	rc, err := open()
	if err != nil {
		return err
	}
	defer rc.Close()
	outPath := filepath.Join(dest, name)
	out, err := os.OpenFile(outPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, modeOrExec(mode))
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return err
}

func modeOrExec(mode os.FileMode) os.FileMode {
	if runtime.GOOS == "windows" {
		return 0o644
	}
	if mode&0o111 != 0 {
		return mode
	}
	return 0o755
}
