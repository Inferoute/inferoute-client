package verify

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// NormalizeDigest strips an optional sha256: prefix and lowercases hex.
func NormalizeDigest(digest string) string {
	d := strings.TrimSpace(digest)
	d = strings.TrimPrefix(d, "sha256:")
	return strings.ToLower(d)
}

// FileHash computes the per-file hash using the manifest hash_method.
// Must match inferoute-node/scripts/bootstrap_approved_model.sh.
func FileHash(path, method string) (string, error) {
	switch method {
	case "safetensors_header":
		return safetensorsHeaderSHA256(path)
	case "full":
		return fullFileSHA256(path)
	default:
		return "", fmt.Errorf("unsupported hash_method: %s", method)
	}
}

func fullFileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// safetensorsHeaderSHA256 hashes 8-byte LE header length + header JSON bytes.
func safetensorsHeaderSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var lenBuf [8]byte
	if _, err := io.ReadFull(f, lenBuf[:]); err != nil {
		return "", fmt.Errorf("safetensors file too short: %w", err)
	}
	headerLen := binary.LittleEndian.Uint64(lenBuf[:])
	header := make([]byte, headerLen)
	if _, err := io.ReadFull(f, header); err != nil {
		return "", fmt.Errorf("truncated safetensors header: %w", err)
	}

	payload := append(lenBuf[:], header...)
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

// WeightFingerprint aggregates per-file hashes into the platform fingerprint.
// Lines are sorted "name:hash\n" then SHA-256 of the concatenation.
func WeightFingerprint(root string, manifest []ManifestEntry) (string, error) {
	if len(manifest) == 0 {
		return "", fmt.Errorf("empty manifest")
	}

	lines := make([]string, 0, len(manifest))
	for _, entry := range manifest {
		path := filepath.Join(root, entry.Name)
		hash, err := FileHash(path, entry.HashMethod)
		if err != nil {
			return "", fmt.Errorf("hash %s: %w", entry.Name, err)
		}
		lines = append(lines, fmt.Sprintf("%s:%s", entry.Name, hash))
	}
	sort.Strings(lines)

	joined := strings.Join(lines, "\n")
	if len(lines) > 0 {
		joined += "\n"
	}
	sum := sha256.Sum256([]byte(joined))
	return hex.EncodeToString(sum[:]), nil
}
