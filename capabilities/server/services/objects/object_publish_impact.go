package objects

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/services/ontology"
)

type ObjectPublishField struct {
	Field   string `json:"field"`
	Change  string `json:"change" enum:"removed,typeChanged"`
	From    string `json:"from"`
	To      string `json:"to,omitempty"`
	Records int    `json:"records"`
}

type ObjectPublishImpact struct {
	Revision uint32               `json:"revision"`
	Records  int                  `json:"records"`
	Fields   []ObjectPublishField `json:"fields"`
}

func (t *Owner) PublishImpact(c platform.Caller, draftID string, now time.Time) (ObjectPublishImpact, *platform.Error) {
	out := ObjectPublishImpact{Fields: []ObjectPublishField{}}
	base, refusal := t.readRecordBase(c.Member, ontology.ObjectType, draftID, now)
	if refusal != nil {
		return out, refusal
	}
	draft := base.view.Record.(ontology.Object)
	out.Revision = draft.Revision
	if draft.Published == "" {
		return out, nil
	}
	changes, err := ontology.DraftChanges(draft)
	if err != nil {
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Published object definition is unavailable")
	}
	for _, item := range changes {
		name, isField := strings.CutPrefix(item.Path, "fields.")
		if !isField || name == "$order" || item.Change == "added" {
			continue
		}
		var from, to ontology.Field
		if json.Unmarshal(item.From, &from) != nil || item.To != nil && json.Unmarshal(item.To, &to) != nil {
			return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Published object definition is unavailable")
		}
		switch {
		case item.Change == "removed":
			out.Fields = append(out.Fields, ObjectPublishField{Field: name, Change: "removed", From: from.Type})
		case from.Type != to.Type:
			out.Fields = append(out.Fields, ObjectPublishField{Field: name, Change: "typeChanged", From: from.Type, To: to.Type})
		}
	}
	reader, refusal := t.newSetRead(t.Store, c, now)
	if refusal != nil {
		return out, refusal
	}
	node, refusal := reader.prepare(platform.ObjectSet{Object: draft.Installed, Archived: true, AsOf: &platform.BusinessTime{All: true}}, false, nil)
	if refusal != nil {
		return out, refusal
	}
	t.Store.Lock()
	defer t.Store.Unlock()
	plan, refusal := reader.compile(node)
	if refusal != nil {
		return out, refusal
	}
	read := reader.types[draft.Installed]
	fields := make([]string, len(out.Fields))
	for i, field := range out.Fields {
		fields[i] = field.Field
	}
	result := out.Fields[:0]
	for _, field := range out.Fields {
		if _, ok := read.view.Info().Field(field.Field); ok {
			result = append(result, field)
		}
	}
	out.Fields = result
	authorize := t.AuthorizeRecordSet(t.Store, reader.c, now)
	question := platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "read", Type: draft.Installed, Fields: fields}
	t.Store.VisitPlan(read.view, plan.plans[0], read.visible, func(record reflect.Value) {
		question.Record = records.Header(record).ID
		decision := authorize(question)
		if !decision.Allow {
			return
		}
		out.Records++
		for i, field := range out.Fields {
			declared, _ := read.view.Info().Field(field.Field)
			if !slices.Contains(decision.Withheld, field.Field) && !record.FieldByIndex(declared.Index).IsZero() {
				out.Fields[i].Records++
			}
		}
	})
	return out, nil
}
