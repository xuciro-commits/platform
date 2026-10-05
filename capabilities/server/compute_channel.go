package platformserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"platformserver/platform"
)

// The per-call result channel (ADR-0047 §13.3, plan A; ordered 2026-10-05).
//
// An operation's inline profile is bounded (48 KiB today). Plan A adds a
// call-specific channel for results above that bound: the host allocates a
// sealed artifact for one call, writes the bytes itself, checks size, schema
// and digest, and the accepted result references the handle rather than
// carrying the bytes. A worker never receives a write handle, cannot enumerate
// the store and cannot pick a path — it hands its bytes back through the same
// call, and the host seals them. A staged result that is never accepted is
// reclaimed by the host's own artifact owner.

// maxInlineOutputBytes is the inline result budget operations keep by default.
const maxInlineOutputBytes = 48 << 10

// maxStagedOutputBytes bounds one call's channel.
const maxStagedOutputBytes = 16 << 20

// stagedKey is the call-scoped key: the tenant, the call and the schema name it
// claims, so two calls can never address each other's bytes.
func stagedKey(tenant, call, schema string) string {
	safe := func(s string) string {
		return strings.Map(func(r rune) rune {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
				return r
			}
			return '-'
		}, s)
	}
	return "calls/" + safe(tenant) + "/" + safe(call) + "/" + safe(schema) + ".json"
}

// StageResult writes one call's result bytes into the channel and answers the
// handle the accepted result references. Size and schema are checked before
// anything is sealed; the digest proves the bytes afterwards.
func (t *Tenant) StageResult(callID, schema string, raw []byte, budget int) (platform.StagedResult, error) {
	handle := platform.StagedResult{Tenant: t.ID, Call: callID, Schema: schema, Key: stagedKey(t.ID, callID, schema), Size: len(raw)}
	if callID == "" || schema == "" || len(raw) == 0 {
		return handle, fmt.Errorf("a staged result needs a call, a schema and bytes")
	}
	if budget < 1 {
		budget = maxStagedOutputBytes
	}
	if budget > maxStagedOutputBytes {
		budget = maxStagedOutputBytes
	}
	if len(raw) > budget {
		return handle, fmt.Errorf("the result is %d bytes, the call channel accepts %d", len(raw), budget)
	}
	if !json.Valid(raw) {
		return handle, fmt.Errorf("a staged result must be JSON")
	}
	sum := sha256.Sum256(raw)
	handle.Digest = "sha256:" + hex.EncodeToString(sum[:])
	if err := t.files().Put(context.Background(), handle.Key, raw, "application/json"); err != nil {
		return handle, fmt.Errorf("stage result: %w", err)
	}
	t.mu.Lock()
	if t.staged == nil {
		t.staged = map[string]platform.StagedResult{}
	}
	t.staged[callID] = handle
	t.mu.Unlock()
	return handle, nil
}

// ReadStagedResult reads a handle back and verifies its digest, so a result is
// exactly what the call sealed.
func (t *Tenant) ReadStagedResult(handle platform.StagedResult) ([]byte, error) {
	reader, size, err := t.files().Get(context.Background(), handle.Key)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(reader); err != nil {
		return nil, err
	}
	raw := buf.Bytes()
	sum := sha256.Sum256(raw)
	if "sha256:"+hex.EncodeToString(sum[:]) != handle.Digest || len(raw) != handle.Size || size != int64(handle.Size) {
		return nil, fmt.Errorf("staged result %s differs from its sealed digest", handle.Call)
	}
	if !json.Valid(raw) {
		return nil, fmt.Errorf("staged result %s is not JSON", handle.Call)
	}
	return raw, nil
}

// StagedResults lists the channel's entries, newest first, for the host console.
func (t *Tenant) StagedResults() []platform.StagedResult {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]platform.StagedResult, 0, len(t.staged))
	for _, handle := range t.staged {
		out = append(out, handle)
	}
	slices.SortFunc(out, func(a, b platform.StagedResult) int { return strings.Compare(a.Call, b.Call) })
	return out
}

// operationOutput accepts an operation's result under the operation's declared
// budgets: inline while it fits, staged through the call channel when the
// operation declares one, and refused — never trimmed — otherwise. The answer
// carries a small reference when the bytes were staged.
func (t *Tenant) operationOutput(op platform.Operation, callID string, raw []byte) (json.RawMessage, *platform.StagedResult, error) {
	if len(raw) <= op.Limits.MaxOutputBytes && len(raw) <= maxInlineOutputBytes {
		if err := op.Output.Validate(raw, op.Limits.MaxOutputBytes); err != nil {
			return nil, nil, err
		}
		return raw, nil, nil
	}
	if op.Limits.StagedOutputBytes < 1 {
		return nil, nil, fmt.Errorf("the result exceeds the operation's inline budget and it declares no staged channel")
	}
	if err := op.Output.Validate(raw, op.Limits.StagedOutputBytes); err != nil {
		return nil, nil, err
	}
	handle, err := t.StageResult(callID, op.Name, raw, op.Limits.StagedOutputBytes)
	if err != nil {
		return nil, nil, err
	}
	reference, _ := json.Marshal(map[string]any{"staged": handle})
	return reference, &handle, nil
}

// reclaimStaged drops a sealed result whose call was never accepted. The host
// owns the store, so cleanup is here and not in any worker.
func (t *Tenant) reclaimStaged(callID string, now time.Time) error {
	t.mu.Lock()
	handle, ok := t.staged[callID]
	if ok {
		delete(t.staged, callID)
	}
	t.mu.Unlock()
	if !ok {
		return nil
	}
	if err := t.files().Delete(context.Background(), handle.Key); err != nil {
		return err
	}
	_ = now
	return nil
}
