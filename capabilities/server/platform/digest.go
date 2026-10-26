package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// ValidDigest reports whether value is a sha256 hex digest, with or without prefix.
func ValidDigest(value string) bool {
	value = strings.TrimPrefix(value, "sha256:")
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}
