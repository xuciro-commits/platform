package host

import (
	"context"
	"platformserver/kernel/protocol"
	"platformserver/services/objects"
	controlowner "platformserver/services/security/control"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

func (t *Tenant) Import(m platform.Member, typ string, data []byte, preview bool, now time.Time) ([]protocol.ImportRow, *platform.Error) {
	if err := t.Admits(m); err != nil {
		return nil, err
	}
	t.Projection.Lock()
	et := t.Projection.Type(typ)
	t.Projection.Unlock()
	if et == nil {
		return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	view, _ := objects.ViewOf(m, et)
	currency := t.setting(t.automation(controlowner.PlatformApp, false), controlowner.SettingCurrency)
	parsed, err := recordRead(t, context.Background(), func(t *Tenant) (objects.ImportFile, *platform.Error) {
		return objects.ParseImport(view.Info(), data, currency, t.objectOwner(t.Projection).Referrer(m, now))
	})
	if err != nil {
		return nil, err
	}
	return objects.Importer{Tenant: t.ID, Member: m.ID,
		Submit: func(s *pb.Submission) (*pb.ChangeRecord, *platform.Error, []platform.FieldIssue) {
			if preview {
				return t.probeDiagnosed(m, s, now)
			}
			return t.submitDiagnosed(m, s, now)
		},
		Verb: func(key, id string) string {
			verb, _ := recordRead(t, context.Background(), func(t *Tenant) (string, *platform.Error) {
				ledger := t.App(view.Info().App).Authority()
				if ledger.AcceptedFor(t.ID, key) == nil && (ledger.AcceptedFor(t.ID, key+":edit") != nil || t.readable(m, typ+"/"+id, now)) {
					return ".edit", nil
				}
				return ".create", nil
			})
			return verb
		}}.Rows(view.Info(), parsed), nil
}

func (t *Tenant) probeDiagnosed(m platform.Member, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error, []platform.FieldIssue) {
	issues, refusal := recordRead(t, context.Background(), func(t *Tenant) ([]platform.FieldIssue, *platform.Error) {
		app := t.App(t.owner["action:"+s.GetSchema().GetName()])
		if app == nil {
			return nil, unknown()
		}
		draft := t.newStagedDecision()
		draft.ProbeOnly = true
		_, refusal := decideAcceptedAs(app, draft, m, s, now, false)
		return t.readableFieldIssues(m, draft.InputIssues[refusal], now), refusal
	})
	return nil, refusal, issues
}

func (t *Tenant) exportRead(m platform.Member, typ string, q platform.Query, now time.Time) ([]byte, *platform.Error) {
	return t.objectOwner(t.Projection).ExportRead(m, typ, q, now)
}
