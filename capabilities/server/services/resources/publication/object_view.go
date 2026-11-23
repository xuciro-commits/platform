package publication

import (
	"encoding/json"
	"fmt"
)

type ViewField struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Ref  string `json:"ref,omitempty"`
}

type defaultViewSpec struct {
	Layout string `json:"layout"`
	Dates  string `json:"dates"`
	Until  string `json:"until"`
	After  string `json:"after"`
}

func CheckObjectView(objectType string, fields []ViewField, view json.RawMessage) error {
	if len(view) == 0 {
		return nil
	}
	var spec defaultViewSpec
	if err := json.Unmarshal(view, &spec); err != nil || spec.Layout != "gantt" {
		return nil
	}
	byName := make(map[string]ViewField, len(fields))
	for _, field := range fields {
		byName[field.Name] = field
	}
	checkDate := func(role, name string) error {
		if name == "" {
			return fmt.Errorf("Gantt view on %s requires a %s date field", objectType, role)
		}
		field, ok := byName[name]
		if !ok {
			return fmt.Errorf("Gantt view on %s references unknown %s field %q", objectType, role, name)
		}
		if field.Type != "date" && field.Type != "datetime" {
			return fmt.Errorf("Gantt view on %s requires %s field %q to be date or datetime, got %s", objectType, role, name, field.Type)
		}
		return nil
	}
	if err := checkDate("start (dates)", spec.Dates); err != nil {
		return err
	}
	if err := checkDate("end (until)", spec.Until); err != nil {
		return err
	}
	if spec.After != "" {
		field, ok := byName[spec.After]
		if !ok {
			return fmt.Errorf("Gantt view on %s references unknown dependency field %q in after", objectType, spec.After)
		}
		if field.Type != "reference" || field.Ref != objectType {
			return fmt.Errorf("Gantt view on %s requires after field %q to reference %s", objectType, spec.After, objectType)
		}
	}
	return nil
}

func CheckObjectAssetView(body json.RawMessage) error {
	var object struct {
		Type      string          `json:"type"`
		Name      string          `json:"name"`
		Namespace string          `json:"namespace"`
		Fields    []ViewField     `json:"fields"`
		View      json.RawMessage `json:"view"`
	}
	if err := json.Unmarshal(body, &object); err != nil {
		return nil
	}
	name := object.Type
	if name == "" {
		if object.Namespace != "" {
			name = object.Namespace + "." + object.Name
		} else {
			name = object.Name
		}
	}
	return CheckObjectView(name, object.Fields, object.View)
}
