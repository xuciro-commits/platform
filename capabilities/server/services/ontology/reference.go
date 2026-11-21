package ontology

import (
	"encoding/json"
	"reflect"
	"slices"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/protocol"
	"platformserver/platform"
)

const referenceLimit = 200

var referenceEvents = []string{SchemaPublish, "platform.package.install", "platform.package.upgrade", "platform.package.downgrade", "platform.package.rollback"}

func FillsReference(names []string) bool {
	return slices.ContainsFunc(names, func(name string) bool { return slices.Contains(referenceEvents, name) })
}

func checkReferenceRecords(o *Object) *platform.Error {
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
		for key := range row {
			if key != "id" && !slices.ContainsFunc(o.Fields, func(f Field) bool { return f.Name == key }) {
				return platform.Refuse(invalid, "Reference record {id} names the unknown field {field}", id, key)
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
			if refusal := submit(&pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: AppID, IdempotencyKey: "reference:" + typ + "/" + id,
				Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: typ + ".create", Version: 1}, Payload: platform.Raw(values)}); refusal != nil {
				return refusal
			}
		}
	}
	return nil
}
