package records

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"reflect"
	"slices"
	"time"
)

type EnterpriseElements interface {
	Element(id string, now time.Time) (platform.ElementInfo, bool)
}

// Enterprise references resolve through the model rather than the record store.
// Lines carry the same declaration and must not bypass its validity/type check.
func CheckEnterpriseReferences(elements EnterpriseElements, replaying bool, fields []platform.FieldInfo, value, previous reflect.Value, now time.Time) *kernel.Error {
	if replaying {
		return nil
	}
	for _, f := range fields {
		v := value.FieldByIndex(f.Index)
		if v.Kind() == reflect.Pointer {
			if v.IsNil() {
				continue
			}
			v = v.Elem()
		}
		if f.Type == "lines" {
			used := map[int]bool{}
			for i := 0; i < v.Len(); i++ {
				if err := CheckEnterpriseReferences(elements, replaying, f.Fields, v.Index(i), PreviousLine(previous, f, v.Index(i), used), now); err != nil {
					return err
				}
			}
		}
		if f.Ref != "enterprise.element" || v.IsZero() {
			continue
		}
		if previous.IsValid() && reflect.DeepEqual(value.FieldByIndex(f.Index).Interface(), previous.FieldByIndex(f.Index).Interface()) {
			continue
		}
		el, ok := elements.Element(v.String(), now)
		if !ok {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_REFERENCE, "{field} names an enterprise element that is not active: {element}", f.Title, v.String())
		}
		families := slices.Clone(f.Stereotypes)
		if f.Stereotype != "" {
			families = append(families, f.Stereotype)
		}
		if len(families) > 0 && !slices.Contains(families, el.Stereotype) {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{field} names an enterprise element of an unsupported type", f.Title)
		}
	}
	return nil
}

func PreviousLine(record reflect.Value, field platform.FieldInfo, current reflect.Value, used map[int]bool) reflect.Value {
	if !record.IsValid() {
		return reflect.Value{}
	}
	lines := FieldValue(record, field.Index)
	if !lines.IsValid() || (lines.Kind() != reflect.Slice && lines.Kind() != reflect.Array) {
		return reflect.Value{}
	}
	// A line has no separate identity. Match its reference fields once, allowing
	// reorder or edits to other columns without allowing another retired use.
	for i := 0; i < lines.Len(); i++ {
		if used[i] {
			continue
		}
		same := true
		for _, column := range field.Fields {
			if column.Ref == "enterprise.element" && !reflect.DeepEqual(FieldValue(lines.Index(i), column.Index).Interface(), FieldValue(current, column.Index).Interface()) {
				same = false
				break
			}
		}
		if same {
			used[i] = true
			return lines.Index(i)
		}
	}
	return reflect.Value{}
}

// RefPath is one declared enterprise reference and where it sits.
type RefPath struct {
	Path       string
	Title      string
	Stereotype string
}

// DeclaresEnterpriseRef reports whether a type names enterprise elements, at
// any depth.
func DeclaresEnterpriseRef(fields []platform.FieldInfo) bool {
	for _, f := range fields {
		if len(f.Fields) > 0 && DeclaresEnterpriseRef(f.Fields) {
			return true
		}
		if f.Ref == "enterprise.element" {
			return true
		}
	}
	return false
}

// MatchingRefs are the declared references through which the record names the
// element; a "lines" field is descended into, and its path is dotted.
func MatchingRefs(fields []platform.FieldInfo, prefix string, v reflect.Value, element string, roles []string) []RefPath {
	return MatchingRefsWithAccess(fields, prefix, v, element, roles, false)
}

func MatchingRefsWithAccess(fields []platform.FieldInfo, prefix string, v reflect.Value, element string, roles []string, all bool) []RefPath {
	var out []RefPath
	for _, f := range fields {
		if !all && !f.ReadsAny(roles) {
			continue
		}
		path := f.Name
		if prefix != "" {
			path = prefix + "." + f.Name
		}
		fv := FieldValue(v, f.Index)
		if len(f.Fields) > 0 {
			if !fv.IsValid() || fv.Kind() != reflect.Slice && fv.Kind() != reflect.Array {
				continue
			}
			for i := 0; i < fv.Len(); i++ {
				out = append(out, MatchingRefsWithAccess(f.Fields, path, fv.Index(i), element, roles, all)...)
			}
			continue
		}
		if f.Ref != "enterprise.element" {
			continue
		}
		if s, ok := ReferenceValue(fv); ok && s == element {
			out = append(out, RefPath{Path: path, Title: f.Title, Stereotype: f.Stereotype})
		}
	}
	return out
}

// FieldValue reads a field, dereferencing the pointers on the way.
func FieldValue(v reflect.Value, index []int) reflect.Value {
	for _, at := range index {
		for v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return reflect.Value{}
			}
			v = v.Elem()
		}
		if v.Kind() != reflect.Struct || at >= v.NumField() {
			return reflect.Value{}
		}
		v = v.Field(at)
	}
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}
	return v
}

// ReferenceValue is a reference field's id: a plain string, or a reference
// type that names its target.
func ReferenceValue(v reflect.Value) (string, bool) {
	if !v.IsValid() {
		return "", false
	}
	if v.Kind() == reflect.String {
		return v.String(), true
	}
	if r, ok := v.Interface().(interface{ String() string }); ok {
		return r.String(), true
	}
	return "", false
}

// RecordLabel is the words a person recognises a record by: its first
// searchable text, else its id.
func RecordLabel(info platform.EntityInfo, value any, id string) string {
	v := reflect.ValueOf(value)
	for v.IsValid() && v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return id
		}
		v = v.Elem()
	}
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return id
	}
	for _, f := range info.Fields {
		if !f.Search {
			continue
		}
		fv := FieldValue(v, f.Index)
		if fv.IsValid() && fv.Kind() == reflect.String && fv.String() != "" {
			return fv.String()
		}
	}
	return id
}

// KnowledgeBearing says a record of this type may become a knowledge passage:
// a knowledge field of its own, the knowledge app's document, or a file that
// may be attached to either. The index re-cuts only these.
func KnowledgeBearing(info platform.EntityInfo) bool {
	return info.Type == platform.KnowledgeDocumentType || info.Type == platform.FileType || info.KnowledgeFiles ||
		slices.ContainsFunc(info.Fields, func(f platform.FieldInfo) bool { return f.Knowledge })
}
