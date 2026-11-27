package ontology

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/protocol"
	"platformserver/platform"
)

const referenceLimit = 500

const ReferenceGroup = 64

var referenceEvents = []string{SchemaPublish, "platform.package.install", "platform.package.upgrade", "platform.package.downgrade", "platform.package.rollback"}

var packageDemoEvents = []string{"platform.package.install", "platform.package.clear-demo"}

func PeopleSeeded(e platform.Event) (string, string, bool) {
	s := e.Record.GetSubmission()
	rest, seeded := strings.CutPrefix(strings.TrimSuffix(s.GetIdempotencyKey(), protocol.ContinuationSuffix), "seed:")
	pkg, _, found := strings.Cut(rest, ":")
	_, installer, by := strings.Cut(rest, "?by=")
	return pkg, installer, seeded && found && by && s.GetSchema().GetName() == "enterprise.element.add" && protocol.Continues(s.GetIdempotencyKey())
}

func DataContinues(e platform.Event) (string, bool) {
	key := e.Record.GetSubmission().GetIdempotencyKey()
	rest, data := strings.CutPrefix(key, "data:")
	pkg, _, found := strings.Cut(rest, ":")
	return pkg, data && found && protocol.Continues(key)
}

func ReferenceContinues(e platform.Event) bool {
	key := e.Record.GetSubmission().GetIdempotencyKey()
	return strings.HasPrefix(key, "reference:") && protocol.Continues(key)
}

func FillsReference(names []string) bool {
	return slices.ContainsFunc(names, func(name string) bool { return slices.Contains(referenceEvents, name) })
}

func HandlesPackageDemo(names []string) bool {
	return slices.ContainsFunc(names, func(name string) bool { return slices.Contains(packageDemoEvents, name) })
}

func CheckReferenceRecords(o *Object) *platform.Error {
	invalid := pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT
	if len(o.Records) > referenceLimit {
		return platform.Refuse(invalid, "An object type ships at most {count} reference records", referenceLimit)
	}
	seen := map[string]bool{}
	for _, raw := range o.Records {
		var row map[string]json.RawMessage
		var id string
		if json.Unmarshal(raw, &row) != nil || json.Unmarshal(row["id"], &id) != nil || id == "" || seen[id] {
			return platform.Refuse(invalid, "Each reference record is an object with its own id")
		}
		seen[id] = true
		for key, raw := range row {
			if key == "id" {
				continue
			}
			k := slices.IndexFunc(o.Fields, func(f Field) bool { return f.Name == key })
			if k < 0 {
				return platform.Refuse(invalid, "Reference record {id} names the unknown field {field}", id, key)
			}
			if !referenceValueFits(o.Fields[k].Type, raw) {
				return platform.Refuse(invalid, "Reference record {id} gives {field} a value that is not a {type}", id, key, o.Fields[k].Type)
			}
		}
	}
	return nil
}

func FillReference(c platform.Caller, lang string, model func(string) (reflect.Type, bool), submit func(*pb.Submission) *platform.Error) *platform.Error {
	found, _, refusal := platform.Find[Object](c, platform.Query{Limit: 500})
	if refusal != nil {
		return refusal
	}
	var pending []*pb.Submission
	for _, object := range found {
		if object.Archived || object.State != "published" || len(object.Records) == 0 {
			continue
		}
		typ := object.Type()
		target, installed := model(typ)
		if !installed {
			continue
		}
		app, _ := platform.Get[protocol.Application](c, object.Namespace)
		words := app.Languages[lang]
		for _, raw := range object.Records {
			var values map[string]any
			if json.Unmarshal(raw, &values) != nil {
				continue
			}
			id, _ := values["id"].(string)
			if _, exists := c.RecordValue(target, id); exists {
				continue
			}
			delete(values, "id")
			for _, field := range object.Fields {
				if text, ok := values[field.Name].(string); ok && words[text] != "" && (field.Type == "text" || field.Type == "longtext") {
					values[field.Name] = words[text]
				}
			}
			pending = append(pending, &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: AppID, IdempotencyKey: "reference:" + typ + "/" + id,
				Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: typ + ".create", Version: 1}, Payload: platform.Raw(values)})
		}
	}
	if len(pending) > ReferenceGroup {
		pending = pending[:ReferenceGroup]
		pending[len(pending)-1].IdempotencyKey += protocol.ContinuationSuffix
	}
	for _, s := range pending {
		if refusal := submit(s); refusal != nil {
			return refusal
		}
	}
	return nil
}

func FillSeed(c platform.Caller, pkgID string, seeds []json.RawMessage, demo bool, now time.Time, model func(string) (reflect.Type, bool), submit func(*pb.Submission) *platform.Error) *platform.Error {
	for _, raw := range seeds {
		var seed struct{ Kind, Type, ID string }
		if json.Unmarshal(raw, &seed) == nil && seed.Kind == "" && seed.Type == "enterprise.element" {
			if _, live := c.Element(seed.ID, now); !live {
				demo = false
			}
		}
	}
	filled := false
	for _, raw := range seeds {
		var seed struct {
			Kind   string                       `json:"kind"`
			Type   string                       `json:"type"`
			ID     string                       `json:"id"`
			Values json.RawMessage              `json:"values"`
			Rows   []map[string]json.RawMessage `json:"rows"`
			Then   []struct {
				Action string          `json:"action"`
				With   json.RawMessage `json:"with"`
			} `json:"then"`
		}
		if json.Unmarshal(raw, &seed) != nil || seed.Type == "" {
			continue
		}
		if seed.Kind == "data" {
			if filled {
				continue
			}
			var refusal *platform.Error
			if filled, refusal = fillData(c, pkgID, seed.Type, seed.Rows, model, submit); refusal != nil {
				return refusal
			}
			continue
		}
		if !demo || seed.ID == "" || seed.Kind == "cleared" {
			continue
		}
		target, installed := model(seed.Type)
		if !installed {
			continue
		}
		if _, exists := c.RecordValue(target, seed.ID); exists {
			continue
		}
		if refusal := submit(&pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: AppID, IdempotencyKey: "seed:" + pkgID + ":" + seed.Type + "/" + seed.ID,
			Target: &pb.EntityRef{Type: seed.Type, Id: seed.ID}, Schema: &pb.SchemaRef{Name: seed.Type + ".create", Version: 1}, Payload: seed.Values}); refusal != nil {
			return refusal
		}
		for _, step := range seed.Then {
			payload := step.With
			if len(payload) == 0 {
				payload = []byte("{}")
			}
			if refusal := submit(&pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: AppID, IdempotencyKey: "seed:" + pkgID + ":" + seed.Type + "/" + seed.ID + ":" + step.Action,
				Target: &pb.EntityRef{Type: seed.Type, Id: seed.ID}, Schema: &pb.SchemaRef{Name: seed.Type + "." + step.Action, Version: 1}, Payload: payload}); refusal != nil {
				return refusal
			}
		}
	}
	return nil
}

func fillData(c platform.Caller, pkgID, typ string, rows []map[string]json.RawMessage, model func(string) (reflect.Type, bool), submit func(*pb.Submission) *platform.Error) (bool, *platform.Error) {
	target, installed := model(typ)
	if !installed {
		return false, nil
	}
	var pending []*pb.Submission
	for _, row := range rows {
		var id string
		if json.Unmarshal(row["id"], &id) != nil || id == "" {
			continue
		}
		if _, exists := c.RecordValue(target, id); exists {
			continue
		}
		values := map[string]json.RawMessage{}
		for name, value := range row {
			if name != "id" {
				values[name] = value
			}
		}
		pending = append(pending, &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: AppID, IdempotencyKey: "data:" + pkgID + ":" + typ + "/" + id,
			Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: typ + ".create", Version: 1}, Payload: platform.Raw(values)})
	}
	if len(pending) == 0 {
		return false, nil
	}
	pending[len(pending)-1].IdempotencyKey += protocol.ContinuationSuffix
	for _, s := range pending {
		if refusal := submit(s); refusal != nil {
			return true, refusal
		}
	}
	return true, nil
}

func ClearSeed(c platform.Caller, pkgID string, seeds []json.RawMessage, model func(string) (reflect.Type, bool), submit func(*pb.Submission) *platform.Error) *platform.Error {
	for i := len(seeds) - 1; i >= 0; i-- {
		var seed struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		}
		if json.Unmarshal(seeds[i], &seed) != nil || seed.Type == "" || seed.ID == "" {
			continue
		}
		target, installed := model(seed.Type)
		if !installed {
			continue
		}
		val, exists := c.RecordValue(target, seed.ID)
		if !exists {
			continue
		}
		if archived := reflect.ValueOf(val).FieldByName("Archived"); archived.IsValid() && archived.Kind() == reflect.Bool && archived.Bool() {
			continue
		}
		if refusal := submit(&pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: AppID, IdempotencyKey: "clear-demo:" + pkgID + ":" + seed.Type + "/" + seed.ID,
			Target: &pb.EntityRef{Type: seed.Type, Id: seed.ID}, Schema: &pb.SchemaRef{Name: seed.Type + ".archive", Version: 1}, Payload: []byte("{}")}); refusal != nil {
			return refusal
		}
	}
	return nil
}

func referenceValueFits(kind string, raw json.RawMessage) bool {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	if value == nil {
		return true
	}
	switch kind {
	case "integer", "decimal":
		_, ok := value.(float64)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "text", "longtext", "choice", "reference", "member", "date", "datetime":
		_, ok := value.(string)
		return ok
	case "money":
		_, ok := value.(map[string]any)
		return ok
	}
	return true
}
