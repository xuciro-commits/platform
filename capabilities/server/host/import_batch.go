package host

import (
	"context"
	"fmt"
	"platformserver/kernel"
	"platformserver/kernel/protocol"
	persistence "platformserver/kernel/store"
	"platformserver/platform"
	releaseowner "platformserver/services/devops/releases"
	"platformserver/services/objects"
	build "platformserver/services/resources/authoring"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

const importBatchRows = 128

type preparedImport struct {
	rows  []protocol.ImportRow
	batch []persistence.AcceptedAppend
}

func (t *Tenant) importBatches(m platform.Member, info platform.RecordTypeInfo, file objects.ImportFile, now time.Time) ([]protocol.ImportRow, *platform.Error) {
	rows := make([]protocol.ImportRow, 0, len(file.Lines))
	for start := 0; start < len(file.Lines); start += importBatchRows {
		group := objects.ImportFile{File: file.File, Lines: file.Lines[start:min(start+importBatchRows, len(file.Lines))]}
		result, refusal := decideOnImage(t, context.Background(), "import",
			func(scope *Tenant) (preparedImport, *platform.Error) { return scope.prepareImport(m, info, group, now) },
			func(t *Tenant, _ preparedAccepted, value preparedImport, refusal *platform.Error) (preparedImport, *platform.Error) {
				if refusal == nil && len(value.batch) != 0 {
					refusal = t.settleImport(m, value.batch, now)
				}
				return value, refusal
			})
		if refusal != nil {
			return nil, refusal
		}
		rows = append(rows, result.rows...)
	}
	return rows, nil
}

func (t *Tenant) retainImportChanges(retain bool) {
	for _, app := range t.apps {
		app.Authority().RetainAcceptedChanges(retain)
	}
}

func (t *Tenant) finishImportResult(m platform.Member, item persistence.AcceptedAppend, raw []byte, settlement kernel.Settlement) (*pb.ChangeRecord, *platform.Error) {
	return t.FinishCommitted(t.readContext(), t.App(item.Entry.App), m, item.Hash, raw, kernel.CommittedResult{
		Settlement: settlement, App: t.App, Batch: t.batchApply(), Record: t.recordApply(), Publish: t.publishAccepted,
	})
}

func (scope *Tenant) prepareImport(m platform.Member, info platform.RecordTypeInfo, file objects.ImportFile, now time.Time) (out preparedImport, refusal *platform.Error) {
	if err := scope.Admits(m); err != nil {
		return out, err
	}
	scope.readScope.definitions = releaseowner.NewDefinitionPlanCache(scope.definitionsFingerprint(), scope.Projection.LiveVersion(build.ResourceType))
	scope.AuditLog.SetSink(nil, scope.readOrigin.AuditLog.ReadPersonal)
	scope.retainImportChanges(true)
	outer := scope.preparedResult
	defer func() { scope.preparedResult = outer }()
	var fault error
	stage := kernel.Settlement{
		Quarantine: func(err error) { fault = err }, Applied: func() {},
		Enqueue: scope.enqueue, ChangedOwner: func(string) {}, OperationsChanged: func(...string) {},
	}
	out.rows = scope.importRows(m, info, file, now, func(sub *pb.Submission) (*pb.ChangeRecord, *platform.Error, []platform.FieldIssue) {
		if fault != nil {
			return nil, kernel.CommitRefusal(fault), nil
		}
		var decision preparedAccepted
		scope.preparedResult = &decision
		answer, rejected := scope.prepareSubmission(m, sub, now, submitOptions{})
		scope.preparedResult = outer
		if len(decision.entry.Body) == 0 {
			return answer.record, rejected, answer.issues
		}
		outer.uploads = append(outer.uploads, decision.uploads...)
		item := persistence.AcceptedAppend{Entry: decision.entry, Key: decision.key, Hash: decision.hash}
		result, rejected := scope.finishImportResult(m, item, item.Entry.Body, stage)
		if fault == nil {
			out.batch = append(out.batch, item)
		}
		return result, rejected, answer.issues
	})
	if fault != nil {
		return preparedImport{}, kernel.CommitRefusal(fault)
	}
	return out, scope.recordReadRefusal()
}

func (t *Tenant) settleImport(m platform.Member, batch []persistence.AcceptedAppend, now time.Time) (refusal *platform.Error) {
	durable := false
	defer func() {
		if p := recover(); p != nil {
			err := fmt.Errorf("import batch: %v", p)
			if durable {
				t.quarantine(err)
			}
			refusal = kernel.CommitRefusal(err)
		}
	}()
	if err := t.Sequence.SettleAcceptedBatch(batch, now, t.settlement(),
		func() { durable = true; t.retainImportChanges(true) },
		func() { t.retainImportChanges(false) },
		func(item persistence.AcceptedAppend, raw []byte, settle kernel.Settlement) {
			t.finishImportResult(m, item, raw, settle)
		}, t.quarantined); err != nil {
		return kernel.CommitRefusal(err)
	}
	return nil
}
