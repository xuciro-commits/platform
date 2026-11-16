package platform

import "fmt"

func (b PageEventBinding) Navigation() *PageNavigation {
	for _, effect := range b.Effects {
		if effect.Kind == "navigate" {
			return effect.Navigate
		}
	}
	return nil
}

func (b PageEventBinding) Variables() []string {
	var ids []string
	for _, effect := range b.Effects {
		switch effect.Kind {
		case "set":
			ids = append(ids, effect.Target)
		case "navigate":
			if effect.Navigate != nil {
				for _, id := range effect.Navigate.Results {
					ids = append(ids, id)
				}
			}
		}
	}
	return ids
}

func (b PageEventBinding) Reads() []string {
	var ids []string
	for _, effect := range b.Effects {
		switch effect.Kind {
		case "set":
			if effect.From != "" {
				ids = append(ids, effect.From)
			}
		case "action":
			if effect.Action != nil && effect.Action.RecordVariable != "" {
				ids = append(ids, effect.Action.RecordVariable)
			}
			if effect.Action != nil {
				for _, arg := range effect.Action.Inputs {
					if arg.Variable != "" {
						ids = append(ids, arg.Variable)
					}
				}
			}
		case "navigate":
			if effect.Navigate != nil {
				for _, arg := range effect.Navigate.Inputs {
					if arg.Variable != "" {
						ids = append(ids, arg.Variable)
					}
				}
			}
		}
	}
	return ids
}

func (p Page) RecordVariableObject(variable string) string {
	return p.RecordResourceObject(variable).Name
}

func CheckPageNavigation(source Page, target Page, nav PageNavigation) error {
	sourceSchema, targetSchema := source.InputSchema, target.InputSchema
	version := 0
	inputs, outputs := map[string]PagePort{}, map[string]PagePort{}
	if targetSchema.Interface != nil {
		version = targetSchema.Interface.Version
		inputs = targetSchema.Interface.Inputs
		outputs = targetSchema.Interface.Outputs
	}
	if nav.InterfaceVersion != version {
		return fmt.Errorf("page navigation to %s interface version differs", nav.Page)
	}
	for id, port := range inputs {
		if _, ok := nav.Inputs[id]; port.Required && !ok {
			return fmt.Errorf("page navigation to %s misses input %s", nav.Page, id)
		}
	}
	for id, value := range nav.Inputs {
		port, ok := inputs[id]
		typ := PageLiteralType(value.Literal)
		if value.Variable != "" {
			typ = sourceSchema.Variables[value.Variable].Type
		}
		if !ok || typ != port.Type || port.Type == "object-set" && (port.Object == nil || source.CollectionInputObject(value.Variable) != *port.Object || !PageUIProfileSupports(sourceSchema.ProfileVersion(), "platform.page.v2.85")) || port.Type == "record" && (port.Object == nil || source.RecordVariableObject(value.Variable) != port.Object.Name) {
			return fmt.Errorf("page navigation input %s type or object differs", id)
		}
	}
	for id, variable := range nav.Results {
		port, ok := outputs[id]
		v := sourceSchema.Variables[variable]
		if !ok || !v.IsWritable() || v.Type != port.Type || v.Type == "record" {
			return fmt.Errorf("page navigation output %s type differs", id)
		}
	}
	return nil
}

func (d PageInputSchema) sharedContextRecord(id string) bool {
	v := d.Variables[id]
	return v.Type == "record" && v.Scope == "application" && v.Mode == "shared" && v.Source != nil && v.Source.Kind == "application" && v.Source.Object != nil && v.Source.Object.Kind == AssetObject && v.Source.Object.Check() == nil
}
