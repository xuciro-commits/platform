package platform

import "fmt"

func (p Page) LoopFactor(owner string) (int, error) {
	factor := 1
	seen := map[string]bool{}
	for owner != "" {
		if seen[owner] {
			return 0, fmt.Errorf("page loop owner cycle")
		}
		seen[owner] = true
		found := false
		for _, scope := range p.ScopeVisibility.Scopes {
			if scope.ID != owner {
				continue
			}
			found = true
			if scope.Kind == "loop" {
				if scope.MaxIterations < 1 || scope.MaxIterations > PageWidgets.Runtime.Loop.MaxItems || factor > PageWidgets.Runtime.Query.MaxTotalLimit/scope.MaxIterations {
					return 0, fmt.Errorf("page loop read budget exceeded")
				}
				factor *= scope.MaxIterations
			}
			owner = scope.Parent
			break
		}
		if !found {
			return 0, fmt.Errorf("page loop owner is unavailable")
		}
	}
	return factor, nil
}

func (p Page) collectionBuilderInput(id, owner string, object AssetRef) bool {
	d := p.InputSchema
	sections := p.DataBindings
	if !PageUIProfileSupports(d.ProfileVersion(), "platform.page.v2.88") {
		return false
	}
	v := d.Variables[id]
	scope := "page"
	if owner != "" {
		scope = "overlay"
	}
	if v.Type != "object-set" || v.Mode != "resource" || v.Source == nil || v.Source.Kind != "query" || v.Owner != owner || v.Scope != scope || v.Writable || v.Source.Query != "" || v.Source.Variable != "" || v.Source.Node != "" || v.Source.Object != nil || len(v.Source.Fields) > 0 || v.Source.Measure != "" || v.Source.Field != "" || v.Source.Port != "" {
		return false
	}
	for _, s := range sections {
		if s.ID != v.Source.Section || s.Ports["collectionOutputVariable"] != id {
			continue
		}
		input := d.Variables[s.Ports["collectionVariable"]]
		if input.Source == nil || input.Source.Kind != "plan" {
			return false
		}
		q, ok := d.Queries[input.Source.Query]
		return ok && q.Owner == owner && q.Object == object
	}
	return false
}

func (p Page) checkCollectionBuilderGraph() error {
	d := p.InputSchema
	sections := p.DataBindings
	c := PageWidgets.Runtime.Query
	for root := range d.Queries {
		nodes := 0
		var visit func(string, int, map[string]bool) error
		visit = func(id string, depth int, path map[string]bool) error {
			nodes++
			q, ok := d.Queries[id]
			if !ok || path[id] || depth > c.Set.MaxDepth || nodes > c.Set.MaxNodes {
				return fmt.Errorf("collection sources are cyclic or exceed their budget")
			}
			path[id] = true
			defer delete(path, id)
			if q.Set != nil {
				for _, input := range q.Set.Inputs {
					if err := visit(input, depth+1, path); err != nil {
						return err
					}
				}
			}
			v := d.Variables[q.Input]
			if q.Input != "" && v.Mode != "input" {
				if !p.collectionBuilderInput(q.Input, q.Owner, q.Object) {
					return fmt.Errorf("collection input producer is unavailable")
				}
				for _, s := range sections {
					if s.ID == v.Source.Section {
						if err := visit(d.Variables[s.Ports["collectionVariable"]].Source.Query, depth+1, path); err != nil {
							return err
						}
					}
				}
			}
			return nil
		}
		if err := visit(root, 0, map[string]bool{}); err != nil {
			return err
		}
	}
	for _, s := range sections {
		v := d.Variables[s.Ports["collectionVariable"]]
		if v.Source != nil && v.Source.Kind == "query" {
			for _, producer := range sections {
				if producer.ID == v.Source.Section && producer.Ports["collectionOutputVariable"] != "" {
					return fmt.Errorf("builder output needs an independent consumer plan")
				}
			}
		}
	}
	return nil
}

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

func (p Page) ComputeResources() map[string]PageComputeResource {
	out := map[string]PageComputeResource{}
	for id, v := range p.InputSchema.Variables {
		if v.Source != nil && v.Source.Kind == "compute" && v.Source.Compute != nil {
			out[id] = *v.Source.Compute
		}
	}
	return out
}
