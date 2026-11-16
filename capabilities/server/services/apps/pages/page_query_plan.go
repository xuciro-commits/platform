package pages

import (
	"encoding/json"
	"fmt"
	"platformserver/platform"
	"slices"
	"strings"
)

func checkQueries(p platform.Page, inputScope string) error {
	d := p.InputSchema
	sections := p.DataBindings
	scopes := map[string]platform.PageScope{}
	for _, scope := range p.ScopeVisibility.Scopes {
		scopes[scope.ID] = scope
	}
	c := platform.PageWidgets.Runtime.Query
	activePlans := activeQueryPlans(p)
	declaredMax := c.MaxPlans
	if platform.PageUIProfileSupports(d.ProfileVersion(), c.InventoryUIProfile) && p.ScopeVisibility.Root != "" {
		declaredMax = c.MaxDeclaredPlans
	}
	if len(activePlans) > c.MaxPlans || len(d.Queries) > declaredMax || len(d.Queries) > 0 && !platform.PageUIProfileSupports(d.ProfileVersion(), "platform.page.v2.9") {
		return fmt.Errorf("page query plans need v2.9 and a bounded plan count")
	}
	var dependsOnPlan func(string, map[string]bool) bool
	dependsOnPlan = func(id string, seen map[string]bool) bool {
		if seen[id] {
			return false
		}
		seen[id] = true
		v := d.Variables[id]
		if v.Source != nil {
			if v.Mode == "property" {
				return dependsOnPlan(v.Source.Variable, seen)
			}
			if v.Source.Kind == "plan" || v.Mode == "aggregate" {
				return true
			}
			if v.Source.Section != "" {
				for _, s := range sections {
					if s.ID == v.Source.Section && s.Ports["collectionVariable"] != "" {
						return true
					}
				}
			}
		}
		if v.Rule != nil {
			var scan func(platform.Expr) bool
			scan = func(expr platform.Expr) bool {
				if expr.Agg != nil && dependsOnPlan(expr.Agg.Source, seen) {
					return true
				}
				if expr.Ref != "" && dependsOnPlan(expr.Ref, seen) {
					return true
				}
				for _, arg := range expr.Args {
					if scan(arg) {
						return true
					}
				}
				return expr.In != nil && scan(*expr.In)
			}
			if scan(*v.Rule) {
				return true
			}
		}
		return false
	}
	if err := checkCollectionBuilderGraph(p); err != nil {
		return err
	}
	if err := checkQuerySets(d); err != nil {
		return err
	}
	total, declaredTotal := 0, 0
	for id, q := range d.Queries {
		if q.Owner != "" {
			if inputScope == "application" {
				return fmt.Errorf("application query %s cannot belong to an overlay", id)
			}
			if _, ok := scopes[q.Owner]; !ok || !platform.PageUIProfileSupports(d.ProfileVersion(), "platform.page.v2.11") {
				return fmt.Errorf("page query %s needs a v2.11 overlay owner", id)
			}
		}
		validSource := q.Object.Check() == nil && q.Object.Kind == platform.AssetObject && q.Interface == ""
		if q.Interface != "" {
			validSource = platform.PageUIProfileSupports(d.ProfileVersion(), c.InterfaceUIProfile) && platform.PageNodeID.MatchString(q.Interface) && q.Object == (platform.AssetRef{}) && q.Query != nil && q.Query.Ref.Kind == platform.AssetQuery && q.Input == "" && q.For == nil && q.Set == nil && q.Direction == "" && q.ItemOwner == "" && q.Filter == nil && inputScope != "application"
		}
		if !platform.PageNodeID.MatchString(id) || len(q.Title) > 1024 || !validSource || q.Limit < 1 || q.Limit > c.MaxLimit || q.Offset < 0 || q.Offset > c.MaxOffset || len(q.Sort) > c.MaxSort {
			return fmt.Errorf("page query %s has an invalid identity, object or budget", id)
		}
		if q.Filter != nil && q.Filter.Kind != "boolean" {
			return fmt.Errorf("page query %s needs one boolean Expr filter", id)
		}
		if q.Input != "" {
			v, ok := d.Variables[q.Input]
			var object platform.AssetRef
			if d.Interface != nil {
				for _, port := range d.Interface.Inputs {
					if port.Variable == q.Input && port.Object != nil {
						object = *port.Object
					}
				}
			}
			if !platform.PageUIProfileSupports(d.ProfileVersion(), "platform.page.v2.85") || inputScope == "application" || !ok || (v.Mode != "input" || object != q.Object) && !collectionBuilderInput(p, q.Input, q.Owner, q.Object) || v.Type != "object-set" || q.Query != nil || q.For != nil || q.Direction != "" || q.ItemOwner != "" || q.Set != nil {
				return fmt.Errorf("query %s needs its exact original collection input", id)
			}
		}
		factor := 1
		if q.ItemOwner != "" {
			if inputScope == "application" || !platform.PageUIProfileSupports(d.ProfileVersion(), "platform.page.v2.20") || scopes[q.ItemOwner].Kind != "loop" || p.ScopeOwner(q.ItemOwner, "overlay") != q.Owner {
				return fmt.Errorf("item query %s needs a matching parent loop scope", id)
			}
			var err error
			factor, err = loopFactor(p, q.ItemOwner)
			if err != nil {
				return err
			}
			if q.Set == nil && (q.Query == nil || q.For == nil || q.For.Variable != scopes[q.ItemOwner].Bindings["itemVariable"]) {
				return fmt.Errorf("item query %s needs its parent record", id)
			}
		}
		declaredTotal += q.Limit * factor
		if activePlans[id] {
			total += q.Limit * factor
		}
		if q.Query != nil && (q.Query.Ref.Check() != nil || (q.Query.Ref.Kind != platform.AssetQuery && q.Query.Ref.Kind != platform.AssetLinkType) || q.Query.SourceVersion == "") {
			return fmt.Errorf("page query %s needs an exact named query binding", id)
		}
		if q.Direction != "" && (q.Query == nil || q.Query.Ref.Kind != platform.AssetLinkType) {
			return fmt.Errorf("direction requires a link binding")
		}
		if q.Query != nil && q.Query.Ref.Kind == platform.AssetLinkType {
			if !platform.PageUIProfileSupports(d.ProfileVersion(), "platform.page.v2.22") || q.For == nil || q.For.Variable == "" || d.Variables[q.For.Variable].Type != "record" || (q.Direction != "forward" && q.Direction != "reverse") || q.Set != nil {
				return fmt.Errorf("link plan needs v2.22, a direction and typed start record")
			}
		}
		if q.For != nil && q.Query == nil {
			return fmt.Errorf("page query %s parent input requires a named query", id)
		}
		for _, field := range q.Sort {
			if !platform.PageNodeID.MatchString(strings.TrimPrefix(field, "-")) {
				return fmt.Errorf("page query %s has an invalid sort", id)
			}
		}
		for _, value := range queryValues(q) {
			if (value.Variable != "") == (len(value.Literal) > 0) {
				return fmt.Errorf("page query %s value needs one variable or literal", id)
			}
			if value.Variable != "" {
				v, ok := d.Variables[value.Variable]
				plannedInput := dependsOnPlan(value.Variable, map[string]bool{})
				if plannedInput && slices.Contains([]string{"page", "overlay"}, inputScope) && recordContextQueryInput(p, id, value.Variable) && !variableDependsOnQuery(p, value.Variable, id) {
					plannedInput = false
				}
				if !ok || inputScope == "application" && (v.Scope != "application" || v.Type == "record" && (q.Query == nil || q.Query.Ref.Kind != platform.AssetLinkType)) || (v.Scope != "page" && v.Scope != "application" && !(v.Scope == "overlay" && v.Owner == q.Owner && q.Owner != "") && !(v.Scope == "loop-item" && v.Owner == q.ItemOwner && q.ItemOwner != "")) || !slices.Contains([]string{"string", "boolean", "record", "decimal", "string-set"}, v.Type) || plannedInput {
					return fmt.Errorf("page query %s parameter escapes its input scope", id)
				}
			} else {
				var literal any
				if _, exact := platform.DecimalLiteral(value.Literal); exact {
					if !platform.PageUIProfileSupports(d.ProfileVersion(), "platform.page.v2.16") {
						return fmt.Errorf("decimal query literal requires v2.16")
					}
					continue
				}
				if json.Unmarshal(value.Literal, &literal) != nil {
					return fmt.Errorf("page query %s has an invalid literal", id)
				}
				switch literal.(type) {
				case string, bool:
					if literalType(value.Literal) == "" {
						return fmt.Errorf("page query %s literal exceeds its limit", id)
					}
				case float64:
				default:
					return fmt.Errorf("page query %s needs a scalar literal", id)
				}
			}
		}
		if q.Filter != nil {
			for _, ref := range platform.ConditionRefs(*q.Filter) {
				name, ok := strings.CutPrefix(ref, "root.")
				if !ok {
					continue
				}
				name, _, _ = strings.Cut(name, "#")
				variable, exists := d.Variables[name]
				plannedInput := dependsOnPlan(name, map[string]bool{})
				if !exists || !platform.PageNodeID.MatchString(name) || inputScope == "application" && variable.Scope != "application" || variable.Scope != "page" && variable.Scope != "application" && !(variable.Scope == "overlay" && variable.Owner == q.Owner && q.Owner != "") && !(variable.Scope == "loop-item" && variable.Owner == q.ItemOwner && q.ItemOwner != "") || !slices.Contains([]string{"string", "boolean", "record", "decimal", "number", "string-set"}, variable.Type) || plannedInput {
					return fmt.Errorf("page query %s filter parameter escapes its input scope", id)
				}
			}
		}
	}
	if platform.PageUIProfileSupports(d.ProfileVersion(), c.InventoryUIProfile) && p.ScopeVisibility.Root != "" && declaredTotal > c.MaxDeclaredTotalLimit {
		return fmt.Errorf("declared inventory query budget exceeded")
	}
	if total > c.MaxTotalLimit {
		return fmt.Errorf("page query plan window budget exceeded")
	}
	aggregates, expanded := 0, 0
	seenAggregates := map[string]bool{}
	for id, v := range d.Variables {
		if v.Mode == "aggregate" {
			if !platform.PageUIProfileSupports(d.ProfileVersion(), "platform.page.v2.21") {
				return fmt.Errorf("count variable requires v2.21")
			}
			aggregates++
			if v.Source != nil && !seenAggregates[v.Source.Query+"/"+v.Source.Kind+"/"+v.Source.Measure] {
				seenAggregates[v.Source.Query+"/"+v.Source.Kind+"/"+v.Source.Measure] = true
				factor, err := loopFactor(p, d.Queries[v.Source.Query].ItemOwner)
				if err != nil {
					return err
				}
				expanded += factor
			}
		}
		if v.Source != nil && (v.Source.Kind == "plan" || v.Mode == "aggregate") {
			q, ok := d.Queries[v.Source.Query]
			if !ok || !platform.PageUIProfileSupports(d.ProfileVersion(), "platform.page.v2.9") || !(q.ItemOwner != "" && v.Scope == "loop-item" && v.Owner == q.ItemOwner || q.ItemOwner == "" && (q.Owner == "" && v.Scope == inputScope || q.Owner != "" && v.Scope == "overlay" && v.Owner == q.Owner)) {
				return fmt.Errorf("page variable %s needs an existing query plan", id)
			}
		}
	}
	if aggregates > platform.PageWidgets.Runtime.Aggregate.MaxVariables || expanded > platform.PageWidgets.Runtime.Aggregate.MaxExpandedReads {
		return fmt.Errorf("count variable read budget exceeded")
	}
	return nil
}

func checkQuerySets(d platform.PageInputSchema) error {
	for root, plan := range d.Queries {
		if plan.Set == nil {
			continue
		}
		if !platform.PageUIProfileSupports(d.ProfileVersion(), "platform.page.v2.18") {
			return fmt.Errorf("query set %s requires v2.18", root)
		}
		nodes := 0
		var visit func(string, int, map[string]bool) error
		visit = func(id string, depth int, path map[string]bool) error {
			nodes++
			q, ok := d.Queries[id]
			if q.Query != nil && q.Query.Ref.Kind == platform.AssetLinkType {
				return fmt.Errorf("link traversal is not a set predicate source")
			}
			if !ok || !platform.PageNodeID.MatchString(id) || path[id] || depth > platform.QuerySetMaxDepth || nodes > platform.QuerySetMaxNodes || q.Object != plan.Object || q.Owner != plan.Owner || q.ItemOwner != plan.ItemOwner {
				return fmt.Errorf("query set %s has a missing, cyclic, incompatible or oversized source", root)
			}
			if q.Set == nil {
				return nil
			}
			if !slices.Contains(platform.PageWidgets.Runtime.Query.Set.Operations, q.Set.Op) || len(q.Set.Inputs) != 2 {
				return fmt.Errorf("query set %s needs a binary supported operation", id)
			}
			path[id] = true
			defer delete(path, id)
			for _, input := range q.Set.Inputs {
				if err := visit(input, depth+1, path); err != nil {
					return err
				}
			}
			return nil
		}
		if err := visit(root, 0, map[string]bool{}); err != nil {
			return err
		}
	}
	return nil
}

func loopFactor(p platform.Page, owner string) (int, error) {
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
				if scope.MaxIterations < 1 || scope.MaxIterations > platform.PageWidgets.Runtime.Loop.MaxItems || factor > platform.PageWidgets.Runtime.Query.MaxTotalLimit/scope.MaxIterations {
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

func collectionBuilderInput(p platform.Page, id, owner string, object platform.AssetRef) bool {
	d := p.InputSchema
	sections := p.DataBindings
	if !platform.PageUIProfileSupports(d.ProfileVersion(), "platform.page.v2.88") {
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

func checkCollectionBuilderGraph(p platform.Page) error {
	d := p.InputSchema
	sections := p.DataBindings
	c := platform.PageWidgets.Runtime.Query
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
				if !collectionBuilderInput(p, q.Input, q.Owner, q.Object) {
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

func recordContextQueryInput(p platform.Page, query, variable string) bool {
	d := p.InputSchema
	sections := p.DataBindings
	q := d.Queries[query]
	v := d.Variables[variable]
	if q.For == nil || q.For.Variable != variable || v.Type != "record" || v.Mode != "resource" || v.Source == nil || v.Source.Kind != "record" || !(v.Scope == "page" || v.Scope == "overlay" && v.Owner == q.Owner && q.Owner != "") {
		return false
	}
	consumer := false
	for _, section := range sections {
		for _, id := range section.Ports {
			window := d.Variables[id]
			if window.Source != nil && window.Source.Kind == "plan" && window.Source.Query == query {
				for _, input := range section.Ports {
					if input == variable {
						consumer = true
					}
				}
			}
		}
	}

	if !consumer {
		return false
	}
	for _, s := range sections {
		if s.ID != v.Source.Section {
			continue
		}
		if v.Source.Port != "" && !s.HasRecordOutput(v.Source.Port, variable) {
			return false
		}
		return s.Ports["selectionVariable"] == variable || s.Ports["recordVariable"] == variable || v.Source.Port != "" || s.Object.Name != "" || s.PrimaryModel() != nil

	}
	return false
}
