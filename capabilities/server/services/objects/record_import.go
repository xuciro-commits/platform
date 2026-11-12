package objects

import (
	"fmt"
	"slices"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type Importer struct {
	Tenant, Member string
	Submit         func(*pb.Submission) (*kernel.Error, []platform.FieldIssue)
	Verb           func(key, id string) string
}

func (i Importer) Rows(info platform.EntityInfo, file string, lines []ImportLine) []platform.ImportRow {
	out := []platform.ImportRow{}
	for _, line := range lines {
		r := platform.ImportRow{Row: line.Row, ID: line.ID, Outcome: line.Outcome}
		if r.Outcome == "" {
			try := func(verb, key string) *kernel.Error {
				r.Error = nil
				r.Action = info.Type + verb
				if !slices.Contains(info.Standard, r.Action) {
					return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
				}
				s := &pb.Submission{TenantId: i.Tenant, PrincipalId: i.Member, Authority: info.App, IdempotencyKey: key,
					Target: &pb.EntityRef{Type: info.Type, Id: r.ID}, Schema: &pb.SchemaRef{Name: r.Action, Version: 1}, Payload: line.Payload}
				err, issues := i.Submit(s)
				if err != nil {
					r.Error = &platform.ErrorBody{Code: err.Code.String(), Message: err.Message, Issues: issues}
				}
				return err
			}
			key := fmt.Sprintf("import:%s:%d", file, r.Row)
			verb := i.Verb(key, r.ID)
			requestKey := key
			if verb == ".edit" {
				requestKey += ":edit"
			}
			err := try(verb, requestKey)
			if verb == ".create" && err != nil && err.Code == pb.ErrorCode_ERROR_CODE_CONFLICT {
				err = try(".edit", key+":edit")
			}
			r.Outcome = "ok"
			if err != nil {
				r.Outcome = err.Code.String()
			}
			if r.Error != nil && len(r.Error.Issues) > 0 && r.Error.Issues[0].Constraint != "" {
				r.Outcome, r.Error.Code = "CONSTRAINT_VIOLATION", "CONSTRAINT_VIOLATION"
			}
		}
		out = append(out, r)
	}
	return out
}
