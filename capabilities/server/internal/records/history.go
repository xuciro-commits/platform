// Package records contains pure record representation operations, without a
// tenant, business decisions, storage coordination or authorization policy.
package records

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

const EmptyHistory = "history.v1.empty"

// AppendHistory extends an immutable chain using canonical JSON, independent
// of PostgreSQL's JSONB whitespace/key order. The position is zero based.
func AppendHistory(prior string, position int, change json.RawMessage) (string, error) {
	if position < 0 || position == 0 && prior != EmptyHistory {
		return "", fmt.Errorf("invalid history chain predecessor")
	}
	if position > 0 {
		raw, err := hex.DecodeString(prior)
		if err != nil || len(raw) != sha256.Size {
			return "", fmt.Errorf("invalid history chain digest")
		}
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(change))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	raw, err := json.Marshal([]any{"platform.record-history.v1", position, prior, value})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
