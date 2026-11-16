package platform

func (p Page) CollectionInputObject(variable string) AssetRef {
	return p.collectionInputObject(variable, map[string]bool{})
}

func (p Page) collectionInputObject(variable string, seen map[string]bool) AssetRef {
	if seen[variable] {
		return AssetRef{}
	}
	seen[variable] = true
	schema := p.InputSchema
	v := schema.Variables[variable]
	if v.Type != "object-set" {
		return AssetRef{}
	}
	if v.Mode == "input" && schema.Interface != nil {
		for _, port := range schema.Interface.Inputs {
			if port.Variable == variable && port.Object != nil {
				return *port.Object
			}
		}
	}
	if v.Source == nil {
		return AssetRef{}
	}
	if v.Source.Object != nil && v.Mode == "shared" {
		return *v.Source.Object
	}
	if v.Source.Kind == "plan" {
		return schema.Queries[v.Source.Query].Object
	}
	if v.Source.Kind == "query" {
		for _, binding := range p.DataBindings {
			if binding.ID == v.Source.Section {
				if source := binding.Ports["collectionVariable"]; source != "" && source != variable {
					return p.collectionInputObject(source, seen)
				}
				return binding.ObjectRef(p)
			}
		}
	}
	return AssetRef{}
}
