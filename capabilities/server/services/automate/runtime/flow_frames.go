package automation

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"platformserver/platform"
)

const MaxFrameBytes = 64 << 20

type FrameStore struct {
	Tenant string
	Files  platform.FileStore
}

func (s FrameStore) key(ref platform.FlowStateArtifact) (string, error) {
	ticket, te := hex.DecodeString(ref.Ticket)
	digest, de := hex.DecodeString(ref.Digest)
	if ref.Tenant != s.Tenant || ref.Instance == "" || ref.Version < 1 || te != nil || len(ticket) != 16 || de != nil || len(digest) != sha256.Size || ref.Size < 1 || ref.Size > MaxFrameBytes {
		return "", fmt.Errorf("the Flow state handle has an invalid owner or bound")
	}
	owner := sha256.Sum256(platform.Raw([]any{s.Tenant, ref.Instance, ref.Version}))
	return fmt.Sprintf("%s/artifacts/flow/%x/%s/%s.json", s.Tenant, owner, ref.Ticket, ref.Digest), nil
}

func (s FrameStore) Seal(instance string, version int, raw []byte, budget int) (platform.FlowStateArtifact, error) {
	if budget <= 0 || budget > MaxFrameBytes {
		budget = MaxFrameBytes
	}
	if len(raw) == 0 || len(raw) > budget || !json.Valid(raw) {
		return platform.FlowStateArtifact{}, fmt.Errorf("the Flow frame exceeds its sealed JSON budget")
	}
	var ticket [16]byte
	if _, err := rand.Read(ticket[:]); err != nil {
		return platform.FlowStateArtifact{}, err
	}
	ref := platform.FlowStateArtifact{Tenant: s.Tenant, Instance: instance, Version: version, Ticket: hex.EncodeToString(ticket[:]), Digest: fmt.Sprintf("%x", sha256.Sum256(raw)), Size: len(raw)}
	key, err := s.key(ref)
	if err != nil {
		return ref, err
	}
	if err := s.Files.Put(context.Background(), key, raw, "application/json"); err != nil {
		return ref, fmt.Errorf("seal Flow frame: %w", err)
	}
	return ref, nil
}

func (s FrameStore) Read(ref platform.FlowStateArtifact) ([]byte, error) {
	key, err := s.key(ref)
	if err != nil {
		return nil, err
	}
	reader, size, err := s.Files.Get(context.Background(), key)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	if size != int64(ref.Size) {
		return nil, fmt.Errorf("the Flow artifact differs from its accepted size")
	}
	raw, err := io.ReadAll(io.LimitReader(reader, int64(ref.Size)+1))
	if err != nil || len(raw) != ref.Size || fmt.Sprintf("%x", sha256.Sum256(raw)) != ref.Digest || !json.Valid(raw) {
		return nil, fmt.Errorf("the Flow artifact differs from its accepted digest")
	}
	return raw, nil
}

func (s FrameStore) Discard(ref platform.FlowStateArtifact) error {
	key, err := s.key(ref)
	if err != nil {
		return err
	}
	if err := s.Files.Delete(context.Background(), key); err != nil {
		return fmt.Errorf("discard Flow artifact: %w", err)
	}
	return nil
}

const orphanRetention = 24 * time.Hour
const maxInventoryFiles = 65536
const maxReferenceBytes = 256 << 20

type FrameCollector struct {
	gate            sync.RWMutex
	recoveredBefore time.Time
}

func (c *FrameCollector) Preparing() func() {
	c.gate.RLock()
	return c.gate.RUnlock
}

func (c *FrameCollector) Recovered(started time.Time) {
	c.gate.Lock()
	defer c.gate.Unlock()
	c.recoveredBefore = started.UTC()
}

func (s FrameStore) References(images func(visit func(json.RawMessage) error) error) (map[string]bool, error) {
	keep := map[string]bool{}
	remaining := maxReferenceBytes
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
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if !json.Valid(raw) {
			return fmt.Errorf("Flow artifact reference image is invalid JSON")
		}
		if err := decoder.Decode(&value); err != nil {
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
					key, err := s.key(ref)
					if err != nil {
						return err
					}
					keep[key] = true
					if len(keep) > maxInventoryFiles {
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
						key, err := s.key(ref)
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
	if err := images(func(raw json.RawMessage) error { return visit(raw, 0) }); err != nil {
		return nil, err
	}
	return keep, nil
}

func inventoryKey(key, prefix string) bool {
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

func (c *FrameCollector) Collect(ctx context.Context, s FrameStore, now time.Time, quarantined func() bool, references func() (map[string]bool, error)) error {
	if !c.gate.TryLock() {
		return nil
	}
	defer c.gate.Unlock()
	started := c.recoveredBefore
	if started.IsZero() || now.IsZero() || quarantined() {
		return nil
	}
	inventory, ok := s.Files.(platform.FileInventory)
	if !ok {
		return fmt.Errorf("Flow artifact file inventory is unavailable")
	}
	cutoff := now.Add(-orphanRetention)
	if recoveryCutoff := started.Add(-time.Second); recoveryCutoff.Before(cutoff) {
		cutoff = recoveryCutoff
	}
	prefix := s.Tenant + "/artifacts/flow/"
	var candidates []platform.StoredFile
	cursor := ""
	seen := 0
	for {
		page, err := inventory.Inventory(ctx, prefix, cursor, platform.MaxFileInventoryPage)
		if err != nil {
			return err
		}
		if len(page.Files) > platform.MaxFileInventoryPage {
			return fmt.Errorf("Flow artifact inventory exceeded its page bound")
		}
		previous := cursor
		for _, file := range page.Files {
			if file.Key <= previous || !strings.HasPrefix(file.Key, prefix) || file.Modified.IsZero() || file.Size < 0 {
				return fmt.Errorf("Flow artifact inventory returned an invalid page")
			}
			previous = file.Key
			seen++
			if seen > maxInventoryFiles {
				return fmt.Errorf("Flow artifact inventory exceeds its complete-read budget; no files deleted")
			}
			if !file.Modified.After(cutoff) && inventoryKey(file.Key, prefix) {
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
	keep, err := references()
	if err != nil {
		return err
	}
	for _, file := range candidates {
		if err := ctx.Err(); err != nil {
			return err
		}
		if quarantined() {
			return fmt.Errorf("Flow artifact collection stopped for a quarantined tenant")
		}
		if keep[file.Key] {
			continue
		}
		if err := s.Files.Delete(ctx, file.Key); err != nil {
			return fmt.Errorf("discard orphan Flow artifact: %w", err)
		}
	}
	return nil
}
