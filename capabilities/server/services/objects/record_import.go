package objects

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"platformserver/kernel/protocol"
	"slices"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

type Importer struct {
	Tenant, Member string
	Submit         func(*pb.Submission) (*pb.ChangeRecord, *platform.Error, []platform.FieldIssue)
	Verb           func(key, id string) string
}

func (i Importer) Rows(info platform.RecordTypeInfo, parsed ImportFile) []protocol.ImportRow {
	editable := slices.Contains(info.Standard, info.Type+".edit")
	out := []protocol.ImportRow{}
	for _, line := range parsed.Lines {
		r := protocol.ImportRow{Row: line.Row, ID: line.ID, Outcome: line.Outcome}
		if line.Issue != nil {
			r.Error = &protocol.ErrorBody{Code: line.Outcome, Message: line.Issue.Message, Issues: []platform.FieldIssue{*line.Issue}}
		} else if r.Outcome == "" {
			try := func(verb, key string) *platform.Error {
				r.Error = nil
				r.Action = info.Type + verb
				if !slices.Contains(info.Standard, r.Action) {
					return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
				}
				s := &pb.Submission{TenantId: i.Tenant, PrincipalId: i.Member, Authority: info.App, IdempotencyKey: key,
					Target: &pb.EntityRef{Type: info.Type, Id: r.ID}, Schema: &pb.SchemaRef{Name: r.Action, Version: 1}, Payload: line.Payload}
				accepted, err, issues := i.Submit(s)
				if id := accepted.GetSubmission().GetTarget().GetId(); id != "" {
					r.ID = id
				}
				if err != nil {
					r.Error = &protocol.ErrorBody{Code: err.Code.String(), Message: err.Message, Issues: issues}
				}
				return err
			}
			key := fmt.Sprintf("import:%s:%d", parsed.File, r.Row)
			if r.ID == "" && info.Naming == "" {
				sum := sha256.Sum256([]byte(i.Tenant + "/" + info.Type + "/" + key))
				r.ID = hex.EncodeToString(sum[:16])
			}
			verb := ".create"
			if r.ID != "" && editable {
				verb = i.Verb(key, r.ID)
			}
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
