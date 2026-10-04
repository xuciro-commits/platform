package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// PageContentVersion identifies the complete original descriptor. A member
// projection keeps this identity but is not byte-identical to the original.
func PageContentVersion(page Page) (string, error) {
	body, err := json.Marshal(page)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return "page.sha256." + hex.EncodeToString(sum[:]), nil
}
func CheckPageContentVersion(version string) error {
	text, ok := strings.CutPrefix(version, "page.sha256.")
	raw, err := hex.DecodeString(text)
	if !ok || err != nil || len(raw) != sha256.Size || hex.EncodeToString(raw) != text {
		return fmt.Errorf("invalid page content version")
	}
	return nil
}
