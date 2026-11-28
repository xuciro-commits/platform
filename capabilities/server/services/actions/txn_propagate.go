package actions

import (
	"encoding/json"
	"fmt"
	"math"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	ruleprotocol "platformserver/kernel/protocol"
	"platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/services/ontology"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
)

type propEdge struct {
	parent, child string
	ratio, loss   float64
}

type propDemand struct {
	due        string
	qty        float64
	origin     string
	sourceType string
	sourceID   string
}

type propRequirement struct {
	node       string
	qty        float64
	due        string
	start      string
	level      int
	origin     string
	trace      string
	sourceType string
	sourceID   string
}

func roundProp(v float64) float64 {
	return math.Round(v*10000) / 10000
}

func fmtProp(v float64) string {
	return strconv.FormatFloat(roundProp(v), 'f', -1, 64)
}

func propString(item platform.ExprItem, name string) string {
	if name == "" {
		return ""
	}
	if name == "id" {
		return item.ID
	}
	v, ok := item.Fields[name]
	if !ok || v.Status != "value" {
		return ""
	}
	var s string
	if json.Unmarshal(v.Data, &s) == nil {
		return s
	}
	return strings.Trim(string(v.Data), `"`)
}

func propFloat(item platform.ExprItem, name string) float64 {
	if name == "" {
		return 0
	}
	v, ok := item.Fields[name]
	if !ok || v.Status != "value" {
		return 0
	}
	var f float64
	if json.Unmarshal(v.Data, &f) == nil {
		return f
	}
	var s string
	if json.Unmarshal(v.Data, &s) == nil {
		f, _ = strconv.ParseFloat(s, 64)
		return f
	}
	return 0
}

func propAddDays(day string, delta int) string {
	t, err := time.Parse(time.DateOnly, day)
	if err != nil {
		return day
	}
	return t.AddDate(0, 0, delta).Format(time.DateOnly)
}

func (b *Runtime) txnPropagate(c platform.Caller, receipt *pb.ChangeRecord, p platform.TxnPropagate, scope platform.RuleReadSpec, subject map[string]any, facts map[string]any, stepKey string, iterations *int, now time.Time, proposal ruleprotocol.TransactionProposal, reserve func(string) *platform.Error) *platform.Error {
	var syntheticArgs []platform.Expr
	touch := func(setName string, fields ...string) {
		emitted := false
		for _, f := range fields {
			if f != "" && f != "id" {
				var agg platform.Expr
				_ = json.Unmarshal(platform.Raw(map[string]any{"kind": "integer", "op": "count", "agg": map[string]any{"source": setName, "field": []string{f}}}), &agg)
				syntheticArgs = append(syntheticArgs, agg)
				emitted = true
			}
		}
		if !emitted {
			var agg platform.Expr
			_ = json.Unmarshal(platform.Raw(map[string]any{"kind": "integer", "op": "count", "agg": map[string]any{"source": setName}}), &agg)
			syntheticArgs = append(syntheticArgs, agg)
		}
	}
	touch(p.Nodes, p.Offset)
	touch(p.Edges.Set, p.Edges.Parent, p.Edges.Child, p.Edges.Ratio, p.Edges.Loss)
	for _, d := range p.Demands {
		touch(d.Set, d.Node, d.Quantity, d.Due, d.SourceField)
	}
	for _, cr := range p.Credits {
		touch(cr.Set, cr.Node, cr.Quantity, cr.Due)
	}
	synthetic := platform.Expr{Op: "and", Kind: "boolean", Args: syntheticArgs}
	ctx, err := ruleprotocol.RuleContext(c, synthetic, scope, b.Lookup, subject, facts, now)
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "{why}", err.Error())
	}
	checkSets := []string{p.Nodes, p.Edges.Set}
	for _, d := range p.Demands {
		checkSets = append(checkSets, d.Set)
	}
	for _, cr := range p.Credits {
		checkSets = append(checkSets, cr.Set)
	}
	for _, setName := range checkSets {
		if ctx.Sets[setName].Status == "withheld" || ctx.Sets[setName].Withheld || !ctx.Sets[setName].Complete {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Propagation input collection is withheld or incomplete / 传递输入集合受权限限制或超出单次完整读取上限")
		}
	}

	offsets := map[string]int{}
	knownNodes := map[string]bool{}
	for _, item := range ctx.Sets[p.Nodes].Items {
		if item.ID != "" {
			knownNodes[item.ID] = true
			if p.Offset != "" {
				offsets[item.ID] = int(math.Round(propFloat(item, p.Offset)))
			}
		}
	}

	adj := map[string][]propEdge{}
	for _, item := range ctx.Sets[p.Edges.Set].Items {
		parent := propString(item, p.Edges.Parent)
		child := propString(item, p.Edges.Child)
		ratio := propFloat(item, p.Edges.Ratio)
		loss := propFloat(item, p.Edges.Loss)
		if parent == "" || child == "" || ratio <= 0 {
			continue
		}
		knownNodes[parent] = true
		knownNodes[child] = true
		adj[parent] = append(adj[parent], propEdge{parent: parent, child: child, ratio: ratio, loss: max(0, loss)})
	}
	for k := range adj {
		slices.SortFunc(adj[k], func(a, b propEdge) int { return strings.Compare(a.child, b.child) })
	}

	// Detect cycles deterministically.
	allNodes := make([]string, 0, len(knownNodes))
	for n := range knownNodes {
		allNodes = append(allNodes, n)
	}
	slices.Sort(allNodes)
	state := map[string]uint8{}
	var hasCycle func(string) bool
	hasCycle = func(u string) bool {
		state[u] = 1
		for _, e := range adj[u] {
			if state[e.child] == 1 || (state[e.child] == 0 && hasCycle(e.child)) {
				return true
			}
		}
		state[u] = 2
		return false
	}
	for _, n := range allNodes {
		if state[n] == 0 && hasCycle(n) {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Propagation graph contains a cycle / 传递关系存在环路")
		}
	}

	// Low-level codes over DAG.
	level := map[string]int{}
	maxLevel := 0
	for range len(allNodes) + 1 {
		changed := false
		for _, u := range allNodes {
			for _, e := range adj[u] {
				if level[e.child] < level[u]+1 {
					level[e.child] = level[u] + 1
					if level[e.child] > maxLevel {
						maxLevel = level[e.child]
					}
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}

	today := c.Today(now)
	demands := map[string][]propDemand{}
	for _, d := range p.Demands {
		label := d.Label
		if label == "" {
			label = d.Set
		}
		for _, item := range ctx.Sets[d.Set].Items {
			node := propString(item, d.Node)
			qty := propFloat(item, d.Quantity)
			if node == "" || qty <= 0 {
				continue
			}
			knownNodes[node] = true
			if _, ok := level[node]; !ok {
				allNodes = append(allNodes, node)
			}
			due := propString(item, d.Due)
			if due == "" {
				due = today
			}
			srcID := propString(item, d.SourceField)
			origin := label
			if srcID != "" {
				origin = label + ":" + srcID
			}
			demands[node] = append(demands[node], propDemand{due: due, qty: qty, origin: origin, sourceType: d.SourceType, sourceID: srcID})
		}
	}
	slices.Sort(allNodes)
	allNodes = slices.Compact(allNodes)

	type propCredit struct {
		due string
		qty float64
	}
	creditsByNode := map[string][]propCredit{}
	creditTotal := map[string]float64{}
	creditParts := map[string]map[string]float64{}
	var creditLabels []string
	for _, cr := range p.Credits {
		label := cr.Label
		if label == "" {
			label = cr.Set
		}
		if !slices.Contains(creditLabels, label) {
			creditLabels = append(creditLabels, label)
		}
		for _, item := range ctx.Sets[cr.Set].Items {
			node := propString(item, cr.Node)
			qty := propFloat(item, cr.Quantity)
			if node == "" || qty <= 0 {
				continue
			}
			cdue := propString(item, cr.Due)
			creditsByNode[node] = append(creditsByNode[node], propCredit{due: cdue, qty: qty})
			creditTotal[node] = roundProp(creditTotal[node] + qty)
			if creditParts[node] == nil {
				creditParts[node] = map[string]float64{}
			}
			creditParts[node][label] = roundProp(creditParts[node][label] + qty)
		}
	}

	var branchReqs, leafReqs []propRequirement
	for lvl := 0; lvl <= maxLevel; lvl++ {
		for _, node := range allNodes {
			if level[node] != lvl {
				continue
			}
			rows := demands[node]
			if len(rows) == 0 {
				continue
			}
			byDue := map[string]*propDemand{}
			var dues []string
			var origins map[string][]string = map[string][]string{}
			for _, r := range rows {
				bkt := byDue[r.due]
				if bkt == nil {
					cp := r
					byDue[r.due] = &cp
					dues = append(dues, r.due)
				} else {
					bkt.qty = roundProp(bkt.qty + r.qty)
					if bkt.sourceID == "" && r.sourceID != "" {
						bkt.sourceType, bkt.sourceID = r.sourceType, r.sourceID
					}
				}
				if r.origin != "" && !slices.Contains(origins[r.due], r.origin) {
					origins[r.due] = append(origins[r.due], r.origin)
				}
			}
			slices.Sort(dues)
			nodeCredits := slices.Clone(creditsByNode[node])
			slices.SortFunc(nodeCredits, func(a, b propCredit) int { return strings.Compare(a.due, b.due) })
			creditNote := ""
			if creditTotal[node] > 0 && len(creditParts[node]) > 0 {
				var parts []string
				for _, lb := range creditLabels {
					if v := creditParts[node][lb]; v > 0 {
						parts = append(parts, lb+":"+fmtProp(v))
					}
				}
				if len(parts) > 0 {
					creditNote = " (" + strings.Join(parts, ", ") + ")"
				}
			}
			for _, due := range dues {
				bkt := byDue[due]
				need := bkt.qty
				deducted := 0.0
				for ci := range nodeCredits {
					if need <= 1e-9 {
						break
					}
					if nodeCredits[ci].qty <= 1e-9 || (nodeCredits[ci].due != "" && nodeCredits[ci].due > due) {
						continue
					}
					use := min(nodeCredits[ci].qty, need)
					nodeCredits[ci].qty = roundProp(nodeCredits[ci].qty - use)
					need = roundProp(need - use)
					deducted = roundProp(deducted + use)
				}
				net := roundProp(bkt.qty - deducted)
				if net <= 1e-9 {
					continue
				}
				start := propAddDays(due, -offsets[node])
				originStr := strings.Join(origins[due], ", ")
				traceStr := fmt.Sprintf("gross %s - supply %s%s = net %s", fmtProp(bkt.qty), fmtProp(deducted), creditNote, fmtProp(net))
				req := propRequirement{
					node: node, qty: net, due: due, start: start, level: lvl,
					origin: originStr, trace: traceStr, sourceType: bkt.sourceType, sourceID: bkt.sourceID,
				}
				if len(adj[node]) > 0 {
					branchReqs = append(branchReqs, req)
					for _, e := range adj[node] {
						childQty := roundProp(net * e.ratio * (1 + e.loss))
						if childQty <= 1e-9 {
							continue
						}
						childOrigin := originStr
						if childOrigin == "" {
							childOrigin = node
						} else {
							childOrigin = childOrigin + " -> " + node
						}
						demands[e.child] = append(demands[e.child], propDemand{
							due: start, qty: childQty, origin: childOrigin,
							sourceType: bkt.sourceType, sourceID: bkt.sourceID,
						})
					}
				} else {
					leafReqs = append(leafReqs, req)
				}
			}
		}
	}

	applyTarget := func(tgt *platform.TxnPropagateTarget, reqs []propRequirement, tag string) *platform.Error {
		if tgt == nil {
			return nil
		}
		entity, ok := b.Entities[tgt.Type]
		if !ok {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Propagation target object is unavailable")
		}
		info, _ := records.Describe(b.App, entity, func(reflect.Type) string { return "" })
		initial := ""
		if info.Lifecycle != nil && len(info.Lifecycle.States) > 0 {
			initial = info.Lifecycle.Initial
		}
		if info.Scope.LevelFor(c.RolesIn(info.Type), c.Member.AuthorizationEngine()) != platform.ScopeTenant || info.Scope.Through != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Propagation target requires tenant-wide read permission / 传递目标对象需要全租户读取权限")
		}
		existingRows, totalRows, findErr := c.FindOf(reflect.TypeOf(entity.Model), platform.Query{
			Domain: platform.Raw([]any{[]any{"state", "=", initial}}),
			Sort:   []string{"id"},
			Limit:  platform.TxnMaxIterations,
		})
		if findErr != nil {
			return findErr
		}
		if totalRows > len(existingRows) {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Transaction iteration exceeds its limit")
		}
		type draftEntry struct {
			id   string
			used bool
		}
		byKey := map[string]*draftEntry{}
		var drafts []*draftEntry
		for _, row := range existingRows {
			var m map[string]any
			if json.Unmarshal(platform.Raw(row), &m) != nil {
				continue
			}
			id, _ := m["id"].(string)
			nodeVal, _ := m[tgt.Node].(string)
			dueVal, _ := m[tgt.Due].(string)
			if id == "" {
				continue
			}
			de := &draftEntry{id: id}
			drafts = append(drafts, de)
			key := nodeVal + "|" + dueVal
			if byKey[key] == nil {
				byKey[key] = de
			}
		}

		extraPatch := map[string]any{}
		var extraSources []string
		for _, assignment := range tgt.Changes {
			field, _ := info.Field(assignment.Field)
			expr, aErr := ontology.AssignmentExpr(field.Type, assignment, scope)
			if aErr != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, aErr.Error())
			}
			res, evalErr := ruleprotocol.EvaluateRule(expr, scope, c, subject, facts, now, b.Lookup)
			if evalErr != nil {
				return evalErr
			}
			extraSources = append(extraSources, res.Sources...)
			if res.Value.Status == "value" {
				val, convErr := ruleprotocol.ExprGoValue(res.Value)
				if convErr != nil {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, convErr.Error())
				}
				extraPatch[assignment.Field] = val
			}
		}

		for idx, req := range reqs {
			*iterations++
			if *iterations > platform.TxnMaxIterations {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Transaction iteration exceeds its limit")
			}
			patch := map[string]any{
				tgt.Node:     req.node,
				tgt.Quantity: json.Number(fmtProp(req.qty)),
				tgt.Due:      req.due,
			}
			for k, v := range extraPatch {
				patch[k] = v
			}
			if tgt.Start != "" {
				patch[tgt.Start] = req.start
			}
			if tgt.Level != "" {
				patch[tgt.Level] = req.level
			}
			if tgt.Origin != "" {
				patch[tgt.Origin] = req.origin
			}
			if tgt.Trace != "" {
				patch[tgt.Trace] = req.trace
			}
			if tgt.SourceType != "" && req.sourceType != "" {
				patch[tgt.SourceType] = req.sourceType
			}
			if tgt.SourceID != "" && req.sourceID != "" {
				patch[tgt.SourceID] = req.sourceID
			}
			key := req.node + "|" + req.due
			verb, id := "create", ""
			if match := byKey[key]; match != nil && !match.used {
				match.used = true
				verb, id = "edit", match.id
			} else if entity.Naming != "" {
				var seqErr *platform.Error
				id, seqErr = c.Next(receipt, entity.Type, now)
				if seqErr != nil {
					return seqErr
				}
			} else {
				id = fmt.Sprintf("%s-%s-%s-%d", receipt.GetChangeId(), stepKey, tag, idx)
			}
			if err := reserve(entity.Type + "/" + id); err != nil {
				return err
			}
			curEntity, writable := proposal.Apply(entity, info, patch, extraSources)
			if err := b.Ledger.ApplyTxnRecord(c, receipt, curEntity, verb, id, platform.Raw(writable), now); err != nil {
				return err
			}
		}
		for _, de := range drafts {
			if de.used {
				continue
			}
			*iterations++
			if *iterations > platform.TxnMaxIterations {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Transaction iteration exceeds its limit")
			}
			if err := reserve(entity.Type + "/" + de.id); err != nil {
				return err
			}
			if err := b.Ledger.ApplyTxnRecord(c, receipt, entity, "archive", de.id, platform.Raw(map[string]any{}), now); err != nil {
				return err
			}
		}
		return nil
	}

	if err := applyTarget(p.Branch, branchReqs, "b"); err != nil {
		return err
	}
	return applyTarget(p.Leaf, leafReqs, "l")
}
