package organization

import (
	"encoding/json"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/protocol"
	"platformserver/platform"
)

const (
	packageInstall   = "platform.package.install"
	packageClearDemo = "platform.package.clear-demo"
)

func (e *Enterprise) AttachOwner(bindings protocol.OwnerBindings) {
	if bindings.Build != nil {
		e.seeds = bindings.Build.PackageSeed
	}
}

func (e *Enterprise) seedPeople(c platform.Caller, pkg, installer string, now time.Time, clear bool) *platform.Error {
	if e.seeds == nil {
		return nil
	}
	seeds, ok := e.seeds(pkg)
	if !ok {
		return nil
	}
	day := now.UTC().Format(time.DateOnly)
	var pending []*pb.Submission
	for _, raw := range seeds {
		var seed struct {
			Kind   string          `json:"kind"`
			Type   string          `json:"type"`
			ID     string          `json:"id"`
			Values json.RawMessage `json:"values"`
		}
		if json.Unmarshal(raw, &seed) != nil || seed.Kind != "" || seed.Type != ElementType || seed.ID == "" {
			continue
		}
		e.mu.Lock()
		exists, live := e.model.ElementOf(seed.ID) != nil, e.model.LiveOn(seed.ID, day)
		e.mu.Unlock()
		target := &pb.EntityRef{Type: ElementType, Id: seed.ID}
		if clear {
			if live {
				payload, _ := json.Marshal(map[string]string{"until": now.UTC().AddDate(0, 0, 1).Format(time.DateOnly), "reason": "Demo data cleared"})
				pending = append(pending, &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: ID, IdempotencyKey: "clear-demo:" + pkg + ":" + ElementType + "/" + seed.ID,
					Target: target, Schema: &pb.SchemaRef{Name: SchemaElementClose, Version: 1}, Payload: payload})
			}
			continue
		}
		if !exists {
			pending = append(pending, &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: ID, IdempotencyKey: "seed:" + pkg + ":" + ElementType + "/" + seed.ID,
				Target: target, Schema: &pb.SchemaRef{Name: SchemaElementAdd, Version: 1}, Payload: seed.Values})
		}
	}
	if !clear && len(pending) > 0 {
		pending[len(pending)-1].IdempotencyKey += "?by=" + installer + protocol.ContinuationSuffix
	}
	for _, s := range pending {
		if _, err := e.Submit(c, s, now); err != nil {
			return err
		}
	}
	return nil
}
