package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// SchemaVersion binds a client contract to its owner's immutable definition.
// Family is the existing K7 numeric schema version; compatible publications
// retain it while their complete definition content has a new identity.
type SchemaVersion struct {
	Version string `json:"version"`
	Family  uint32 `json:"family"`
}

func (v SchemaVersion) Check() error {
	raw, ok := strings.CutPrefix(v.Version, "schema.sha256.")
	decoded, err := hex.DecodeString(raw)
	if !ok || err != nil || len(decoded) != sha256.Size || strings.ToLower(raw) != raw || v.Family == 0 || v.Family > 64 {
		return fmt.Errorf("schema version needs its content identity and family")
	}
	return nil
}

func SchemaContentVersion(body json.RawMessage) (string, error) {
	canonical, err := canonicalBody(body)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return "schema.sha256." + hex.EncodeToString(sum[:]), nil
}
