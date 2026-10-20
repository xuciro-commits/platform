// Package acceptance provides pure accepted-result encoding and validation.
// The tenant and each business owner retain persistence, locking and decisions.
package acceptance

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
)

const MaxResultBytes = 1 << 20

// Digest preserves the original accepted-result canonical JSON encoding,
// including exact JSON number spellings and large integers in raw images.
// Result families clear their own digest field before calling this function.
func Digest(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	var normalized any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&normalized); err != nil {
		return "", err
	}
	canonical, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}

// DecodeJSON reads exactly one typed value. Result families retain their own
// size, version, identity and predecessor checks.
func DecodeJSON(raw []byte, target any, name string) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("%s has trailing data", name)
	}
	return nil
}
