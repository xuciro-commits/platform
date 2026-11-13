package host

import (
	"encoding/json"
	"fmt"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
)

func (t *Tenant) Composite(m platform.Member, key string, edits []platform.CompositeEdit, now time.Time) (platform.CompositeAnswer, error) {
	return decideWithError(t, "composite", func(t *Tenant) (platform.CompositeAnswer, error) { return t.prepareComposite(m, key, edits, now) }, func(t *Tenant, candidate preparedAccepted) (platform.CompositeAnswer, error) {
		saved, err := acceptedformat.DecodeComposite(candidate.entry.Body)
		if err != nil {
			return platform.CompositeAnswer{}, err
		}
		committed, err := t.acceptResult(candidate.entry, candidate.key, candidate.hash)
		if err != nil {
			return platform.CompositeAnswer{}, err
		}
		return t.finishCommittedComposite(m, saved, committed)
	})
}

func (t *Tenant) prepareComposite(m platform.Member, key string, edits []platform.CompositeEdit, now time.Time) (platform.CompositeAnswer, error) {
	var draft *stagedDecision
	answer, prepared, err := (build.CompositePlan{
		Tenant: t.ID, Admits: t.Admits, Quarantined: t.quarantined, Bound: t.delegatedBound,
		Hosted:      func() bool { return t.App(build.ID) != nil },
		PriorAnswer: t.Committed.CompositeAnswer, PriorDigest: t.Committed.Composite,
		Receipt:     func(id string) *pb.ChangeRecord { return t.App(build.ID).AcceptedLedger().AcceptedFor(t.ID, id) },
		ActionOwner: func(schema string) platform.App { return t.App(t.owner["action:"+schema]) },
		Begin: func() build.CompositeDraft {
			draft = t.newStagedDecision()
			return build.CompositeDraft{
				Decide: func(m platform.Member, app platform.App, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
					return platform.Decide(platform.NewCaller(draft, m, build.ID, false, false), app, s, at)
				},
				AnswerRequests: draft.answerRequests, StageObservers: draft.stageObservers,
				BatchResult: func(receipt *pb.ChangeRecord, at time.Time) ([]byte, error) {
					return draft.batchResult(build.ID, receipt.GetSubmission(), receipt, at)
				},
			}
		},
	}).Prepare(m, key, edits, now)
	if err != nil || prepared == nil {
		return answer, err
	}
	saved := prepared.Saved
	principal, _ := json.Marshal(m)
	entry := Entry{App: build.ID, Kind: "accepted-result", Principal: principal, Body: prepared.Raw, At: now, Versions: draft.FlowVersions()}
	if t.preparedResult != nil {
		*t.preparedResult = preparedAccepted{entry: entry, key: saved.Key, hash: saved.RequestHash, uploads: t.preparedResult.uploads}
		return build.CompositeAnswerOf(saved)
	}
	committed, err := t.acceptResult(entry, saved.Key, saved.RequestHash)
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
	return build.CompositeAnswerOf(applied)
}

func (t *Tenant) applyAcceptedComposite(raw []byte) (acceptedformat.Composite, error) {
	return build.CompositeApply{
		Tenant: t.ID, PriorDigest: t.Committed.Composite, Remember: t.audit.Remember, Enqueue: t.enqueue,
		ApplyBatch: func(changes []byte) error {
			batch, _, _ := acceptedformat.DecodeBatch(changes)
			if _, err := t.applyAcceptedBatch(t.App(build.ID).AcceptedLedger(), changes); err != nil {
				return err
			}
			t.publishAcceptedBatch(batch)
			return nil
		},
		Save: func(key, digest string, raw []byte) {
			t.Committed.SaveComposite(key, digest)
			t.Committed.SaveCompositeAnswer(key, raw)
		},
	}.Apply(raw)
}
