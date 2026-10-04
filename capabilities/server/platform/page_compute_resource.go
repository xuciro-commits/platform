package platform

import (
	"fmt"
	"slices"
)

// PageComputeResource retains an ordinary operation contract and its original
// record input. It grants no access and stores no business fields or result.
type PageComputeResource struct {
	Operation      AssetBinding       `json:"operation"`
	RecordVariable string             `json:"recordVariable"`
	Inputs         map[string]Binding `json:"inputs"`
	OutputField    string             `json:"outputField,omitempty"`
}

func (d *PageDocument) checkComputeResource(v PageVariable) error {
	c := v.Source.Compute
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.ComputeResource.RequiredUIProfile) || !slices.Contains([]string{"number", "decimal"}, v.Type) || v.Mode != "resource" || v.Writable || !slices.Contains([]string{"page", "overlay"}, v.Scope) || c == nil || !pageNodeID.MatchString(c.RecordVariable) || c.Operation.Ref.Kind != AssetCompute || c.Operation.Ref.Check() != nil || c.Operation.SourceVersion == "" || len(c.Inputs) == 0 || len(c.Inputs) > 64 || c.OutputField != "" && !pageNodeID.MatchString(c.OutputField) {
		return fmt.Errorf("compute resource needs its finite typed operation and original record")
	}
	parent := d.Variables[c.RecordVariable]
	if parent.Type != "record" || !(d.sharedContextRecord(c.RecordVariable) || parent.Mode == "resource" && parent.Source != nil && parent.Source.Kind == "record" && parent.Scope == v.Scope && parent.Owner == v.Owner) {
		return fmt.Errorf("compute input needs its confirmed record in the original owner")
	}
	subject := false
	for name, b := range c.Inputs {
		if !pageNodeID.MatchString(name) || b.Check() != nil || b.Source != "literal" && b.Source != "subject" || b.Source == "subject" && (len(b.Path) != 1 || !pageNodeID.MatchString(b.Path[0])) {
			return fmt.Errorf("compute input needs a scalar source field or typed literal")
		}
		subject = subject || b.Source == "subject"
	}
	if !subject {
		return fmt.Errorf("record computation needs an original protected source field")
	}
	s := v.Source
	if s.Section != "" || s.Node != "" || s.Query != "" || s.Variable != "" || s.Object != nil || s.Field != "" || len(s.Fields) > 0 || s.Measure != "" || s.Port != "" {
		return fmt.Errorf("compute resource cannot carry another source")
	}
	return nil
}
func (p Page) ComputeResources() map[string]PageComputeResource {
	out := map[string]PageComputeResource{}
	if p.Document != nil {
		for id, v := range p.Document.Variables {
			if v.Source != nil && v.Source.Kind == "compute" && v.Source.Compute != nil {
				out[id] = *v.Source.Compute
			}
		}
	}
	return out
}
func (p Page) CheckComputeResource(c PageComputeResource, object EntityInfo, op Operation, valueType string) error {
	ref := p.RecordResourceObject(c.RecordVariable)
	if ref.App != object.App || ref.Name != object.Type || ref.Kind != AssetObject || op.Check() != nil || op.Name != c.Operation.Ref.Name || op.Input.Type != "object" || op.Input.Nullable {
		return fmt.Errorf("compute resource original object or operation differs")
	}
	for _, name := range op.Input.Required {
		if _, ok := c.Inputs[name]; !ok {
			return fmt.Errorf("compute input %s is missing", name)
		}
	}
	for name, b := range c.Inputs {
		schema, ok := op.Input.Properties[name]
		if !ok {
			return fmt.Errorf("compute input %s is unknown", name)
		}
		if b.Source == "literal" {
			if schema.Validate(b.Value, 64<<10) != nil {
				return fmt.Errorf("compute literal input differs")
			}
			continue
		}
		if b.Source != "subject" || len(b.Path) != 1 {
			return fmt.Errorf("compute source binding differs")
		}
		f, ok := object.Field(b.Path[0])
		if !ok {
			return fmt.Errorf("compute source field is unavailable")
		}
		typ := map[string]string{"text": "string", "longtext": "string", "choice": "string", "boolean": "boolean", "integer": "integer", "decimal": "number", "date": "string", "datetime": "string"}[f.Type]
		if typ == "" || schema.Type != typ && !(typ == "integer" && schema.Type == "number") {
			return fmt.Errorf("compute source field and input type differ")
		}
	}
	output := op.Output
	if c.OutputField != "" {
		if output.Type != "object" || output.Nullable || !slices.Contains(output.Required, c.OutputField) {
			return fmt.Errorf("compute scalar output must be required")
		}
		output = output.Properties[c.OutputField]
	}
	if !slices.Contains([]string{"number", "integer"}, output.Type) || output.Nullable || valueType == "decimal" && output.Type != "integer" {
		return fmt.Errorf("compute output needs a nonnullable numeric value")
	}
	return nil
}
