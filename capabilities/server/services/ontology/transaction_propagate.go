package ontology

import (
	"fmt"
	ruleprotocol "platformserver/kernel/protocol"
	"platformserver/platform"
	"slices"
)

func collectionInfo(name string, scope platform.RuleReadSpec, lookup func(string) (platform.RecordTypeInfo, bool)) (platform.RecordTypeInfo, error) {
	col, ok := scope.Collections[name]
	if !ok {
		return platform.RecordTypeInfo{}, fmt.Errorf("Propagate step collection %q is undeclared", name)
	}
	if col.Protocol != "" {
		return platform.RecordTypeInfo{Type: col.Protocol, Fields: col.Fields}, nil
	}
	info, ok := lookup(col.Object)
	if !ok {
		return platform.RecordTypeInfo{}, fmt.Errorf("Propagate step collection %q object is unavailable", name)
	}
	return info, nil
}

func checkTxnPropagate(ctx TransactionContext, step platform.TxnStep, scope platform.RuleReadSpec, lookup func(string) (platform.RecordTypeInfo, bool), engines ...platform.RuleEngine) error {
	p := step.Propagate
	if p == nil || step.Target != (platform.TxnRef{}) || p.Nodes == "" || p.Edges.Set == "" || len(p.Demands) == 0 || (p.Branch == nil && p.Leaf == nil) {
		return fmt.Errorf("Propagate step needs nodes, edges, demands and at least one output target")
	}
	if p.Branch != nil && p.Leaf != nil && p.Branch.Type == p.Leaf.Type {
		return fmt.Errorf("Propagate branch and leaf targets must be distinct objects")
	}
	nodeInfo, err := collectionInfo(p.Nodes, scope, lookup)
	if err != nil {
		return err
	}
	if p.Offset != "" {
		if f, ok := nodeInfo.Field(p.Offset); !ok || !slices.Contains([]string{"integer", "decimal"}, f.Type) {
			return fmt.Errorf("Propagate offset field %q must be numeric", p.Offset)
		}
	}
	edgeInfo, err := collectionInfo(p.Edges.Set, scope, lookup)
	if err != nil {
		return err
	}
	for _, req := range []struct{ name, kind string }{{p.Edges.Parent, "text"}, {p.Edges.Child, "text"}, {p.Edges.Ratio, "num"}} {
		f, ok := edgeInfo.Field(req.name)
		if req.name == "" || !ok || (req.kind == "num" && !slices.Contains([]string{"integer", "decimal"}, f.Type)) {
			return fmt.Errorf("Propagate edge field %q is invalid", req.name)
		}
	}
	if p.Edges.Loss != "" {
		if f, ok := edgeInfo.Field(p.Edges.Loss); !ok || !slices.Contains([]string{"integer", "decimal"}, f.Type) {
			return fmt.Errorf("Propagate edge loss field %q must be numeric", p.Edges.Loss)
		}
	}
	for _, list := range [][]platform.TxnPropagateInput{p.Demands, p.Credits} {
		for _, in := range list {
			inInfo, err := collectionInfo(in.Set, scope, lookup)
			if err != nil {
				return err
			}
			nf, nok := inInfo.Field(in.Node)
			qf, qok := inInfo.Field(in.Quantity)
			if in.Node == "" || in.Quantity == "" || !nok || !qok || nf.Name == "" || !slices.Contains([]string{"integer", "decimal"}, qf.Type) {
				return fmt.Errorf("Propagate input %q needs node and numeric quantity fields", in.Set)
			}
			if in.Due != "" {
				if df, ok := inInfo.Field(in.Due); !ok || df.Type != "date" {
					return fmt.Errorf("Propagate input %q due field %q must be a date", in.Set, in.Due)
				}
			}
			if in.SourceField != "" && in.SourceField != "id" {
				if _, ok := inInfo.Field(in.SourceField); !ok {
					return fmt.Errorf("Propagate input %q source field %q is undeclared", in.Set, in.SourceField)
				}
			}
		}
	}
	for _, tgt := range []*platform.TxnPropagateTarget{p.Branch, p.Leaf} {
		if tgt == nil {
			continue
		}
		info, ok := lookup(tgt.Type)
		if !ok || info.App != ctx.App || platform.OwnerOfType(info.Type) != ctx.App || info.Lifecycle == nil || len(info.Lifecycle.States) == 0 {
			return fmt.Errorf("Propagate target %q must be an owned stateful object", tgt.Type)
		}
		if tgt.Type != scope.Object && !ctx.Saved(tgt.Type) {
			return fmt.Errorf("Propagate target %q needs a saved builder object", tgt.Type)
		}
		reqFields := []struct{ name, kind string }{
			{tgt.Node, "ref"}, {tgt.Quantity, "num"}, {tgt.Due, "date"},
		}
		for _, rf := range reqFields {
			f, ok := info.Field(rf.name)
			if rf.name == "" || !ok || f.ReadOnly || (rf.kind == "num" && !slices.Contains([]string{"integer", "decimal"}, f.Type)) || (rf.kind == "date" && f.Type != "date") {
				return fmt.Errorf("Propagate target %q field %q is invalid", tgt.Type, rf.name)
			}
		}
		for _, opt := range []struct{ name, kind string }{
			{tgt.Start, "date"}, {tgt.Level, "integer"}, {tgt.Origin, "text"}, {tgt.Trace, "text"}, {tgt.SourceType, "text"}, {tgt.SourceID, "text"},
		} {
			if opt.name == "" {
				continue
			}
			f, ok := info.Field(opt.name)
			if !ok || f.ReadOnly || (opt.kind == "date" && f.Type != "date") || (opt.kind == "integer" && !slices.Contains([]string{"integer", "decimal"}, f.Type)) || (opt.kind == "text" && !slices.Contains([]string{"text", "longtext", "reference"}, f.Type)) {
				return fmt.Errorf("Propagate target %q optional field %q is invalid", tgt.Type, opt.name)
			}
		}
		seen := map[string]bool{}
		for _, assignment := range tgt.Changes {
			field, ok := info.Field(assignment.Field)
			if !ok || field.ReadOnly || seen[field.Name] {
				return fmt.Errorf("Propagate target %q change field %q is invalid", tgt.Type, assignment.Field)
			}
			seen[field.Name] = true
			expr, err := AssignmentExpr(field.Type, assignment, scope)
			if err != nil {
				return err
			}
			if _, err := ruleprotocol.RuleSchema(expr, scope, lookup, engines...); err != nil {
				return err
			}
		}
	}
	return nil
}
