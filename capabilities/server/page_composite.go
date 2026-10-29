package platformserver

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type PageCompositeSource struct {
	Object  string           `json:"object"`
	Records []map[string]any `json:"records"`
}
type PageMatrixCell struct {
	Row    string `json:"row"`
	Column string `json:"column"`
	Value  any    `json:"value"`
	Kind   string `json:"kind"`
}
type PageCompositeResult struct {
	Sources map[string]PageCompositeSource `json:"sources"`
	Cells   []PageMatrixCell               `json:"cells,omitempty"`
}

// All sources and cells are read within the route's existing tenant lock. A
// client supplies typed identities only; cell inputs are confirmed here.
func (t *Tenant) readPageComposite(member platform.Member, page platform.Page, section platform.PageDataBinding, request PageModelRead, at time.Time) (PageModelResult, *kernel.Error) {
	result := PageModelResult{Object: section.PrimaryModel().Type, Records: []map[string]any{}, Fields: []platform.FieldInfo{}, Access: map[string]map[string]platform.AuthzDecision{}, Composite: &PageCompositeResult{Sources: map[string]PageCompositeSource{}}}
	c := section
	recordLimit, cellLimit, byteLimit := platform.CompositeLimits()
	keys := make([]string, 0, len(c.Models))
	for key := range c.Models {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		binding := c.Models[key]
		info, _ := t.entity(binding.Model.Type)
		model := binding.Model
		model.Fields = slices.Clone(model.Fields)
		roles := make([]string, 0, len(binding.Roles))
		for role := range binding.Roles {
			roles = append(roles, role)
		}
		slices.Sort(roles)
		for _, role := range roles {
			field := binding.Roles[role]
			model.Fields = append(model.Fields, field)
		}
		read := request
		read.Offset, read.Limit, read.Search, read.Sort, read.Query = 0, recordLimit, "", nil, nil
		// The active node selection is not a filter on all composite sources.
		read.Records = map[string]PageModelRecord{}
		for name, record := range request.Records {
			if name != "$record" && name != "$parent" {
				read.Records[name] = record
			}
		}
		value, err := t.readPageModelSource(member, page, platform.PageDataBinding{Model: &model, ID: section.ID, Kind: "records"}, read, at)
		if err != nil {
			return result, err
		}
		if value.Total > read.Limit {
			return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Composite source exceeds 200 records; bind a narrower declared collection")
		}
		result.Partial = result.Partial || value.Partial
		result.Hidden = result.Hidden || value.Hidden
		caller := platform.NewCaller(runtime{t}, member, info.App, false, false)
		rows := []map[string]any{}
		for _, row := range value.Records {
			id := fmt.Sprint(row["id"])
			// Evaluate against the original initialized typed value, after the
			// ordinary record owner has admitted it (including false and zero).
			ruleRecord := t.pageModelRuleRecord(model.Type, id, row)
			for _, role := range roles {
				field := binding.Roles[role]
				if v, ok := row[field.Key()]; ok {
					row["$view."+role] = v
				}
			}
			if value.LabelField != "" {
				if _, explicit := binding.Roles["label"]; !explicit {
					row["$view.label"] = row[value.LabelField]
				}
			}
			denied := false
			for role, expr := range binding.Expressions {
				answer, refusal := platform.EvaluateRule(expr, platform.RuleReadSpec{Object: model.Type}, caller, ruleRecord, nil, at, t.entity)
				if refusal != nil {
					if refusal.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
						return result, refusal
					}
					result.Partial, denied = true, true
					break
				}
				var decoded any
				if err := json.Unmarshal(answer.Value.Data, &decoded); err != nil {
					return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Composite expression returned an unreadable value")
				}
				row["$view."+role] = decoded
			}
			if denied {
				if model.Visibility != "mark" {
					result.Hidden = true
				}
				continue
			}
			rows = append(rows, row)
		}
		result.Composite.Sources[key] = PageCompositeSource{Object: model.Type, Records: rows}
	}
	if result.Hidden {
		result.Composite.Sources = map[string]PageCompositeSource{}
		return result, nil
	}
	if _, graph := section.Models["nodes"]; graph {
		if err := checkCompositeGraph(result.Composite, c.Acyclic, result.Partial); err != nil {
			return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
	}
	if section.Value != nil {
		rows, columns := result.Composite.Sources["rows"], result.Composite.Sources["columns"]
		if len(rows.Records)*len(columns.Records) > cellLimit {
			return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Matrix exceeds 1600 cells; bind narrower declared collections")
		}
		info, _ := t.entity(rows.Object)
		caller := platform.NewCaller(runtime{t}, member, info.App, false, false)
		for _, row := range rows.Records {
			for _, column := range columns.Records {
				answer, err := platform.EvaluateRule(*c.Value, platform.MatrixRuleReads(c), caller, nil, map[string]any{"row": row["id"], "column": column["id"]}, at, t.entity)
				if err != nil {
					if err.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
						return result, err
					}
					result.Partial = true
					if c.Models["rows"].Model.Visibility != "mark" || c.Models["columns"].Model.Visibility != "mark" {
						result.Hidden = true
						result.Composite.Sources = map[string]PageCompositeSource{}
						result.Composite.Cells = nil
						return result, nil
					}
					continue
				}
				var value any
				if err := json.Unmarshal(answer.Value.Data, &value); err != nil {
					return result, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Matrix expression returned an unreadable value")
				}
				result.Composite.Cells = append(result.Composite.Cells, PageMatrixCell{Row: fmt.Sprint(row["id"]), Column: fmt.Sprint(column["id"]), Value: value, Kind: c.Value.Kind})
			}
		}
	}
	if len(platform.Raw(result)) > byteLimit {
		return PageModelResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Composite result exceeds its byte budget")
	}
	return result, nil
}

func checkCompositeGraph(data *PageCompositeResult, acyclic, partial bool) error {
	nodes := map[string]bool{}
	layers := map[string]bool{}
	for _, row := range data.Sources["layers"].Records {
		layers[fmt.Sprint(row["id"])] = true
	}
	for _, row := range data.Sources["nodes"].Records {
		nodes[fmt.Sprint(row["id"])] = true
		if layer, ok := row["$view.layer"]; ok && layer != nil && layer != "" && !layers[fmt.Sprint(layer)] && !partial {
			return fmt.Errorf("Graph node names a layer outside its bound collection")
		}
	}
	adjacent := map[string][]string{}
	for _, edge := range data.Sources["edges"].Records {
		from, to := fmt.Sprint(edge["$view.from"]), fmt.Sprint(edge["$view.to"])
		if !nodes[from] || !nodes[to] {
			if !partial {
				return fmt.Errorf("Graph edge names a node outside its bound collection")
			}
			continue
		}
		adjacent[from] = append(adjacent[from], to)
	}
	for _, row := range data.Sources["overlay"].Records {
		if !nodes[fmt.Sprint(row["$view.node"])] && !partial {
			return fmt.Errorf("Graph overlay names a node outside its bound collection")
		}
	}
	if acyclic {
		state := map[string]int{}
		var visit func(string) bool
		visit = func(id string) bool {
			if state[id] == 1 {
				return false
			}
			if state[id] == 2 {
				return true
			}
			state[id] = 1
			for _, next := range adjacent[id] {
				if !visit(next) {
					return false
				}
			}
			state[id] = 2
			return true
		}
		for id := range nodes {
			if !visit(id) {
				return fmt.Errorf("The bound graph contains a cycle")
			}
		}
	}
	return nil
}

func (t *Tenant) pageModelRuleRecord(typ, id string, fallback any) any {
	t.records.Lock()
	defer t.records.Unlock()
	if entity := t.records.Type(typ); entity != nil {
		if original := entity.Rows().Get(id); original != nil {
			return original.Value.Interface()
		}
	}
	return fallback
}
