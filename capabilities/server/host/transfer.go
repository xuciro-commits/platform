package host

import (
	"context"
	"platformserver/services/objects"
	controlowner "platformserver/services/security/control"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (t *Tenant) Import(m platform.Member, typ string, data []byte, preview bool, now time.Time) ([]platform.ImportRow, *kernel.Error) {
	if err := t.Admits(m); err != nil {
		return nil, err
	}
	t.Projection.Lock()
	et := t.Projection.Type(typ)
	t.Projection.Unlock()
	if et == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	view, _ := objects.ViewOf(m, et)
	currency := t.setting(t.automation(controlowner.PlatformApp, false), controlowner.SettingCurrency)
	file, lines, ok := objects.ParseImport(view.Info(), data, currency)
	if !ok {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	return objects.Importer{Tenant: t.ID, Member: m.ID,
		Submit: func(s *pb.Submission) (*kernel.Error, []platform.FieldIssue) {
			if preview {
				return t.probeDiagnosed(m, s, now)
			}
			_, err, issues := t.submitDiagnosed(m, s, now)
			return err, issues
		},
		Verb: func(key, id string) string {
			verb, _ := recordRead(t, context.Background(), func(t *Tenant) (string, *kernel.Error) {
				ledger := t.App(view.Info().App).AcceptedLedger()
				if ledger.AcceptedFor(t.ID, key) != nil {
					return ".create", nil
				}
				if ledger.AcceptedFor(t.ID, key+":edit") != nil || t.readable(m, typ+"/"+id, now) {
					return ".edit", nil
				}
				return ".create", nil
			})
			return verb
		}}.Rows(view.Info(), file, lines), nil
}

func (t *Tenant) probeDiagnosed(m platform.Member, s *pb.Submission, now time.Time) (*kernel.Error, []platform.FieldIssue) {
	issues, refusal := recordRead(t, context.Background(), func(t *Tenant) ([]platform.FieldIssue, *kernel.Error) {
		app := t.App(t.owner["action:"+s.GetSchema().GetName()])
		if app == nil {
			return nil, unknown()
		}
		draft := t.newStagedDecision()
		draft.ProbeOnly = true
		_, refusal := decideAcceptedAs(app, draft, m, s, now, false)
		return t.readableFieldIssues(m, draft.InputIssues[refusal], now), refusal
	})
	return refusal, issues
}

func (t *Tenant) exportRead(m platform.Member, typ string, q platform.Query, now time.Time) ([]byte, *kernel.Error) {
	return t.objectOwner(t.Projection).ExportRead(m, typ, q, now)
}
