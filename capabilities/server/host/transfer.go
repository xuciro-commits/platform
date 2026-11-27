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
		return objects.ParseImport(view.Info(), data, currency, t.objectOwner(t.Projection).Referrer(m, now, t.i18n.Translations), t.i18n.Translations)
	})
	if err != nil {
		return nil, err
	}
	if !preview {
		return t.importBatches(m, view.Info(), parsed, now)
	}
	return t.importRows(m, view.Info(), parsed, now, func(s *pb.Submission) (*pb.ChangeRecord, *platform.Error, []platform.FieldIssue) {
		return t.probeDiagnosed(m, s, now)
	}), nil
}

func (t *Tenant) importRows(m platform.Member, info platform.RecordTypeInfo, parsed objects.ImportFile, now time.Time, submit func(*pb.Submission) (*pb.ChangeRecord, *platform.Error, []platform.FieldIssue)) []protocol.ImportRow {
	return objects.Importer{Tenant: t.ID, Member: m.ID, Submit: submit,
		Verb: func(key, id string) string {
			verb, _ := recordRead(t, context.Background(), func(t *Tenant) (string, *platform.Error) {
				app := t.App(info.App)
				if app == nil {
					return ".create", nil
				}
				ledger := app.Authority()
				if ledger.AcceptedFor(t.ID, key) == nil && (ledger.AcceptedFor(t.ID, key+":edit") != nil || t.readable(m, info.Type+"/"+id, now)) {
					return ".edit", nil
				}
				return ".create", nil
			})
			return verb
		}}.Rows(info, parsed)
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
