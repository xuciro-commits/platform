package host

import (
	"context"
	"fmt"
	"platformserver/services/objects"
	controlowner "platformserver/services/security/control"
	"slices"
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
	view, _ := viewOf(m, et)
	currency := t.setting(t.automation(controlowner.PlatformApp, false), controlowner.SettingCurrency)
	file, lines, ok := objects.ParseImport(view.Info(), data, currency)
	if !ok {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	out := []platform.ImportRow{}
	for _, line := range lines {
		r := platform.ImportRow{Row: line.Row, ID: line.ID, Outcome: line.Outcome}
		if r.Outcome == "" {
			try := func(verb, key string) *kernel.Error {
				r.Error = nil
				r.Action = typ + verb
				if !slices.Contains(view.Info().Standard, r.Action) {
					return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
				}
				s := &pb.Submission{TenantId: t.ID, PrincipalId: m.ID, Authority: view.Info().App, IdempotencyKey: key,
					Target: &pb.EntityRef{Type: typ, Id: r.ID}, Schema: &pb.SchemaRef{Name: r.Action, Version: 1}, Payload: line.Payload}
				if preview {
					refusal, issues := t.probeDiagnosed(m, s, now)
					if refusal != nil {
						r.Error = &platform.ErrorBody{Code: refusal.Code.String(), Message: refusal.Message, Issues: issues}
					}
					return refusal
				}
				_, err, issues := t.submitDiagnosed(m, s, now)
				if err != nil {
					r.Error = &platform.ErrorBody{Code: err.Code.String(), Message: err.Message, Issues: issues}
				}
				return err
			}
			key := fmt.Sprintf("import:%s:%d", file, r.Row)
			verb, _ := recordRead(t, context.Background(), func(t *Tenant) (string, *kernel.Error) {
				ledger := t.App(view.Info().App).AcceptedLedger()
				if ledger.AcceptedFor(t.ID, key) != nil {
					return ".create", nil
				}
				if ledger.AcceptedFor(t.ID, key+":edit") != nil || t.readable(m, typ+"/"+r.ID, now) {
					return ".edit", nil
				}
				return ".create", nil
			})
			requestKey := key
			if verb == ".edit" {
				requestKey += ":edit"
			}
			err := try(verb, requestKey)
			if verb == ".create" && err != nil && err.Code == pb.ErrorCode_ERROR_CODE_CONFLICT {
				err = try(".edit", key+":edit")
			}
			r.Outcome = outcomeOf(err)
			if r.Error != nil && len(r.Error.Issues) > 0 && r.Error.Issues[0].Constraint != "" {
				r.Outcome, r.Error.Code = "CONSTRAINT_VIOLATION", "CONSTRAINT_VIOLATION"
			}
		}
		out = append(out, r)
	}
	return out, nil
}

func (t *Tenant) probeDiagnosed(m platform.Member, s *pb.Submission, now time.Time) (*kernel.Error, []platform.FieldIssue) {
	issues, refusal := recordRead(t, context.Background(), func(t *Tenant) ([]platform.FieldIssue, *kernel.Error) {
		app := t.owner["action:"+s.GetSchema().GetName()]
		if app == nil {
			return nil, unknown()
		}
		draft := t.newStagedDecision()
		draft.probing = true
		_, refusal := decideAcceptedAs(app, draft, m, s, now, false)
		return t.readableFieldIssues(m, draft.inputIssues[refusal], now), refusal
	})
	return refusal, issues
}

func (t *Tenant) exportRead(m platform.Member, typ string, q platform.Query, now time.Time) ([]byte, *kernel.Error) {
	m = t.authorizationMember(m)
	if err := t.Admits(m); err != nil {
		return nil, err
	}
	t.Projection.Lock()
	et := t.Projection.Type(typ)
	t.Projection.Unlock()
	if et == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	c := platform.NewCaller(runtime{t: t, scoped: true}, m, et.Info().App, false, false)
	if err := authorizationRefusal(t.authorizeFrom(t.Projection, c, platform.AuthzRequest{Principal: m.ID, Tenant: m.Tenant, Kind: "export", Type: typ}, now, false)); err != nil {
		return nil, err
	}
	view, _ := viewOf(m, et)
	export := objects.NewExport(view.Info())
	q.Offset, q.Limit = 0, 500
	for {
		page, err := t.recordsRead(m, typ, q, now)
		if err != nil {
			return nil, err
		}
		for _, r := range page.Records {
			export.Add(r)
		}
		q.Offset += len(page.Records)
		if len(page.Records) == 0 || q.Offset >= page.Total {
			break
		}
	}
	return export.Bytes(), nil
}
