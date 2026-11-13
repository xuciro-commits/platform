package objects

import (
	"encoding/json"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func DeclaredQueryFor(inputs json.RawMessage) (string, *kernel.Error) {
	values := struct {
		For string `json:"for,omitempty"`
	}{}
	if len(inputs) > 0 && string(inputs) != "null" {
		if _, err := platform.DecodeValue(inputs, 32<<10); err != nil {
			return "", platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		var keys map[string]json.RawMessage
		if err := json.Unmarshal(inputs, &keys); err != nil {
			return "", platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Query inputs must be an object")
		}
		for key := range keys {
			if key != "for" {
				return "", platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Unknown query parameter {field}", key)
			}
		}
		if err := json.Unmarshal(inputs, &values); err != nil {
			return "", platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Query record reference must be a string")
		}
	}
	return values.For, nil
}

func DeclaredQueryAnswer(c platform.Caller, q platform.NamedQuery, info platform.EntityInfo, found bool, records []any, total int) (json.RawMessage, *kernel.Error) {
	sources := []string{}
	for _, value := range records {
		if row, ok := value.(InterfaceRecord); ok {
			ref := row.Type + "/" + row.ID
			sources = append(sources, ref)
			for _, field := range q.InterfaceShape.Fields {
				sources = append(sources, ref+"#"+field.Name)
			}
			continue
		}
		row, _ := json.Marshal(value)
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(row, &fields)
		var id string
		_ = json.Unmarshal(fields["id"], &id)
		if id == "" {
			continue
		}
		ref := q.Object + "/" + id
		sources = append(sources, ref)
		if found {
			for _, field := range info.Fields {
				if field.ReadsAny(c.RolesIn(info.App), c.Rules()) {
					sources = append(sources, ref+"#"+field.Name)
				}
			}
		}
	}
	raw, err := json.Marshal(struct {
		Records []any    `json:"records"`
		Total   int      `json:"total"`
		Sources []string `json:"sources"`
	}{records, total, sources})
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	return raw, nil
}
