package result

import (
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/internal/acceptance"
	"platformserver/internal/records"
	"platformserver/journal"
	"platformserver/platform"
	"slices"
	"strings"
	"time"
)

// Batch preserves one top-level input and all its nested decisions.
// Before validates predecessor images/history before authoritative application.
type Batch struct {
	Unique         []journal.StateUnique          `json:"unique,omitempty"`
	Business       []platform.BusinessRefusalFact `json:"business,omitempty"`
	SchemaVersion  *platform.SchemaVersion        `json:"schemaVersion,omitempty"`
	Authorizations []AuthorizationEvidence        `json:"authorizations,omitempty"`
	Version        int                            `json:"version"`
	Kind           string                         `json:"kind"`
	Tenant         string                         `json:"tenant"`
	App            string                         `json:"app"`
	At             time.Time                      `json:"at"`
	RequestHash    string                         `json:"requestHash"`
	Digest         string                         `json:"digest"`
	Receipt        json.RawMessage                `json:"receipt"`              // top-level answer/key
	Submission     json.RawMessage                `json:"submission,omitempty"` // original input, when answer is an approval receipt
	Rows           []BatchRow                     `json:"rows"`
	Decisions      []Decision                     `json:"decisions"`
	Sequences      map[string]int                 `json:"sequences,omitempty"`
	SequenceBases  map[string]int                 `json:"sequenceBases,omitempty"`
	Notices        *Notices                       `json:"notices,omitempty"`
	Intents        []platform.Effect              `json:"intents,omitempty"`
	Observations   []Observation                  `json:"observations,omitempty"`
	States         []State                        `json:"states,omitempty"`
	Deliveries     []ConnectorDelivery            `json:"deliveries,omitempty"`
	Operations     []OperationCancellation        `json:"operations,omitempty"`
	Control        *Control                       `json:"control,omitempty"`
	DirectRows     []CallbackRow                  `json:"directRows,omitempty"`
	Iterations     []platform.TxnIteration        `json:"iterations,omitempty"`
	Definitions    []ReleaseInstallation          `json:"definitions,omitempty"`
}

type ResultBudgetError struct{ Bytes, Limit int }

func (e *ResultBudgetError) Error() string {
	return fmt.Sprintf("record batch is %d bytes; limit is %d bytes", e.Bytes, e.Limit)
}

func DecodeBatch(raw []byte) (Batch, *pb.ChangeRecord, error) {
	var result Batch
	if len(raw) > acceptance.MaxResultBytes {
		return result, nil, &ResultBudgetError{len(raw), acceptance.MaxResultBytes}
	}
	if len(raw) == 0 {
		return result, nil, fmt.Errorf("record batch is empty")
	}
	if err := acceptance.DecodeJSON(raw, &result, "record batch"); err != nil {
		return result, nil, err
	}
	if err := ValidateAuthorizationEvidence(result.Tenant, result.Authorizations); err != nil {
		return result, nil, err
	}

	digest, err := DigestBatch(result)
	if err != nil || result.Digest != digest {
		return result, nil, fmt.Errorf("record batch content digest differs")
	}
	receipt := new(pb.ChangeRecord)
	if err := protojson.Unmarshal(result.Receipt, receipt); err != nil {
		return result, nil, fmt.Errorf("record batch receipt: %w", err)
	}
	sub, err := BatchSubmission(result, receipt)
	if err != nil {
		return result, nil, err
	}
	if result.Version < 1 || result.Version > 10 || result.Kind != "record-batch" || result.Tenant == "" || result.App == "" ||
		result.At.IsZero() || sub == nil || sub.GetTenantId() != result.Tenant ||
		sub.GetAuthority() != result.App || sub.GetPrincipalId() == "" || sub.GetIdempotencyKey() == "" ||
		sub.GetSchema() == nil || sub.GetTarget() == nil || receipt.GetChangeId() == "" ||
		receipt.GetRecordedTime() == nil || receipt.GetValidTime() == nil ||
		len(result.Decisions) == 0 {
		return result, nil, fmt.Errorf("record batch has inconsistent identity or effects")
	}
	if result.Version < 8 && (result.Version < 4 && (result.Version == 3) != (result.Control != nil) || result.Version < 5 && (result.Version == 4) != (len(result.DirectRows) > 0) || result.Version < 6 && (result.Version == 5) != (len(result.Iterations) > 0) || result.Version < 7 && (result.Version == 6) != (len(result.Definitions) > 0) || result.Version < 8 && (result.Version == 7) != (len(result.Business) > 0)) {
		return result, nil, fmt.Errorf("record batch control format differs")
	}
	if result.Version < 8 && len(result.Unique) > 0 {
		return result, nil, fmt.Errorf("legacy batch has unique-key metadata")
	}
	if len(result.Business) > 64 {
		return result, nil, fmt.Errorf("business result exceeds its evidence bound")
	}
	for _, f := range result.Business {
		if f.Level == "block" || !f.At.Equal(result.At) {
			return result, nil, fmt.Errorf("accepted business evidence is not a successful branch")
		}
		owner, _, _ := strings.Cut(f.Action, ".")
		if err := ValidateBusinessFact(owner, f); err != nil {
			return result, nil, err
		}
	}
	iterationOwner := result.App
	if result.Version >= 9 {
		if result.Version == 9 && len(result.Iterations) == 0 {
			return result, nil, fmt.Errorf("record batch omits its decision-bound iterations")
		}
		iterationOwner = ""
	}
	if err := ValidateTxnIterations(iterationOwner, result.Iterations); err != nil {
		return result, nil, err
	}
	if err := ValidateSubmissionSchemaVersion(sub, result.SchemaVersion); err != nil {
		return result, nil, err
	}
	hash, err := SubmissionHash(sub, result.SchemaVersion)
	if err != nil || result.RequestHash != hash {
		return result, nil, fmt.Errorf("record batch request digest differs")
	}
	changes := map[string]*pb.ChangeRecord{}
	written := map[string]bool{}
	top := false
	hasIntervals := false
	for _, decision := range result.Decisions {
		r := new(pb.ChangeRecord)
		if err := protojson.Unmarshal(decision.Receipt, r); err != nil {
			return result, nil, fmt.Errorf("record batch child receipt: %w", err)
		}
		s, e := r.GetSubmission(), decision.Event
		key := e.App + "/" + r.GetChangeId()
		if decision.Definition != nil {
			if observation := decision.Definition.Observation; observation != nil && observation.Intervals != nil {
				hasIntervals = true
				if result.Version < 10 {
					return result, nil, fmt.Errorf("legacy record batch contains state interval evidence")
				}
				if observation.Definition.Ref.App != e.App || observation.Record != s.GetTarget().GetType()+"/"+s.GetTarget().GetId() {
					return result, nil, fmt.Errorf("state interval evidence differs from its accepted subject")
				}
			}
			if err := decision.Definition.Check(e.App, s.GetSchema().GetName()); err != nil {
				return result, nil, err
			}
		}
		if s == nil || s.GetTenantId() != result.Tenant || s.GetAuthority() != e.App ||
			s.GetIdempotencyKey() == "" || s.GetPrincipalId() == "" || s.GetTarget() == nil || s.GetSchema() == nil ||
			r.GetChangeId() == "" || r.GetRecordedTime() == nil || r.GetValidTime() == nil ||
			e.App == "" || len(e.Names) == 0 || e.Names[0] != s.GetSchema().GetName() || changes[key] != nil {
			return result, nil, fmt.Errorf("record batch has an inconsistent child decision")
		}
		if e.Hops < 0 || e.Hops > MaxHops+1 || e.Hops > MaxHops && len(e.Subscribers) > 0 ||
			e.Hops <= MaxHops && len(e.Stopped) > 0 {
			return result, nil, fmt.Errorf("record batch has an invalid delivery hop boundary")
		}
		changes[key] = r
		top = top || proto.Equal(r, receipt)
		changed := map[string]bool{}
		for _, ref := range e.Changed {
			if ref == "" || changed[ref] {
				return result, nil, fmt.Errorf("record batch has duplicate or foreign changed records")
			}
			changed[ref], written[ref] = true, true
		}
		subscribers := map[string]bool{}
		for _, id := range e.Subscribers {
			if id == "" || subscribers[id] {
				return result, nil, fmt.Errorf("record batch has duplicate or empty subscriber")
			}
			subscribers[id] = true
		}
		for _, effect := range e.Effects {
			if effect.ID == "" || effect.Endpoint == "" || effect.Event == "" {
				return result, nil, fmt.Errorf("record batch has incomplete effect intent")
			}
		}
	}
	if result.Version >= 9 {
		for _, iteration := range result.Iterations {
			if changes[iteration.Decision] == nil {
				return result, nil, fmt.Errorf("transaction iteration has no matching accepted decision")
			}
		}
	}
	for _, fact := range result.Business {
		matched := false
		for _, decision := range result.Decisions {
			r := changes[decision.Event.App+"/"+ReceiptID(decision.Receipt)]
			if r != nil && fact.Member == r.GetSubmission().GetPrincipalId() && fact.Action == r.GetSubmission().GetSchema().GetName() && fact.Subject == r.GetSubmission().GetTarget().GetType()+"/"+r.GetSubmission().GetTarget().GetId() {
				matched = true
				break
			}
		}
		if !matched {
			return result, nil, fmt.Errorf("business evidence has no matching accepted decision")
		}
	}
	if result.Version == 10 && !hasIntervals {
		return result, nil, fmt.Errorf("record batch omits its state interval evidence")
	}
	if !top || len(written) != len(result.Rows) {
		return result, nil, fmt.Errorf("record batch omits the top-level receipt or a written row")
	}
	seen := map[string]bool{}
	for _, row := range result.Rows {
		ref := row.Type + "/" + row.ID
		if result.Version >= 8 {
			if row.HistoryRef == nil || row.HistoryBase != row.HistoryRef.Base {
				return result, nil, fmt.Errorf("record batch has no versioned history")
			}
			if err := records.ValidateHistoryRef(row.Image); err != nil {
				return result, nil, err
			}
		} else if row.HistoryRef != nil || row.Format != "" || row.Predecessor != "" {
			return result, nil, fmt.Errorf("legacy record batch carries new metadata")
		}

		if row.Type == "" || row.ID == "" || row.App == "" || len(row.Value) == 0 ||
			len(row.History) == 0 || row.HistoryBase < 0 || result.Version == 1 && row.HistoryBase != 0 || seen[ref] || !written[ref] {
			return result, nil, fmt.Errorf("record batch omits or duplicates a row")
		}
		seen[ref] = true
		last := row.History[len(row.History)-1]
		lastApp := row.App
		if len(row.Authorities) > 0 {
			lastApp = row.Authorities[len(row.Authorities)-1]
		}
		r := changes[lastApp+"/"+last.Change]
		if r == nil || last.Schema != r.GetSubmission().GetSchema().GetName() ||
			last.By != r.GetSubmission().GetPrincipalId() || !last.At.Equal(r.GetRecordedTime().AsTime()) {
			return result, nil, fmt.Errorf("record batch row history differs from its decision")
		}
	}
	for _, direct := range result.DirectRows {
		row := direct.Row
		if result.Version >= 8 {
			if err := records.ValidateHistoryRef(row.Image); err != nil {
				return result, nil, err
			}
		} else if row.HistoryRef != nil || row.Format != "" {
			return result, nil, fmt.Errorf("legacy callback row carries new metadata")
		}
		ref := row.Type + "/" + row.ID
		if seen[ref] || written[ref] || row.Type == "" || row.ID == "" || row.App == "" || direct.Source.ID == "" || direct.Source.App != row.App || len(row.History) == 0 || len(row.Value) == 0 {
			return result, nil, fmt.Errorf("record batch has invalid or overlapping callback rows")
		}
		seen[ref] = true
	}
	for key, n := range result.Sequences {
		app, _, ok := strings.Cut(key, "/")
		base, exists := result.SequenceBases[key]
		if !ok || !exists || base < 0 || n <= base ||
			!slices.ContainsFunc(result.Decisions, func(d Decision) bool { return d.Event.App == app }) {
			return result, nil, fmt.Errorf("record batch has invalid sequence allocation")
		}
	}
	if len(result.SequenceBases) != len(result.Sequences) {
		return result, nil, fmt.Errorf("record batch has unmatched sequence predecessors")
	}
	return result, receipt, nil
}

// HasSavedAnswer distinguishes a reply to an original request from an ordinary
// batch whose receipt already carries that request in its owner's ledger.
func (result Batch) HasSavedAnswer(submission *pb.Submission) bool {
	return len(result.Submission) > 0 || len(result.Business) > 0 || strings.HasPrefix(submission.GetCorrelationId(), "correction:")
}

func BatchSubmission(result Batch, receipt *pb.ChangeRecord) (*pb.Submission, error) {
	if len(result.Submission) == 0 {
		return receipt.GetSubmission(), nil
	}
	s := new(pb.Submission)
	if err := protojson.Unmarshal(result.Submission, s); err != nil {
		return nil, fmt.Errorf("record batch original submission: %w", err)
	}
	if receipt.GetSubmission().GetAuthority() != "work" ||
		receipt.GetSubmission().GetSchema().GetName() != "work.approval.request" {
		return nil, fmt.Errorf("record batch answer differs without an approval request")
	}
	answer := receipt.GetSubmission()
	var payload struct {
		Requester  string          `json:"requester"`
		Submission json.RawMessage `json:"submission"`
	}
	held := new(pb.Submission)
	if json.Unmarshal(answer.GetPayload(), &payload) != nil || protojson.Unmarshal(payload.Submission, held) != nil ||
		!proto.Equal(held, s) || payload.Requester != s.GetPrincipalId() || answer.GetPrincipalId() != "app:work" ||
		answer.GetTarget().GetType() != "work.approval" ||
		answer.GetTarget().GetId() != s.GetAuthority()+"."+s.GetIdempotencyKey() ||
		answer.GetIdempotencyKey() != "approval:"+answer.GetTarget().GetId() {
		return nil, fmt.Errorf("record batch approval does not hold its original submission")
	}
	return s, nil
}

func DigestBatch(result Batch) (string, error) {
	result.Digest = ""
	return acceptance.Digest(result)
}

func ReceiptID(raw json.RawMessage) string {
	r := new(pb.ChangeRecord)
	_ = protojson.Unmarshal(raw, r)
	return r.GetChangeId()
}
