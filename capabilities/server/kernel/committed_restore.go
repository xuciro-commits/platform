package kernel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
)

type ReleaseSnapshot struct {
	Requests, Applied map[string]string
	Candidates        map[string]json.RawMessage
	Active            string
}

func (q *Sequence) RestoreCommitted(s CommittedState, releases ReleaseSnapshot, apps []platform.App, appOf func(string) platform.App, inputs func(string) platform.AcceptedInputApp) error {
	for key, binding := range s.SubmissionSchemas {
		app, request, _ := strings.Cut(key, "/")
		owner := appOf(app)
		if owner == nil || request == "" || binding.Check() != nil {
			return fmt.Errorf("tenant %s: schema snapshot %s is incompatible", q.ID, key)
		}
		receipt := owner.AcceptedLedger().AcceptedFor(q.ID, request)
		sub := receipt.GetSubmission()
		if raw := s.AcceptedAnswers[key]; len(raw) > 0 {
			result, answer, err := acceptedformat.DecodeBatch(raw)
			if err != nil {
				return fmt.Errorf("tenant %s: schema snapshot %s has an invalid answer", q.ID, key)
			}
			sub, err = acceptedformat.BatchSubmission(result, answer)
			if err != nil || sub.GetIdempotencyKey() != request || result.App != app {
				return fmt.Errorf("tenant %s: schema snapshot %s has no matching request", q.ID, key)
			}
		}
		if sub == nil || acceptedformat.ValidateSubmissionSchemaVersion(sub, &binding) != nil {
			return fmt.Errorf("tenant %s: schema snapshot %s has no matching receipt", q.ID, key)
		}
	}
	for key, result := range s.Refusals {
		raw, err := json.Marshal(result)
		if err != nil {
			return fmt.Errorf("tenant %s: refusal snapshot %s: %w", q.ID, key, err)
		}
		decoded, sub, err := acceptedformat.DecodeRefusal(raw)
		if err != nil || decoded.Tenant != q.ID || key != decoded.App+"/"+sub.GetIdempotencyKey() {
			return fmt.Errorf("tenant %s: refusal snapshot %s is incompatible: %v", q.ID, key, err)
		}
	}
	for key, hash := range releases.Requests {
		if len(hash) != 64 || releases.Applied[key] == "" {
			return fmt.Errorf("release request snapshot %s is incompatible", key)
		}
	}
	for key, raw := range s.CompositeAnswers {
		r, err := acceptedformat.DecodeComposite(raw)
		if err != nil || r.Version < 2 || r.Tenant != q.ID || r.Key != key || s.Composites[key] != r.Digest {
			return fmt.Errorf("tenant %s: composite answer snapshot %s is incompatible", q.ID, key)
		}
		if r.Refusal == "" {
			batch, _, _ := acceptedformat.DecodeBatch(r.Changes)
			for _, decision := range batch.Decisions {
				receipt := new(pb.ChangeRecord)
				if protojson.Unmarshal(decision.Receipt, receipt) != nil {
					return fmt.Errorf("invalid composite snapshot receipt")
				}
				owner := appOf(decision.Event.App)
				if owner == nil || !proto.Equal(owner.AcceptedLedger().AcceptedFor(q.ID, receipt.GetSubmission().GetIdempotencyKey()), receipt) {
					return fmt.Errorf("composite snapshot has no accepted receipt")
				}
			}
		}
	}
	if len(s.DecisionDefinitions) > 0 {
		receipts := map[string]*pb.ChangeRecord{}
		for _, app := range apps {
			for _, receipt := range app.AcceptedLedger().RecordsFor(q.ID) {
				receipts[app.Manifest().ID+"/"+receipt.GetChangeId()] = receipt
			}
		}
		for key, definition := range s.DecisionDefinitions {
			receipt := receipts[key]
			if definition == nil || receipt == nil || definition.Check(receipt.GetSubmission().GetAuthority(), receipt.GetSubmission().GetSchema().GetName()) != nil || definition.Contract.Family != receipt.GetSubmission().GetSchema().GetVersion() {
				return fmt.Errorf("tenant %s: decision definition snapshot %s has no matching receipt", q.ID, key)
			}
		}
	}
	q.Committed.Restore(s)
	for key, raw := range s.AcceptedAnswers {
		if strings.HasPrefix(key, "release:") {
			result, err := acceptedformat.DecodeRelease(raw)
			if err != nil || (result.Version != 3 && result.Version != 4) || result.Tenant != q.ID || key != "release:"+result.Key || releases.Applied[result.Key] != result.Digest || !bytes.Equal(releases.Candidates[result.CandidateID], result.Bytes) {
				return fmt.Errorf("tenant %s: release answer snapshot %s is incompatible", q.ID, key)
			}
			continue
		}
		result, receipt, err := acceptedformat.DecodeBatch(raw)
		sub, subErr := acceptedformat.BatchSubmission(result, receipt)
		if err != nil || subErr != nil || result.Tenant != q.ID ||
			!result.HasSavedAnswer(sub) || key != result.App+"/"+sub.GetIdempotencyKey() {
			return fmt.Errorf("tenant %s: accepted answer snapshot %s is incompatible", q.ID, key)
		}
		owner := appOf(receipt.GetSubmission().GetAuthority())
		if owner == nil || !proto.Equal(owner.AcceptedLedger().AcceptedFor(q.ID, receipt.GetSubmission().GetIdempotencyKey()), receipt) {
			return fmt.Errorf("tenant %s: accepted answer snapshot %s has no receipt", q.ID, key)
		}
		if _, refused := q.Committed.Refusal(key); refused {
			return fmt.Errorf("tenant %s: accepted answer snapshot %s reuses a refused key", q.ID, key)
		}
		binding, bound := s.SubmissionSchemas[key]
		if bound != (result.SchemaVersion != nil) || result.SchemaVersion != nil && binding != *result.SchemaVersion {
			return fmt.Errorf("tenant %s: accepted answer snapshot %s has a different schema binding", q.ID, key)
		}
	}
	for key, raw := range s.AcceptedInputs {
		result, err := acceptedformat.DecodeInput(raw)
		owner := inputs(result.App)
		if err != nil || owner == nil || result.Tenant != q.ID ||
			key != result.App+"/"+result.Key || !slices.Contains(owner.AcceptedInputs(), result.Name) {
			return fmt.Errorf("tenant %s: accepted input snapshot %s is incompatible: %v", q.ID, key, err)
		}
	}
	for id, raw := range releases.Candidates {
		if _, err := platform.ReadCandidate(id, raw); err != nil {
			return fmt.Errorf("tenant %s: release snapshot %s is incompatible: %w", q.ID, id, err)
		}
	}
	if releases.Active != "" && releases.Candidates[releases.Active] == nil {
		return fmt.Errorf("tenant %s: active release snapshot has no saved candidate", q.ID)
	}
	return nil
}
