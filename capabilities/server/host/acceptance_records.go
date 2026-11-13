package host

import (
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	runtimekernel "platformserver/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/services/actions"
	releaseowner "platformserver/services/devops/releases"
	"platformserver/services/resources/authoring"
	"time"
)

func (d *stagedDecision) approvals() runtimekernel.ApprovalRequests {
	return runtimekernel.ApprovalRequests{Tenant: d.tenant.ID, Runtime: d, App: d.tenant.App, Decide: d.Decide, Probe: d.ProbeDecision}
}

func (d *stagedDecision) RequestApproval(c platform.Caller, app string, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return d.approvals().Request(d.ProbeOnly, c, app, s, now)
}

func (d *stagedDecision) batchResult(app string, request *pb.Submission, receipt *pb.ChangeRecord, at time.Time) ([]byte, error) {
	return d.BatchResult(d.sources(), app, request, receipt, at)
}

func (t *Tenant) applyAcceptedBatch(l *platform.Ledger, raw []byte) (bool, error) {
	return t.ApplyBatch(l, raw, t.batchApply())
}

func (t *Tenant) batchApply() runtimekernel.BatchApply {
	h := runtimekernel.BatchApply{App: t.App, OwnerLedger: runtimekernel.LedgerOf(t.App), CheckBindings: build.CheckRunApplicationBindings, DefinitionType: build.AppType,
		Claims: t.Claims.BatchPrepare(t.ID),
		Rows: func(draft *records.Store, result acceptedformat.Batch) error {
			return actions.CallbackRows{
				Answerer: func(app string) bool { return t.answerers[app] != nil },
				Pending:  func(id string) platform.Effect { queued, _ := t.outbound.Get(id); return queued.Effect },
				Prepare:  t.prepareAcceptedDirectRows,
			}.Apply(draft, result.DirectRows, result.At)
		},
		Install: runtimekernel.BatchInstall{
			Definitions: func(draft *records.Store, definitions []platform.ReleaseInstallation) (*records.Store, func() error, error) {
				installation, err := t.prepareBatchDefinitions(draft, definitions)
				if err != nil {
					return nil, nil, err
				}
				return installation.Projection, func() error { return t.installReleaseDefinitions(installation, definitions) }, nil
			},
			Publication: t.installPublicationLinkConstraint}.Install,
		Installed: t.Claims.BatchInstalled(t.ID),
	}
	return runtimekernel.BatchState{App: t.App, States: t.states, Notices: &t.Notices, Sequences: &t.Sequences,
		CheckControl: t.validateAcceptedControl, ApplyControl: t.applyAcceptedControl, CheckIntents: t.validateAcceptedIntents, InstallIntents: t.installAcceptedIntents,
		ApplyNotices: t.applyAcceptedNotices, CheckOperations: t.operationCancellations().Validate, ApplyOperations: t.operationCancellations().Apply}.Hooks(h)
}

func (t *Tenant) installReleaseDefinitions(installed *Tenant, definitions []platform.ReleaseInstallation) error {
	t.setDefinitions(installed.definitions)
	t.owner = maps.Clone(installed.owner)
	registry, err := t.assetRegistryLocked()
	if err != nil {
		return err
	}
	return releaseowner.ApplyInstallations(registry, definitions)
}

func (t *Tenant) publishAcceptedBatch(result acceptedformat.Batch) {
	runtimekernel.PublishBatch(result, t.publishAccepted)
}
