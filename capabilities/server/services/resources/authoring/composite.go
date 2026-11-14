package build

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
)

type CompositeRefusal string

func (r CompositeRefusal) Error() string { return string(r) }

func CompositeFailure(err error) map[string]any {
	failure := map[string]any{"error": err.Error()}
	var refusal CompositeRefusal
	if errors.As(err, &refusal) {
		failure["outcome"] = "refused"
	}
	return failure
}

func CompositeAnswerOf(saved acceptedformat.Composite) (platform.CompositeAnswer, error) {
	if saved.Refusal != "" {
		return platform.CompositeAnswer{}, CompositeRefusal(saved.Refusal)
	}
	return platform.CompositeAnswer{Key: strings.TrimPrefix(saved.Key, "composite:"), Applied: len(saved.Edits), Digest: saved.Digest}, nil
}

type CompositeDraft struct {
	Decide         func(m platform.Member, app platform.App, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error)
	AnswerRequests func(at time.Time) *kernel.Error
	StageObservers func() *kernel.Error
	BatchResult    func(receipt *pb.ChangeRecord, at time.Time) ([]byte, error)
}

type CompositePlan struct {
	Tenant      string
	Admits      func(platform.Member) *kernel.Error
	Hosted      func() bool
	Quarantined func() bool
	PriorAnswer func(key string) json.RawMessage
	PriorDigest func(key string) (string, bool)
	Receipt     func(id string) *pb.ChangeRecord
	Begin       func() CompositeDraft
	ActionOwner func(schema string) platform.App
	Bound       func(platform.Member, *pb.Submission) *kernel.Error
}

type CompositePrepared struct {
	Saved acceptedformat.Composite
	Raw   []byte
}

func (p CompositePlan) Prepare(m platform.Member, key string, edits []platform.CompositeEdit, now time.Time) (platform.CompositeAnswer, *CompositePrepared, error) {
	if err := p.Admits(m); err != nil {
		return platform.CompositeAnswer{}, nil, err
	}
	if key == "" || len(key) > 200 || len(edits) == 0 || len(edits) > 64 || now.IsZero() {
		return platform.CompositeAnswer{}, nil, fmt.Errorf("a composite needs an idempotency key and one to 64 edits")
	}
	if !p.Hosted() {
		return platform.CompositeAnswer{}, nil, fmt.Errorf("this tenant runs no builder")
	}
	if p.Quarantined() {
		return platform.CompositeAnswer{}, nil, fmt.Errorf("tenant is quarantined")
	}
	saved := acceptedformat.Composite{Version: 2, Kind: "composite-result", Tenant: p.Tenant, App: ID, Member: m.ID,
		Key: "composite:" + key, At: now.UTC(), Edits: edits}
	var err error
	saved.RequestHash, err = acceptedformat.CompositeRequestHash(p.Tenant, m.ID, saved.Key, edits)
	if err != nil {
		return platform.CompositeAnswer{}, nil, err
	}
	if prior := p.PriorAnswer(saved.Key); len(prior) > 0 {
		answer, err := acceptedformat.DecodeComposite(prior)
		if err != nil || answer.RequestHash != saved.RequestHash {
			return platform.CompositeAnswer{}, nil, fmt.Errorf("composite key belongs to another request")
		}
		reply, err := CompositeAnswerOf(answer)
		return reply, nil, err
	}
	if digest, exists := p.PriorDigest(saved.Key); exists {
		for i, edit := range edits {
			x := p.Receipt(key + "#" + strconv.Itoa(i)).GetSubmission()
			if x.GetPrincipalId() != m.ID || x.GetSchema().GetName() != edit.Schema || !proto.Equal(x.GetTarget(), edit.Target) || !acceptedformat.SameCompositePayload(x.GetPayload(), edit.Payload) {
				return platform.CompositeAnswer{}, nil, fmt.Errorf("historical composite key belongs to another request")
			}
		}
		return platform.CompositeAnswer{Key: key, Applied: len(edits), Digest: digest}, nil, nil
	}
	draft := p.Begin()
	var receipt *pb.ChangeRecord
	for i, edit := range edits {
		app := p.ActionOwner(edit.Schema)
		if app == nil || app.Manifest().ID != ID {
			saved.Refusal = fmt.Sprintf("edit %d: %s is not a builder action", i+1, edit.Schema)
			break
		}
		sub := &pb.Submission{TenantId: p.Tenant, PrincipalId: m.ID, Authority: ID, IdempotencyKey: key + "#" + strconv.Itoa(i), Target: edit.Target, Schema: &pb.SchemaRef{Name: edit.Schema, Version: 1}, Payload: edit.Payload, ExpectedRevision: edit.ExpectedRevision}
		if refusal := p.Bound(m, sub); refusal != nil {
			saved.Refusal = fmt.Sprintf("edit %d: %s refused (%s): %s", i+1, edit.Schema, refusal.Error(), refusal.Message)
			break
		}
		r, refusal := draft.Decide(m, app, sub, now)
		if refusal != nil {
			saved.Refusal = fmt.Sprintf("edit %d: %s refused (%s): %s", i+1, edit.Schema, refusal.Error(), refusal.Message)
			break
		}
		receipt = r
	}
	if saved.Refusal == "" {
		if refusal := draft.AnswerRequests(now); refusal != nil {
			saved.Refusal = fmt.Sprintf("%s: %s", refusal.Error(), refusal.Message)
		}
	}
	if saved.Refusal == "" {
		if refusal := draft.StageObservers(); refusal != nil {
			saved.Refusal = fmt.Sprintf("%s: %s", refusal.Error(), refusal.Message)
		}
	}
	if saved.Refusal == "" {
		saved.Changes, err = draft.BatchResult(receipt, now)
		if err != nil {
			return platform.CompositeAnswer{}, nil, err
		}
	}
	raw, err := acceptedformat.EncodeComposite(saved)
	if err != nil {
		return platform.CompositeAnswer{}, nil, err
	}
	return platform.CompositeAnswer{}, &CompositePrepared{Saved: saved, Raw: raw}, nil
}

type CompositeApply struct {
	Tenant      string
	PriorDigest func(key string) (string, bool)
	ApplyBatch  func(changes []byte) error
	Remember    func(platform.AuditEntry)
	Save        func(key, digest string, raw []byte)
	Enqueue     func(time.Time)
}

func (a CompositeApply) Apply(raw []byte) (acceptedformat.Composite, error) {
	saved, err := acceptedformat.DecodeComposite(raw)
	if err != nil {
		return saved, err
	}
	if saved.Tenant != a.Tenant {
		return saved, fmt.Errorf("composite result belongs to another tenant")
	}
	if prior, ok := a.PriorDigest(saved.Key); ok {
		if prior != saved.Digest {
			return saved, fmt.Errorf("composite idempotency key changed")
		}
		return saved, nil
	}
	if saved.Refusal == "" {
		if err := a.ApplyBatch(saved.Changes); err != nil {
			return saved, err
		}
		a.Remember(platform.AuditEntry{At: saved.At, Member: saved.Member, App: ID, Action: "composite", Target: saved.Key})
	}
	a.Save(saved.Key, saved.Digest, raw)
	a.Enqueue(saved.At)
	return saved, nil
}
