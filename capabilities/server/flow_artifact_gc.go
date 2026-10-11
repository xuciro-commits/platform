package platformserver

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"platformserver/platform"
)

// This is process exclusion and a recovery watermark, never a second artifact
// ledger or retry queue. In-flight/ambiguous bytes born in this process cannot
// be declared orphaned from a record view that may not have applied their commit.
type flowArtifactGC struct {
	gate            sync.RWMutex
	recoveredBefore time.Time
}

const flowOrphanRetention = 24 * time.Hour
const maxFlowInventoryFiles = 65536
const maxFlowReferenceBytes = 256 << 20

// A lease spans preparation through commit/discard, before taking Tenant.mu.
func (t *Tenant) beginFlowArtifactPreparation() func() {
	t.flowArtifacts.gate.RLock()
	return t.flowArtifacts.gate.RUnlock
}

// The deployment calls this only after full restore/replay and process Check,
// with the clock captured BEFORE recovery. NewTenant never assumes recovery.
func (t *Tenant) flowArtifactRecoveryComplete(started time.Time) {
	if started.IsZero() || t.quarantined() {
		return
	}
	t.flowArtifacts.gate.Lock()
	defer t.flowArtifacts.gate.Unlock()
	t.flowArtifacts.recoveredBefore = started.UTC()
}

// flowArtifactReferences reads the complete original record owner's view.
// Historical before/after values remain roots even after the live frame moves.
func (s *recordStore) flowArtifactReferences(store flowFrameStore) (map[string]bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entity := s.types[platform.FlowInstanceType]
	if entity == nil {
		return nil, fmt.Errorf("Flow artifact references have no installed record owner")
	}
	keep := map[string]bool{}
	remaining := maxFlowReferenceBytes
	var visit func(json.RawMessage, int) error
	visit = func(raw json.RawMessage, depth int) error {
		if len(raw) == 0 {
			return nil
		}
		if depth > 64 || len(raw) > remaining {
			return fmt.Errorf("Flow artifact references exceed the complete-read budget")
		}
		remaining -= len(raw)
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return fmt.Errorf("Flow artifact reference image is invalid: %w", err)
		}
		var walk func(any, int) error
		walk = func(value any, depth int) error {
			if depth > 64 {
				return fmt.Errorf("Flow artifact reference image exceeds its depth bound")
			}
			switch value := value.(type) {
			case map[string]any:
				_, ticket := value["ticket"]
				_, digest := value["digest"]
				_, owner := value["tenant"]
				_, instance := value["instance"]
				_, version := value["version"]
				_, size := value["size"]
				if ticket && digest && owner && instance && version && size {
					encoded, err := json.Marshal(value)
					if err != nil {
						return err
					}
					var ref platform.FlowStateArtifact
					if err := json.Unmarshal(encoded, &ref); err != nil {
						return fmt.Errorf("Flow artifact reference is invalid: %w", err)
					}
					key, err := store.key(ref)
					if err != nil {
						return err
					}
					keep[key] = true
					if len(keep) > maxFlowInventoryFiles {
						return fmt.Errorf("Flow artifact reference set exceeds its complete-read budget")
					}
				}
				for name, child := range value {
					if (name == "sealed" || name == "checkpointArtifact") && child != nil {
						encoded, err := json.Marshal(child)
						if err != nil {
							return err
						}
						var ref platform.FlowStateArtifact
						if err := json.Unmarshal(encoded, &ref); err != nil {
							return err
						}
						key, err := store.key(ref)
						if err != nil {
							return err
						}
						keep[key] = true
					}
					if err := walk(child, depth+1); err != nil {
						return err
					}
				}
			case []any:
				for _, child := range value {
					if err := walk(child, depth+1); err != nil {
						return err
					}
				}
			}
			return nil
		}
		return walk(value, depth)
	}
	for _, row := range entity.rows {
		image, err := row.image()
		if err != nil {
			return nil, err
		}
		if err := visit(image, 0); err != nil {
			return nil, err
		}
		// Original recovery bytes may differ from the current typed image.
		current, err := json.Marshal(row.value.Interface())
		if err != nil {
			return nil, err
		}
		if err := visit(current, 0); err != nil {
			return nil, err
		}
		for _, change := range row.history {
			for _, field := range change.Fields {
				if err := visit(field.Before, 0); err != nil {
					return nil, err
				}
				if err := visit(field.After, 0); err != nil {
					return nil, err
				}
			}
		}
	}
	return keep, nil
}

func flowInventoryKey(key, prefix string) bool {
	if !strings.HasPrefix(key, prefix) {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(key, prefix), "/")
	if len(parts) != 3 || !strings.HasSuffix(parts[2], ".json") {
		return false
	}
	parts[2] = strings.TrimSuffix(parts[2], ".json")
	for index, size := range []int{32, 16, 32} {
		decoded, err := hex.DecodeString(parts[index])
		if err != nil || len(decoded) != size || hex.EncodeToString(decoded) != parts[index] {
			return false
		}
	}
	return true
}

// The existing continuous-source poll calls this; a failed delete leaves the
// original file to retry next poll. An incomplete inventory/reference read never
// authorizes a partial sweep. Superseded accepted frames use their original
// accepted cleanup intent instead; this collector only handles orphan bytes.
func (t *Tenant) collectUnacceptedFlowArtifacts(ctx context.Context, now time.Time) error {
	if !t.flowArtifacts.gate.TryLock() {
		return nil
	}
	defer t.flowArtifacts.gate.Unlock()
	started := t.flowArtifacts.recoveredBefore
	if started.IsZero() || now.IsZero() || t.quarantined() {
		return nil
	}
	inventory, ok := t.files().(FileInventory)
	if !ok {
		return fmt.Errorf("Flow artifact file inventory is unavailable")
	}
	cutoff := now.Add(-flowOrphanRetention)
	// Object-store modification clocks can be rounded down to whole seconds.
	if recoveryCutoff := started.Add(-time.Second); recoveryCutoff.Before(cutoff) {
		cutoff = recoveryCutoff
	}
	prefix := t.ID + "/artifacts/flow/"
	var candidates []StoredFile
	cursor := ""
	seen := 0
	for {
		page, err := inventory.Inventory(ctx, prefix, cursor, maxFileInventoryPage)
		if err != nil {
			return err
		}
		if len(page.Files) > maxFileInventoryPage {
			return fmt.Errorf("Flow artifact inventory exceeded its page bound")
		}
		previous := cursor
		for _, file := range page.Files {
			if file.Key <= previous || !strings.HasPrefix(file.Key, prefix) || file.Modified.IsZero() || file.Size < 0 {
				return fmt.Errorf("Flow artifact inventory returned an invalid page")
			}
			previous = file.Key
			seen++
			if seen > maxFlowInventoryFiles {
				return fmt.Errorf("Flow artifact inventory exceeds its complete-read budget; no files deleted")
			}
			if !file.Modified.After(cutoff) && flowInventoryKey(file.Key, prefix) {
				candidates = append(candidates, file)
			}
		}
		if page.Next == "" {
			break
		}
		if len(page.Files) == 0 || page.Next != previous {
			return fmt.Errorf("Flow artifact inventory cursor does not advance")
		}
		cursor = page.Next
	}
	if len(candidates) == 0 {
		return nil
	}
	t.mu.Lock()
	if t.quarantined() {
		t.mu.Unlock()
		return nil
	}
	keep, err := t.records.flowArtifactReferences(flowFrameStore{tenant: t.ID, files: t.files()})
	t.mu.Unlock()
	if err != nil {
		return err
	}
	for _, file := range candidates {
		if err := ctx.Err(); err != nil {
			return err
		}
		if t.quarantined() {
			return fmt.Errorf("Flow artifact collection stopped for a quarantined tenant")
		}
		if keep[file.Key] {
			continue
		}
		if err := t.files().Delete(ctx, file.Key); err != nil {
			return fmt.Errorf("discard orphan Flow artifact: %w", err)
		}
	}
	return nil
}
