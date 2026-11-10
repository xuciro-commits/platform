package platform

import (
	"fmt"
	"slices"
)

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
				if scope.MaxIterations < 1 || scope.MaxIterations > pageWidgets.Runtime.Loop.MaxItems || factor > pageWidgets.Runtime.Query.MaxTotalLimit/scope.MaxIterations {
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

type PageCollectionBuilder struct {
	Fields []string `json:"fields"`
}

func (p Page) collectionBuilderInput(id, owner string, object AssetRef) bool {
	d := p.InputSchema
	sections := p.DataBindings
	if !PageUIProfileSupports(d.profileVersion(), "platform.page.v2.88") {
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
	c := pageWidgets.Runtime.Query
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

type PageCollectionInput struct {
	Kind      string             `json:"kind"`
	Object    AssetRef           `json:"object"`
	Predicate RecordSetPredicate `json:"predicate"`
	Sort      []string           `json:"sort"`

	SortLocked bool           `json:"sortLocked,omitempty"`
	Traversal  *LinkTraversal `json:"traversal,omitempty"`
	Bindings   []AssetBinding `json:"bindings,omitempty"`
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

type PageComputeResource struct {
	Operation      AssetBinding       `json:"operation"`
	RecordVariable string             `json:"recordVariable"`
	Inputs         map[string]Binding `json:"inputs"`
	OutputField    string             `json:"outputField,omitempty"`
}

func (d PageInputSchema) checkComputeResource(v PageVariable) error {
	c := v.Source.Compute
	if !PageUIProfileSupports(d.profileVersion(), pageWidgets.Runtime.ComputeResource.RequiredUIProfile) || !slices.Contains([]string{"number", "decimal"}, v.Type) || v.Mode != "resource" || v.Writable || !slices.Contains([]string{"page", "overlay"}, v.Scope) || c == nil || !pageNodeID.MatchString(c.RecordVariable) || c.Operation.Ref.Kind != AssetCompute || c.Operation.Ref.Check() != nil || c.Operation.SourceVersion == "" || len(c.Inputs) == 0 || len(c.Inputs) > 64 || c.OutputField != "" && !pageNodeID.MatchString(c.OutputField) {
		return fmt.Errorf("compute resource needs its finite typed operation and original record")
	}
	parent := d.Variables[c.RecordVariable]
	if parent.Type != "record" || !(d.sharedContextRecord(c.RecordVariable) || parent.Mode == "resource" && parent.Source != nil && parent.Source.Kind == "record" && parent.Scope == v.Scope && parent.Owner == v.Owner) {
		return fmt.Errorf("compute input needs its confirmed record in the original owner")
	}
	subject := false
	for name, b := range c.Inputs {
		if !pageNodeID.MatchString(name) || b.Check() != nil || b.Source != "literal" && b.Source != "subject" || b.Source == "subject" && (len(b.Path) < 1 || len(b.Path) > 16 || slices.ContainsFunc(b.Path, func(part string) bool {
			return !pageNodeID.MatchString(part)
		})) {
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
	for id, v := range p.InputSchema.Variables {
		if v.Source != nil && v.Source.Kind == "compute" && v.Source.Compute != nil {
			out[id] = *v.Source.Compute
		}
	}
	return out
}

func (p Page) CheckComputeResource(c PageComputeResource, object EntityInfo, op Operation, valueType string, lookups ...func(string) (EntityInfo, bool)) error {
	lookup := func(typ string) (EntityInfo, bool) {
		return object, typ == object.Type
	}
	if len(lookups) > 0 {
		lookup = lookups[0]
	}
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
		if b.Source != "subject" || len(b.Path) < 1 || len(b.Path) > 16 {
			return fmt.Errorf("compute source binding differs")
		}
		f, err := RecordPathField(object.Type, b.Path, lookup)
		if err != nil {
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

type PageLoop struct {
	Collection   string `json:"collection"`
	ItemVariable string `json:"itemVariable"`
	Limit        int    `json:"limit"`
}
