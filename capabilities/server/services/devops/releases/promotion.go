package releases

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"platformserver/platform"
)

type PromotionRequest struct {
	From        string `json:"from"`
	Candidate   string `json:"candidate"`
	Key         string `json:"key"`
	Activate    bool   `json:"activate"`
	TargetGrant string `json:"targetGrant"`
}

func (r PromotionRequest) Complete() bool { return r.Key != "" && r.TargetGrant != "" }

type PromotionResult struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Candidate string `json:"candidate"`
	Digest    string `json:"digest"`
	Active    bool   `json:"active"`
	Key       string `json:"key"`
	Assets    int    `json:"assets"`
}

type PromotionDestination struct {
	ID          string
	Quarantined bool
	HasApp      func(string) bool
	PriorHash   func(key string) (string, bool)
	PriorBytes  func(candidate string) json.RawMessage
	Hash        func() (string, error)
}

func (t PromotionDestination) Preflight(candidate platform.ReleaseCandidate, raw []byte, key string) (bool, error) {
	if err := MissingAssetHost(candidate, t.ID, t.HasApp); err != nil {
		return false, err
	}
	if t.Quarantined {
		return false, fmt.Errorf("tenant %s is quarantined", t.ID)
	}
	if prior, ok := t.PriorHash("promotion:" + key); ok {
		hash, err := t.Hash()
		if err != nil || prior != hash {
			return false, fmt.Errorf("promotion key belongs to another request")
		}
		return true, nil
	}
	if prior := t.PriorBytes(candidate.ID); prior != nil && !bytes.Equal(prior, raw) {
		return false, fmt.Errorf("candidate %s exists in %s with different bytes", candidate.ID, t.ID)
	}
	return false, nil
}

func ReadSealed(id string, artifact platform.SealedArtifact, open func() (io.ReadCloser, error)) ([]byte, error) {
	reader, err := open()
	if err != nil {
		return nil, fmt.Errorf("sealed candidate %s: %w", id, err)
	}
	defer reader.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(reader); err != nil {
		return nil, fmt.Errorf("sealed candidate %s: %w", id, err)
	}
	if err := VerifySealed(id, artifact, buf.Bytes()); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
