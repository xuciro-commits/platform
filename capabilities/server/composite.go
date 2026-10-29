package platformserver

import (
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/proto"

	acceptedformat "platformserver/kernel/acceptedformat"

	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"platformserver/studio/build"
)

func (t *Tenant) Composite(m platform.Member, key string, edits []platform.CompositeEdit, now time.Time) (platform.CompositeAnswer, error) {
	if err := t.admits(m); err != nil {
		return platform.CompositeAnswer{}, err
	}
	if key == "" || len(key) > 200 || len(edits) == 0 || len(edits) > 64 || now.IsZero() {
		return platform.CompositeAnswer{}, fmt.Errorf("a composite needs an idempotency key and one to 64 edits")
	}
	if t.app(build.ID) == nil {
		return platform.CompositeAnswer{}, fmt.Errorf("this tenant runs no builder")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.quarantined() {
		return platform.CompositeAnswer{}, fmt.Errorf("tenant is quarantined")
	}
	saved := acceptedformat.Composite{Version: 2, Kind: "composite-result", Tenant: t.ID, App: build.ID, Member: m.ID,
		Key: "composite:" + key, At: now.UTC(), Edits: edits}
	var err error
	saved.RequestHash, err = acceptedformat.CompositeRequestHash(t.ID, m.ID, saved.Key, edits)
	if err != nil {
		return platform.CompositeAnswer{}, err
	}
	if prior := t.committed.CompositeAnswer(saved.Key); len(prior) > 0 {
		answer, err := acceptedformat.DecodeComposite(prior)
		if err != nil || answer.RequestHash != saved.RequestHash {
			return platform.CompositeAnswer{}, fmt.Errorf("composite key belongs to another request")
		}
		return compositeAnswer(answer)
	}
	if digest, exists := t.committed.Composite(saved.Key); exists {
		for i, edit := range edits {
			r := t.app(build.ID).AcceptedLedger().AcceptedFor(t.ID, key+"#"+strconv.Itoa(i))
			x := r.GetSubmission()
			if x.GetPrincipalId() != m.ID || x.GetSchema().GetName() != edit.Schema || !proto.Equal(x.GetTarget(), edit.Target) || !acceptedformat.SameCompositePayload(x.GetPayload(), edit.Payload) {
				return platform.CompositeAnswer{}, fmt.Errorf("historical composite key belongs to another request")
			}
		}
		return platform.CompositeAnswer{Key: key, Applied: len(edits), Digest: digest}, nil
	}
	draft := t.newStagedDecision()
	var receipt *pb.ChangeRecord
	for i, edit := range edits {
		app := t.owner["action:"+edit.Schema]
		if app == nil || app.Manifest().ID != build.ID {
			saved.Refusal = fmt.Sprintf("edit %d: %s is not a builder action", i+1, edit.Schema)
			break
		}
		sub := &pb.Submission{TenantId: t.ID, PrincipalId: m.ID, Authority: build.ID, IdempotencyKey: key + "#" + strconv.Itoa(i), Target: edit.Target, Schema: &pb.SchemaRef{Name: edit.Schema, Version: 1}, Payload: edit.Payload}
		if refusal := t.delegatedBound(m, sub); refusal != nil {
			saved.Refusal = fmt.Sprintf("edit %d: %s refused (%s): %s", i+1, edit.Schema, refusal.Error(), refusal.Message)
			break
		}
		r, refusal := platform.Decide(platform.NewCaller(draft, m, build.ID, false, false), app, sub, now)
		if refusal != nil {
			saved.Refusal = fmt.Sprintf("edit %d: %s refused (%s): %s", i+1, edit.Schema, refusal.Error(), refusal.Message)
			break
		}
		receipt = r
	}
	if saved.Refusal == "" {
		if refusal := draft.answerRequests(now); refusal != nil {
			saved.Refusal = fmt.Sprintf("%s: %s", refusal.Error(), refusal.Message)
		}
	}
	if saved.Refusal == "" {
		if refusal := draft.stageObservers(); refusal != nil {
			saved.Refusal = fmt.Sprintf("%s: %s", refusal.Error(), refusal.Message)
		}
	}
	if saved.Refusal == "" {
		saved.Changes, err = draft.batchResult(build.ID, receipt.GetSubmission(), receipt, now)
		if err != nil {
			return platform.CompositeAnswer{}, err
		}
	}
	raw, err := acceptedformat.EncodeComposite(saved)
	if err != nil {
		return platform.CompositeAnswer{}, err
	}
	principal, _ := json.Marshal(m)
	committed, err := t.acceptResult(Entry{App: build.ID, Kind: "accepted-result", Principal: principal, Body: raw, At: now, Versions: draft.flowVersions()}, saved.Key, saved.RequestHash)
	if err != nil {
		return platform.CompositeAnswer{}, err
	}
	return t.finishCommittedComposite(m, saved, committed)
}

func (t *Tenant) finishCommittedComposite(m platform.Member, saved acceptedformat.Composite, committed []byte) (answer platform.CompositeAnswer, err error) {
	defer func() {
		if failure := recover(); failure != nil {
			err = fmt.Errorf("apply committed composite: %v", failure)
			t.quarantine(err)
			answer = platform.CompositeAnswer{}
		}
	}()
	applied, err := t.applyAcceptedComposite(committed)
	if err != nil || applied.Key != saved.Key || applied.RequestHash != saved.RequestHash || applied.Member != m.ID {
		t.quarantine(fmt.Errorf("committed composite result differs: %v", err))
		return platform.CompositeAnswer{}, fmt.Errorf("committed composite result differs: %v", err)
	}
	t.acceptedResultApplied()
	t.changed()
	return compositeAnswer(applied)
}

type compositeRefusal string

func (r compositeRefusal) Error() string { return string(r) }

func compositeAnswer(saved acceptedformat.Composite) (platform.CompositeAnswer, error) {
	if saved.Refusal != "" {
		return platform.CompositeAnswer{}, compositeRefusal(saved.Refusal)
	}
	return platform.CompositeAnswer{Key: strings.TrimPrefix(saved.Key, "composite:"), Applied: len(saved.Edits), Digest: saved.Digest}, nil
}

func (t *Tenant) stageCompositeLocked(m platform.Member, edits []platform.CompositeEdit, key string, now time.Time) (*Tenant, error) {
	owner, ok := t.app(build.ID).(build.ConstructionDrafts)
	if !ok {
		return nil, fmt.Errorf("this tenant runs no builder")
	}
	draft := (hostView{t: t, app: owner}).installationDraft()
	for i, edit := range edits {
		app := t.owner["action:"+edit.Schema]
		if app == nil || app.Manifest().ID != build.ID {
			return nil, fmt.Errorf("edit %d: %s is not a builder action", i+1, edit.Schema)
		}
		sub := &pb.Submission{TenantId: t.ID, PrincipalId: m.ID, Authority: build.ID,
			IdempotencyKey: key + "#" + strconv.Itoa(i), Target: edit.Target,
			Schema: &pb.SchemaRef{Name: edit.Schema, Version: 1}, Payload: edit.Payload}
		if err := t.delegatedBound(m, sub); err != nil {
			return nil, fmt.Errorf("edit %d: %s refused (%s)", i+1, edit.Schema, err.Error())
		}
		if _, err := app.Submit(platform.NewCaller(runtime{draft}, m, build.ID, true, false), sub, now); err != nil {
			return nil, fmt.Errorf("edit %d: %s refused (%s)", i+1, edit.Schema, err.Error())
		}
	}
	return draft, nil
}

func (t *Tenant) applyAcceptedComposite(raw []byte) (acceptedformat.Composite, error) {
	saved, err := acceptedformat.DecodeComposite(raw)
	if err != nil {
		return saved, err
	}
	if saved.Tenant != t.ID {
		return saved, fmt.Errorf("composite result belongs to another tenant")
	}
	if prior, ok := t.committed.Composite(saved.Key); ok {
		if prior != saved.Digest {
			return saved, fmt.Errorf("composite idempotency key changed")
		}
		return saved, nil
	}
	if saved.Version >= 2 {
		if saved.Refusal == "" {
			batch, _, _ := acceptedformat.DecodeBatch(saved.Changes)
			if _, err := t.applyAcceptedBatch(t.app(build.ID).AcceptedLedger(), saved.Changes); err != nil {
				return saved, err
			}
			t.publishAcceptedBatch(batch)
			t.audit.Remember(platform.AuditEntry{At: saved.At, Member: saved.Member, App: build.ID, Action: "composite", Target: saved.Key})
		}
		t.committed.SaveComposite(saved.Key, saved.Digest)
		t.committed.SaveCompositeAnswer(saved.Key, raw)
		t.enqueue(saved.At)
		return saved, nil
	}
	draft, err := t.stageCompositeLocked(platform.Member{ID: saved.Member, Tenant: t.ID, Roles: t.rolesOf(saved.Member)}, saved.Edits, saved.Key, saved.At)
	if err != nil {
		return saved, fmt.Errorf("committed composite does not apply: %w", err)
	}
	if err := t.records.PromoteRecords(draft.records); err != nil {
		return saved, err
	}
	t.setDefinitions(draft.definitions)
	t.committed.SaveComposite(saved.Key, saved.Digest)
	t.audit.Remember(platform.AuditEntry{At: saved.At, Member: saved.Member, App: build.ID, Action: "composite", Target: saved.Key})
	return saved, nil
}

func (t *Tenant) rolesOf(member string) map[string]string {
	if m, ok := t.Member(member); ok {
		return m.Roles
	}
	return map[string]string{}
}
