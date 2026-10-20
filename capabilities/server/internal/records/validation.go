package records

import (
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
)

type RecordLookup interface{ HasRecord(typ, id string) bool }

func CheckFields(info platform.EntityInfo, entity any, v reflect.Value, replaying bool, lookup RecordLookup) ([]platform.FieldIssue, *kernel.Error) {
	invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	// A conditional field (FieldInfo.When) follows its condition's value.
	stringOf := func(name string) string {
		i := slices.IndexFunc(info.Fields, func(x platform.FieldInfo) bool { return x.Name == name })
		if i < 0 {
			return ""
		}
		cv := v.FieldByIndex(info.Fields[i].Index)
		if cv.Kind() == reflect.Pointer {
			if cv.IsNil() {
				return ""
			}
			cv = cv.Elem()
		}
		if cv.Kind() == reflect.Bool {
			return strconv.FormatBool(cv.Bool())
		}
		return cv.String()
	}
	// Check the resulting record too: a partial edit must not bypass a
	// declared relationship by omitting its unchanged endpoint.
	if !replaying && slices.ContainsFunc(info.Fields, func(f platform.FieldInfo) bool { return f.Constraints != nil }) {
		raw, marshalErr := json.Marshal(entity)
		if marshalErr != nil {
			return nil, invalid
		}
		var values map[string]any
		if json.Unmarshal(raw, &values) != nil {
			return nil, invalid
		}
		fields := []platform.Field{}
		for _, f := range info.Fields {
			if f.When != nil && !f.Active(stringOf) {
				continue
			}
			// omitempty is a transport detail; a stored zero or false is a value.
			if _, present := values[f.Name]; !present {
				raw, err := json.Marshal(v.FieldByIndex(f.Index).Interface())
				if err != nil {
					return nil, invalid
				}
				var value any
				if json.Unmarshal(raw, &value) != nil {
					return nil, invalid
				}
				values[f.Name] = value
			}
			typ := map[string]string{"integer": "integer", "decimal": "number", "boolean": "boolean", "date": "date", "datetime": "datetime"}[f.Type]
			if typ == "" {
				typ = "string"
			}
			fields = append(fields, platform.Field{Name: f.Name, Type: typ, Required: f.Required, Description: f.Title, Constraints: f.Constraints, Choices: f.Choices})
		}
		if issues := platform.InputIssues(fields, values); len(issues) > 0 {
			return issues, nil
		}
	}
	for _, f := range info.Fields {
		fv := v.FieldByIndex(f.Index)
		if f.When != nil {
			on, _ := info.Field(f.When.Field)
			switch active := f.Active(stringOf); {
			case active && f.Required && f.Constraints == nil && fv.IsZero():
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{field} is required when {condition} is {values}", f.Title, on.Title, strings.Join(f.When.In, ", "))
			case !active && !fv.IsZero():
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{field} applies only when {condition} is {values}", f.Title, on.Title, strings.Join(f.When.In, ", "))
			case !active:
				continue
			}
		} else if f.Required && f.Constraints == nil && fv.IsZero() {
			return nil, invalid
		}
		if fv.Kind() == reflect.Pointer {
			if fv.IsNil() {
				continue
			}
			fv = fv.Elem()
		}
		switch f.Type {
		case "choice":
			if !fv.IsZero() && !slices.Contains(f.Choices, fv.String()) {
				return nil, invalid
			}
		}
		if l := info.Lifecycle; l != nil && f.Name == l.Field && !fv.IsZero() && !slices.ContainsFunc(l.States, func(st platform.State) bool { return st.Name == fv.String() }) {
			return nil, invalid // not a state of the lifecycle
		}
		switch f.Type {
		case "":
		case "json":
			raw, err := json.Marshal(fv.Interface())
			if err != nil {
				return nil, invalid
			}
			if _, err := platform.DecodeValue(raw, 64<<10); err != nil {
				return nil, invalid
			}
		case "date":
			if _, err := time.Parse(time.DateOnly, fv.String()); !fv.IsZero() && err != nil {
				return nil, invalid
			}
		case "text", "longtext":
			if f.Required && strings.TrimSpace(fv.String()) == "" {
				return nil, invalid
			}
		case "money":
			if m := fv.Interface().(platform.Money); m.Amount != 0 && len(m.Currency) != 3 {
				return nil, invalid
			}
		case "reference", "references":
			if f.Ref == "enterprise.element" {
				continue // checked through the directory, including nested lines
			}
			ids := []string{fv.String()}
			if f.Type == "references" {
				ids = ids[:0]
				for i := range fv.Len() {
					ids = append(ids, fv.Index(i).String())
				}
			}
			for _, id := range ids {
				if id != "" && !lookup.HasRecord(f.Ref, id) {
					return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_REFERENCE}
				}
			}
		}
	}
	return nil, nil
}
