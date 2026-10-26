package driver

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"platformserver/platform"
)

const MaxBinaryBytes = 64 << 20

// Binary resolves only a digest beneath the administrator's trusted directory.
// A binding never supplies a program name, command line or download address.
func Binary(directory, digest string) (string, []byte, error) {
	if !platform.DriverDigest(digest) {
		return "", nil, fmt.Errorf("invalid driver binary digest")
	}
	path := filepath.Join(directory, "artifacts", strings.TrimPrefix(digest, "sha256:"))
	info, err := os.Lstat(path)
	if err != nil {
		return "", nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > MaxBinaryBytes {
		return "", nil, fmt.Errorf("driver binary is absent or exceeds its byte budget")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, MaxBinaryBytes+1))
	sum := sha256.Sum256(raw)
	if err != nil || len(raw) > MaxBinaryBytes || hex.EncodeToString(sum[:]) != strings.TrimPrefix(digest, "sha256:") {
		return "", nil, fmt.Errorf("driver binary content differs from its digest")
	}
	if info.Mode().Perm()&0111 == 0 {
		return "", nil, fmt.Errorf("driver binary is not executable")
	}
	return path, raw, nil
}
