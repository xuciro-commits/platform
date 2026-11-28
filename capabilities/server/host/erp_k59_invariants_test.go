package host

import (
	"context"
	"encoding/json"
	"math"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/devops/packages"
	"platformserver/services/ontology"
	build "platformserver/services/resources/authoring"
	controlowner "platformserver/services/security/control"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestInvariantK59ContractReadsInFlightAndGraphPropagation(t *testing.T) {
	source := newInvariantTenant(t, "k58-source")
	lit := func(v any, kind string) platform.Expr {
		return platform.Expr{Kind: kind, Lit: &platform.ExprLiteral{Value: platform.Raw(v)}}
	}
	exprPtr := func(e platform.Expr) *platform.Expr { return &e }
	aggOp := func(op, kind, src string, inner *platform.Expr) platform.Expr {
		m := map[string]any{"kind": kind, "op": op, "agg": map[string]any{"source": src}}
		if inner != nil {
			m["in"] = inner
		}
		var out platform.Expr
		_ = json.Unmarshal(platform.Raw(m), &out)
		return out
	}

	const (
		metricContract = "sample.metric/1"
		ingestContract = "sample.ingest/1"
		unitType       = "invariantproject.unitnode"
		factType       = "invariantproject.factline"
		voucherType    = "invariantproject.voucherdoc"
		hubType        = "invariantproject.reconcilehub"
		receiverType   = "invariantproject.ingestdoc"
		edgeType       = "invariantproject.graphlink"
		demandType     = "invariantproject.demandrow"
		creditType     = "invariantproject.creditrow"
		branchPropType = "invariantproject.branchplan"
		leafPropType   = "invariantproject.leafplan"
		runType        = "invariantproject.planrun"
	)

	// 1. Declare contracts with ReadSchemas and polymorphic RefType payload fields.
	source.accept(t, "build.contract.create", build.ContractType, "C_METRIC", map[string]any{
		"name":  "sample.metric",
		"major": 1,
		"title": "Sample metric",
		"reads": []string{"totals"},
		"readSchemas": map[string]any{
			"totals": []platform.Field{
				{Name: "amount", Type: "number", Description: "Total amount"},
			},
		},
	})
	source.accept(t, "build.contract.publish", build.ContractType, "C_METRIC", nil)

	source.accept(t, "build.contract.create", build.ContractType, "C_INGEST", map[string]any{
		"name":  "sample.ingest",
		"major": 1,
		"title": "Sample ingest",
		"actions": []protocol.ContractAction{{
			Name:  "send",
			Title: "Send",
			Payload: []platform.Field{
				{Name: "scope", Type: "string", Required: true, Description: "Scope"},
				{Name: "amount", Type: "number", Required: true, Description: "Amount"},
				{Name: "origintype", Type: "string", Description: "Origin type"},
				{Name: "originid", Type: "string", RefType: "origintype", Description: "Origin"},
			},
		}},
	})
	source.accept(t, "build.contract.publish", build.ContractType, "C_INGEST", nil)

	// 2. Provider side: unitnode + factline ledger whose balance implements sample.metric/1#totals,
	// plus voucherdoc testing transactional reverse step & via-target update.
	source.accept(t, "build.object.create", build.ObjectType, "UNIT", map[string]any{
		"name": "unitnode", "title": "Unit node", "plural": "Unit nodes",
		"fields": []map[string]any{
			{"name": "title", "title": "Title", "type": "text", "required": true, "search": true},
			{"name": "offsetdays", "title": "Offset days", "type": "integer", "default": "0"},
			{"name": "statusnote", "title": "Status note", "type": "text"},
		},
	})
	source.accept(t, "build.object.publish", build.ObjectType, "UNIT", nil)

	source.accept(t, "build.object.create", build.ObjectType, "FACT", map[string]any{
		"name": "factline", "title": "Fact line", "plural": "Fact lines", "ledger": true,
		"fields": []map[string]any{
			{"name": "unit", "title": "Unit", "type": "reference", "ref": unitType, "required": true},
			{"name": "docref", "title": "Doc ref", "type": "text"},
			{"name": "amount", "title": "Amount", "type": "decimal", "required": true},
		},
		"balances": []map[string]any{{
			"name": "totals", "title": "Totals", "keys": []string{"unit"}, "measures": []string{"amount"},
			"protocols": []protocol.ProtocolBinding{{Name: "sample.metric", Version: 1, Reads: []string{"totals"}}},
		}},
	})
	source.accept(t, "build.object.publish", build.ObjectType, "FACT", nil)

	source.accept(t, "build.object.create", build.ObjectType, "VOUCHER", map[string]any{
		"name": "voucherdoc", "title": "Voucher", "plural": "Vouchers",
		"states": []ontology.State{{Name: "draft", Title: "Draft"}, {Name: "posted", Title: "Posted"}, {Name: "voided", Title: "Voided"}},
		"fields": []map[string]any{
			{"name": "title", "title": "Title", "type": "text", "required": true, "search": true},
			{"name": "unit", "title": "Unit", "type": "reference", "ref": unitType, "required": true},
			{"name": "amount", "title": "Amount", "type": "decimal", "required": true},
			{"name": "fact", "title": "Fact", "type": "reference", "ref": factType, "readOnly": true},
		},
	})
	source.accept(t, "build.object.publish", build.ObjectType, "VOUCHER", nil)

	postVoucherAct := ontology.Action{
		Name: "postdoc", Title: "Post", From: []string{"draft"}, Roles: []string{ontology.BuilderRole},
		Uses: []string{ingestContract + "#send"},
		Transaction: &platform.BusinessTxn{Subject: voucherType, Steps: []platform.TxnStep{
			{Name: "fact", Kind: "create", Target: platform.TxnRef{Type: factType}, Changes: []platform.TxnSet{
				{Field: "unit", Expr: exprPtr(platform.ExprReference("reference", "unit"))},
				{Field: "docref", Expr: exprPtr(platform.ExprReference("text", "id"))},
				{Field: "amount", Expr: exprPtr(platform.ExprReference("decimal", "amount"))},
				{Field: "on", Expr: exprPtr(lit("2026-04-10", "date"))},
			}},
			{Name: "mark", Kind: "update", Target: platform.TxnRef{Type: voucherType, ID: "$subject"}, Changes: []platform.TxnSet{
				{Field: "state", Expr: exprPtr(lit("posted", "choice"))},
				{Field: "fact", Expr: exprPtr(platform.ExprReference("reference", "step.fact"))},
			}},
			{Name: "note", Kind: "update", Target: platform.TxnRef{Type: unitType, Via: "unit"}, Changes: []platform.TxnSet{
				{Field: "statusnote", Expr: exprPtr(lit("posted", "text"))},
			}},
			{Name: "send", Kind: "request", Request: &platform.TxnRequest{
				Deferred: true, Optional: true, Use: ingestContract + "#send",
				Key: exprPtr(platform.Expr{Op: "concat", Kind: "text", Args: []platform.Expr{lit("ING-", "text"), platform.ExprReference("text", "id")}}),
				Payload: []platform.TxnSet{
					{Field: "scope", Expr: exprPtr(lit("DEFAULT", "text"))},
					{Field: "amount", Expr: exprPtr(platform.ExprReference("decimal", "amount"))},
					{Field: "origintype", Expr: exprPtr(lit("provider.voucherdoc", "text"))},
					{Field: "originid", Expr: exprPtr(platform.ExprReference("text", "id"))},
				},
			}},
		}},
	}
	voidVoucherAct := ontology.Action{
		Name: "voiddoc", Title: "Void", From: []string{"posted"}, Roles: []string{ontology.BuilderRole},
		Inputs: []ontology.Input{{Name: "on", Title: "Reversal date", Type: "date", Required: true}},
		Transaction: &platform.BusinessTxn{Subject: voucherType, Steps: []platform.TxnStep{
			{Name: "rev", Kind: "reverse", Target: platform.TxnRef{Type: factType, Via: "fact"}, Changes: []platform.TxnSet{
				{Field: "on", Expr: exprPtr(platform.ExprReference("date", "input.on"))},
			}},
			{Name: "mark", Kind: "update", Target: platform.TxnRef{Type: voucherType, ID: "$subject"}, Changes: []platform.TxnSet{
				{Field: "state", Expr: exprPtr(lit("voided", "choice"))},
			}},
			{Name: "note", Kind: "update", Target: platform.TxnRef{Type: unitType, Via: "unit"}, Changes: []platform.TxnSet{
				{Field: "statusnote", Expr: exprPtr(lit("voided", "text"))},
			}},
		}},
	}
	for _, item := range []struct {
		id  string
		def ontology.Action
	}{{"V_POST", postVoucherAct}, {"V_VOID", voidVoucherAct}} {
		source.accept(t, "build.actiontype.create", build.ActionTypeType, item.id, map[string]any{"name": voucherType + "." + item.def.Name, "object": voucherType, "definition": item.def})
		source.accept(t, "build.actiontype.publish", build.ActionTypeType, item.id, nil)
	}
	source.accept(t, "build.object.publish", build.ObjectType, "VOUCHER", nil)

	// 3. Consumer/Reconcile side: reconcilehub + ingestdoc with InFlight projection & protocol-backed RuleCollection.
	source.accept(t, "build.object.create", build.ObjectType, "HUB", map[string]any{
		"name": "reconcilehub", "title": "Reconcile hub", "plural": "Reconcile hubs",
		"fields": []map[string]any{
			{"name": "title", "title": "Title", "type": "text", "required": true, "search": true},
		},
		"records": []map[string]any{{"id": "DEFAULT", "title": "Main Hub"}},
	})
	source.accept(t, "build.object.publish", build.ObjectType, "HUB", nil)

	source.accept(t, "build.object.create", build.ObjectType, "RECEIVER", map[string]any{
		"name": "ingestdoc", "title": "Ingest doc", "plural": "Ingest docs",
		"fields": []map[string]any{
			{"name": "scope", "title": "Scope", "type": "reference", "ref": hubType, "inverse": "received", "required": true},
			{"name": "amount", "title": "Amount", "type": "decimal", "required": true},
			{"name": "origintype", "title": "Origin type", "type": "text"},
			{"name": "originid", "title": "Origin", "type": "text", "refType": "origintype"},
		},
		"protocols": []protocol.ProtocolBinding{{
			Name:     "sample.ingest",
			Version:  1,
			InFlight: &protocol.InFlightBinding{By: "scope", Ref: hubType, Inverse: "inflight"},
			Actions:  map[string]string{"send": "create"},
		}},
	})
	source.accept(t, "build.object.publish", build.ObjectType, "RECEIVER", nil)

	hubSets := []platform.RecordSet{
		{Name: "received", Object: receiverType, Direction: "inverse", From: "received", Limit: 200, Order: []platform.RecordSetSort{{Expr: platform.ExprReference("text", "id")}}},
		{Name: "inflight", Object: receiverType + ".inflight", Direction: "inverse", From: "inflight", Limit: 200, Order: []platform.RecordSetSort{{Expr: platform.ExprReference("text", "id")}}},
	}
	hubReads := &platform.RuleReadSpec{Collections: map[string]platform.RuleCollection{
		"ext":      {App: build.ID, Protocol: metricContract + "#totals", Policy: "complete"},
		"received": {App: build.ID, Object: receiverType, Set: "received", Policy: "complete"},
		"inflight": {App: build.ID, Object: receiverType + ".inflight", Set: "inflight", Policy: "complete"},
	}}
	sumExt := aggOp("sum", "decimal", "ext", exprPtr(platform.Expr{Kind: "decimal", Op: "coalesce", Args: []platform.Expr{platform.ExprReference("decimal", "item.amount"), lit(0, "decimal")}}))
	sumRec := aggOp("sum", "decimal", "received", exprPtr(platform.Expr{Kind: "decimal", Op: "coalesce", Args: []platform.Expr{platform.ExprReference("decimal", "item.amount"), lit(0, "decimal")}}))
	sumInf := aggOp("sum", "decimal", "inflight", exprPtr(platform.Expr{Kind: "decimal", Op: "coalesce", Args: []platform.Expr{platform.ExprReference("decimal", "item.amount"), lit(0, "decimal")}}))
	cntInf := aggOp("count", "integer", "inflight", nil)
	diffExpr := platform.Expr{Kind: "decimal", Op: "-", Args: []platform.Expr{platform.ExprReference("decimal", "externalamount"), platform.ExprReference("decimal", "receivedamount")}}
	source.accept(t, "build.object.edit", build.ObjectType, "HUB", map[string]any{
		"sets":  hubSets,
		"reads": hubReads,
		"fields": []map[string]any{
			{"name": "title", "title": "Title", "type": "text", "required": true, "search": true},
			{"name": "externalamount", "title": "External amount", "type": "decimal", "live": sumExt},
			{"name": "receivedamount", "title": "Received amount", "type": "decimal", "live": sumRec},
			{"name": "difference", "title": "Difference", "type": "decimal", "live": diffExpr},
			{"name": "inflightamount", "title": "In-flight amount", "type": "decimal", "live": sumInf},
			{"name": "inflightcount", "title": "In-flight count", "type": "integer", "live": cntInf},
		},
	})
	source.accept(t, "build.object.publish", build.ObjectType, "HUB", nil)

	// 4. Graph propagation objects (3-level DAG + cycle detection + idempotent draft upsert).
	source.accept(t, "build.object.create", build.ObjectType, "EDGE", map[string]any{
		"name": "graphlink", "title": "Graph link", "plural": "Graph links",
		"fields": []map[string]any{
			{"name": "run", "title": "Run", "type": "text", "default": "ALL"},
			{"name": "parent", "title": "Parent", "type": "reference", "ref": unitType, "required": true},
			{"name": "child", "title": "Child", "type": "reference", "ref": unitType, "required": true},
			{"name": "ratio", "title": "Ratio", "type": "decimal", "required": true},
			{"name": "loss", "title": "Loss", "type": "decimal", "default": "0"},
		},
	})
	source.accept(t, "build.object.publish", build.ObjectType, "EDGE", nil)

	source.accept(t, "build.object.create", build.ObjectType, "DEMAND", map[string]any{
		"name": "demandrow", "title": "Demand row", "plural": "Demand rows",
		"fields": []map[string]any{
			{"name": "node", "title": "Node", "type": "reference", "ref": unitType, "required": true},
			{"name": "quantity", "title": "Quantity", "type": "decimal", "required": true},
			{"name": "due", "title": "Due", "type": "date", "required": true},
			{"name": "docid", "title": "Doc ID", "type": "text", "required": true},
		},
	})
	source.accept(t, "build.object.publish", build.ObjectType, "DEMAND", nil)

	source.accept(t, "build.object.create", build.ObjectType, "CREDIT", map[string]any{
		"name": "creditrow", "title": "Credit row", "plural": "Credit rows",
		"fields": []map[string]any{
			{"name": "node", "title": "Node", "type": "reference", "ref": unitType, "required": true},
			{"name": "quantity", "title": "Quantity", "type": "decimal", "required": true},
		},
	})
	source.accept(t, "build.object.publish", build.ObjectType, "CREDIT", nil)

	proposalFields := []map[string]any{
		{"name": "node", "title": "Node", "type": "reference", "ref": unitType, "required": true},
		{"name": "quantity", "title": "Quantity", "type": "decimal", "required": true},
		{"name": "due", "title": "Due", "type": "date", "required": true},
		{"name": "start", "title": "Start", "type": "date"},
		{"name": "level", "title": "Level", "type": "integer"},
		{"name": "origin", "title": "Origin", "type": "text"},
		{"name": "trace", "title": "Trace", "type": "longtext"},
		{"name": "sourcetype", "title": "Source type", "type": "text"},
		{"name": "sourceid", "title": "Source", "type": "text", "refType": "sourcetype"},
	}
	for _, spec := range []struct{ id, name, title string }{
		{"BPLAN", "branchplan", "Branch plan"},
		{"LPLAN", "leafplan", "Leaf plan"},
	} {
		fullType := "invariantproject." + spec.name
		confirmProp := ontology.Action{
			Name: "confirm", Title: "Confirm", From: []string{"draft"}, Roles: []string{ontology.BuilderRole},
			Transaction: &platform.BusinessTxn{Subject: fullType, Steps: []platform.TxnStep{
				{Name: "ok", Kind: "update", Target: platform.TxnRef{Type: fullType, ID: "$subject"}, Changes: []platform.TxnSet{
					{Field: "state", Expr: exprPtr(lit("confirmed", "choice"))},
				}},
			}},
		}
		source.accept(t, "build.object.create", build.ObjectType, spec.id, map[string]any{
			"name": spec.name, "title": spec.title, "plural": spec.title + "s",
			"states": []ontology.State{{Name: "draft", Title: "Draft"}, {Name: "confirmed", Title: "Confirmed"}},
			"fields": proposalFields,
		})
		source.accept(t, "build.object.publish", build.ObjectType, spec.id, nil)
		source.accept(t, "build.actiontype.create", build.ActionTypeType, spec.id+"_CFM", map[string]any{"name": fullType + ".confirm", "object": fullType, "definition": confirmProp})
		source.accept(t, "build.actiontype.publish", build.ActionTypeType, spec.id+"_CFM", nil)
		source.accept(t, "build.object.publish", build.ObjectType, spec.id, nil)
	}

	for _, q := range []struct {
		id, name, obj string
	}{
		{"Q_NODES", "allnodes", unitType},
		{"Q_EDGES", "alledges", edgeType},
		{"Q_DEMANDS", "alldemands", demandType},
		{"Q_CREDITS", "allcredits", creditType},
	} {
		source.accept(t, "build.query.create", build.QueryType, q.id, map[string]any{"name": q.name, "title": q.name, "description": q.name, "object": q.obj, "sort": []string{"id"}, "limit": 200})
		source.accept(t, "build.query.publish", build.QueryType, q.id, nil)
	}

	source.accept(t, "build.object.create", build.ObjectType, "RUN", map[string]any{
		"name": "planrun", "title": "Plan run", "plural": "Plan runs",
		"states": []ontology.State{{Name: "draft", Title: "Draft"}, {Name: "completed", Title: "Completed"}},
		"fields": []map[string]any{{"name": "title", "title": "Title", "type": "text", "required": true, "search": true}},
	})
	source.accept(t, "build.object.publish", build.ObjectType, "RUN", nil)

	execRunAct := ontology.Action{
		Name: "execute", Title: "Execute", From: []string{"draft", "completed"}, Roles: []string{ontology.BuilderRole},
		Transaction: &platform.BusinessTxn{
			Subject: runType,
			Reads: &platform.RuleReadSpec{Collections: map[string]platform.RuleCollection{
				"nodes":   {App: build.ID, Query: "allnodes", Version: 1, Object: unitType, Policy: "complete"},
				"edges":   {App: build.ID, Query: "alledges", Version: 1, Object: edgeType, Policy: "complete"},
				"demands": {App: build.ID, Query: "alldemands", Version: 1, Object: demandType, Policy: "complete"},
				"credits": {App: build.ID, Query: "allcredits", Version: 1, Object: creditType, Policy: "complete"},
			}},
			Steps: []platform.TxnStep{
				{
					Name: "prop",
					Kind: "propagate",
					Propagate: &platform.TxnPropagate{
						Nodes:  "nodes",
						Offset: "offsetdays",
						Edges:  platform.TxnPropagateEdges{Set: "edges", Parent: "parent", Child: "child", Ratio: "ratio", Loss: "loss"},
						Demands: []platform.TxnPropagateInput{
							{Set: "demands", Node: "node", Quantity: "quantity", Due: "due", SourceType: "provider.voucherdoc", SourceField: "docid", Label: "order"},
						},
						Credits: []platform.TxnPropagateInput{
							{Set: "credits", Node: "node", Quantity: "quantity", Label: "available"},
						},
						Branch: &platform.TxnPropagateTarget{
							Type: branchPropType, Node: "node", Quantity: "quantity", Due: "due", Start: "start",
							Level: "level", Origin: "origin", Trace: "trace", SourceType: "sourcetype", SourceID: "sourceid",
						},
						Leaf: &platform.TxnPropagateTarget{
							Type: leafPropType, Node: "node", Quantity: "quantity", Due: "due", Start: "start",
							Level: "level", Origin: "origin", Trace: "trace", SourceType: "sourcetype", SourceID: "sourceid",
						},
					},
				},
				{Name: "done", Kind: "update", Target: platform.TxnRef{Type: runType, ID: "$subject"}, Changes: []platform.TxnSet{
					{Field: "state", Expr: exprPtr(lit("completed", "choice"))},
				}},
			},
		},
	}
	source.accept(t, "build.actiontype.create", build.ActionTypeType, "RUN_EXEC", map[string]any{"name": runType + ".execute", "object": runType, "definition": execRunAct})
	source.accept(t, "build.actiontype.publish", build.ActionTypeType, "RUN_EXEC", nil)
	source.accept(t, "build.object.publish", build.ObjectType, "RUN", nil)

	assets, err := source.App(build.ID).(*build.Build).ReleaseAssets()
	if err != nil {
		t.Fatal(err)
	}
	providerRoots := []platform.AssetRef{
		{App: build.ID, Kind: protocol.AssetContract, Name: "sample.metric"},
		{App: build.ID, Kind: protocol.AssetContract, Name: "sample.ingest"},
		{App: build.ID, Kind: platform.AssetObject, Name: unitType},
		{App: build.ID, Kind: platform.AssetObject, Name: factType},
		{App: build.ID, Kind: platform.AssetObject, Name: voucherType},
		{App: build.ID, Kind: platform.AssetObject, Name: edgeType},
		{App: build.ID, Kind: platform.AssetObject, Name: demandType},
		{App: build.ID, Kind: platform.AssetObject, Name: creditType},
		{App: build.ID, Kind: platform.AssetObject, Name: branchPropType},
		{App: build.ID, Kind: platform.AssetObject, Name: leafPropType},
		{App: build.ID, Kind: platform.AssetObject, Name: runType},
		{App: build.ID, Kind: platform.AssetQuery, Name: "allnodes"},
		{App: build.ID, Kind: platform.AssetQuery, Name: "alledges"},
		{App: build.ID, Kind: platform.AssetQuery, Name: "alldemands"},
		{App: build.ID, Kind: platform.AssetQuery, Name: "allcredits"},
	}
	providerCand, err := PublicationResolver().Candidate(providerRoots, assets)
	if err != nil {
		t.Fatal(err)
	}
	providerPkg := platform.PackageDescriptor{
		ID: "provider", Version: "1.0.0", Namespace: "provider", Title: "Provider",
		Contracts: &platform.PackageContracts{Provides: []string{metricContract}, Optional: []string{ingestContract}},
		Blueprint: &platform.PackageBlueprint{Roots: providerRoots, Assets: providerCand.Assets},
	}

	consumerRoots := []platform.AssetRef{
		{App: build.ID, Kind: platform.AssetObject, Name: hubType},
		{App: build.ID, Kind: platform.AssetObject, Name: receiverType},
	}
	consumerCand, err := PublicationResolver().Candidate(consumerRoots, assets)
	if err != nil {
		t.Fatal(err)
	}
	consumerPkg := platform.PackageDescriptor{
		ID: "consumer", Version: "1.0.0", Namespace: "consumer", Title: "Consumer",
		Contracts: &platform.PackageContracts{Provides: []string{ingestContract}, Optional: []string{metricContract}},
		Blueprint: &platform.PackageBlueprint{Roots: consumerRoots, Assets: consumerCand.Assets},
	}

	seat := protocol.Seat{Subjects: []string{"k58-admin"}, Member: platform.Member{ID: "k58-admin", Roles: map[string]string{"platform": "admin", build.ID: ontology.BuilderRole, "provider": ontology.BuilderRole, "consumer": ontology.BuilderRole}}}
	target := newInvariantTenant(t, "k58-target", seat)
	admin, _ := target.Member("k58-admin")
	submitPkg := func(id, schema string, payload any) {
		t.Helper()
		s := target.submission(schema, protocol.PackageType, id, payload)
		s.Authority, s.PrincipalId = "platform", admin.ID
		if _, refusal := target.Submit(admin, s, invariantClock); refusal != nil {
			t.Fatalf("%s %s: %s (%s)", schema, id, refusal.Code, refusal.Message)
		}
	}
	install := func(d platform.PackageDescriptor) {
		t.Helper()
		view, err := target.InspectPackage(context.Background(), protocol.PackageInspectRequest{Content: platform.Raw(d)})
		if err != nil || !view.Precheck.OK {
			t.Fatalf("inspect %s: %+v %v", d.ID, view.Precheck, err)
		}
		submitPkg(d.ID, packages.SchemaPackageImport, map[string]any{"id": d.ID, "digest": view.Digest})
		submitPkg(d.ID, packages.SchemaPackageInstall, map[string]any{"id": d.ID, "version": d.Version, "namespace": d.Namespace})
	}
	install(providerPkg)
	install(consumerPkg)

	bindProto := func(contract string, payload map[string]any) {
		t.Helper()
		s := target.submission(controlowner.SchemaProtocolBind, controlowner.ProtocolType, contract, payload)
		s.PrincipalId, s.Authority = admin.ID, "platform"
		if _, refusal := target.Submit(admin, s, invariantClock); refusal != nil {
			t.Fatalf("bind %s %+v: %v", contract, payload, refusal)
		}
	}
	submitAs := func(schema, typ, id string, payload any) {
		t.Helper()
		s := target.submission(schema, typ, id, payload)
		s.PrincipalId = admin.ID
		if _, refusal := target.Submit(admin, s, invariantClock); refusal != nil {
			s.IdempotencyKey += "-sim"
			sim, _ := target.Simulate(admin, "", "", s, invariantClock)
			t.Fatalf("%s %s/%s: %s (%s) trace=%+v issues=%+v", schema, typ, id, refusal.Code, refusal.Message, sim.Trace, sim.Issues)
		}
	}
	readRec := func(typ, id string) map[string]any {
		t.Helper()
		view, refusal := target.RecordOf(t.Context(), admin, typ, id, invariantClock)
		if refusal != nil {
			t.Fatalf("read %s/%s: %v", typ, id, refusal)
		}
		var out map[string]any
		_ = json.Unmarshal(platform.Raw(view.Record), &out)
		return out
	}

	// Bind metric & ingest, then PAUSE ingest to verify held in-flight projection.
	bindProto(metricContract, map[string]any{"provider": build.ID})
	bindProto(ingestContract, map[string]any{"provider": build.ID, "paused": true})

	submitAs("provider.unitnode.create", "provider.unitnode", "N-TOP", map[string]any{"title": "Top Assembly", "offsetdays": 2})
	submitAs("provider.voucherdoc.create", "provider.voucherdoc", "V1", map[string]any{"title": "Voucher 1", "unit": "N-TOP", "amount": 120})
	submitAs("provider.voucherdoc.postdoc", "provider.voucherdoc", "V1", map[string]any{})
	for range 5 {
		target.Dispatch(invariantClock.Add(time.Second))
		target.Round(invariantClock.Add(time.Second), 4)
	}

	// Verify via-target update worked, metric read reflects 120, and ingest is held in inflight projection!
	if got := readRec("provider.unitnode", "N-TOP")["statusnote"]; got != "posted" {
		t.Fatalf("expected unit statusnote=posted via $subject.<ref> target, got %v", got)
	}
	hub := readRec("consumer.reconcilehub", "DEFAULT")
	if hub["externalamount"] != float64(120) || hub["receivedamount"] != float64(0) || hub["difference"] != float64(120) || hub["inflightamount"] != float64(120) || hub["inflightcount"] != float64(1) {
		t.Fatalf("expected held in-flight difference=120 & count=1, got %+v", hub)
	}
	inflightPage, refusal := target.objectOwner(target.Projection).RecordSetPage(admin, mustEntityInfo(t, target.Tenant, "consumer.reconcilehub"), "DEFAULT", "inflight", invariantClock)
	if refusal != nil || len(inflightPage.Records) != 1 {
		t.Fatalf("expected 1 inline inflight row on DEFAULT hub, got %d (%v)", len(inflightPage.Records), refusal)
	}
	var infRow map[string]any
	_ = json.Unmarshal(inflightPage.Records[0], &infRow)
	if infRow["state"] != "held" || infRow["originid"] != "V1" || infRow["origintype"] != "provider.voucherdoc" {
		t.Fatalf("unexpected inflight row: %+v", infRow)
	}

	// Verify unbinding sample.metric/1 cleanly evaluates optional protocol collection to 0.
	bindProto(metricContract, map[string]any{"provider": ""})
	if got := readRec("consumer.reconcilehub", "DEFAULT")["externalamount"]; got != float64(0) {
		t.Fatalf("expected unbound metric collection to evaluate to 0, got %v", got)
	}
	bindProto(metricContract, map[string]any{"provider": build.ID})

	// Resume ingest delivery -> held request settles, inflight drops to 0, difference = 0!
	bindProto(ingestContract, map[string]any{"provider": build.ID, "paused": false})
	for range 10 {
		target.Dispatch(invariantClock.Add(2 * time.Second))
		target.Round(invariantClock.Add(2*time.Second), 4)
	}
	hub = readRec("consumer.reconcilehub", "DEFAULT")
	if hub["externalamount"] != float64(120) || hub["receivedamount"] != float64(120) || hub["difference"] != float64(0) || hub["inflightcount"] != float64(0) {
		t.Fatalf("expected settled difference=0 & inflightcount=0, got %+v", hub)
	}

	// Verify transactional ledger reversal (`kind: "reverse"`).
	v1 := readRec("provider.voucherdoc", "V1")
	factID, _ := v1["fact"].(string)
	submitAs("provider.voucherdoc.voiddoc", "provider.voucherdoc", "V1", map[string]any{"on": "2026-04-12"})
	if got := readRec("provider.factline", factID)["reversed"]; got != true {
		t.Fatalf("expected original ledger fact %s reversed=true, got %v", factID, got)
	}
	if got := readRec("provider.unitnode", "N-TOP")["statusnote"]; got != "voided" {
		t.Fatalf("expected unit statusnote=voided, got %v", got)
	}
	if got := readRec("consumer.reconcilehub", "DEFAULT")["externalamount"]; got != float64(0) {
		t.Fatalf("expected externalamount=0 after ledger reversal, got %v", got)
	}

	// Verify 3-level DAG topological requirement propagation & idempotent upsert.
	submitAs("provider.unitnode.create", "provider.unitnode", "N-MID", map[string]any{"title": "Mid Subassembly", "offsetdays": 3})
	submitAs("provider.unitnode.create", "provider.unitnode", "N-RAW", map[string]any{"title": "Raw Material", "offsetdays": 5})
	submitAs("provider.graphlink.create", "provider.graphlink", "E1", map[string]any{"parent": "N-TOP", "child": "N-MID", "ratio": 2, "loss": 0})
	submitAs("provider.graphlink.create", "provider.graphlink", "E2", map[string]any{"parent": "N-MID", "child": "N-RAW", "ratio": 3, "loss": 0.1})
	submitAs("provider.graphlink.create", "provider.graphlink", "E3", map[string]any{"parent": "N-TOP", "child": "N-RAW", "ratio": 1, "loss": 0})
	submitAs("provider.demandrow.create", "provider.demandrow", "DEM1", map[string]any{"node": "N-TOP", "quantity": 10, "due": "2026-05-20", "docid": "V1"})
	submitAs("provider.creditrow.create", "provider.creditrow", "CRD-TOP", map[string]any{"node": "N-TOP", "quantity": 4})
	submitAs("provider.creditrow.create", "provider.creditrow", "CRD-MID", map[string]any{"node": "N-MID", "quantity": 3})
	submitAs("provider.creditrow.create", "provider.creditrow", "CRD-RAW", map[string]any{"node": "N-RAW", "quantity": 5})
	submitAs("provider.planrun.create", "provider.planrun", "RUN1", map[string]any{"title": "MRP Run 1"})
	submitAs("provider.planrun.execute", "provider.planrun", "RUN1", map[string]any{})

	listAll := func(typ string) []map[string]any {
		t.Helper()
		page, refusal := target.Records(t.Context(), admin, typ, platform.Query{Sort: []string{"id"}, Limit: 100}, invariantClock)
		if refusal != nil {
			t.Fatalf("list %s: %v", typ, refusal)
		}
		var out []map[string]any
		_ = json.Unmarshal(platform.Raw(page.Records), &out)
		return out
	}
	branches := listAll("provider.branchplan")
	leaves := listAll("provider.leafplan")
	if len(branches) != 2 {
		t.Fatalf("expected 2 branch plans (N-TOP, N-MID), got %+v", branches)
	}
	branchIDs := map[string]string{}
	for _, row := range branches {
		node, _ := row["node"].(string)
		branchIDs[node], _ = row["id"].(string)
		switch node {
		case "N-TOP":
			if row["quantity"] != float64(6) || row["due"] != "2026-05-20" || row["start"] != "2026-05-18" || row["level"] != float64(0) || row["sourceid"] != "V1" {
				t.Fatalf("unexpected N-TOP branch plan: %+v", row)
			}
		case "N-MID":
			if row["quantity"] != float64(9) || row["due"] != "2026-05-18" || row["start"] != "2026-05-15" || row["level"] != float64(1) || row["sourceid"] != "V1" {
				t.Fatalf("unexpected N-MID branch plan: %+v", row)
			}
		default:
			t.Fatalf("unexpected branch node: %+v", row)
		}
	}
	totalLeaf := 0.0
	leafIDsBefore := []string{}
	for _, row := range leaves {
		id, _ := row["id"].(string)
		leafIDsBefore = append(leafIDsBefore, id)
		qty, _ := row["quantity"].(float64)
		totalLeaf += qty
		if row["node"] != "N-RAW" || row["level"] != float64(2) || row["sourceid"] != "V1" || !strings.Contains(row["trace"].(string), "=") {
			t.Fatalf("unexpected N-RAW leaf plan: %+v", row)
		}
	}
	if math.Abs(totalLeaf-30.7) > 1e-6 {
		t.Fatalf("expected total N-RAW net requirement = 30.7 (29.7 + 6 - 5), got %v (%+v)", totalLeaf, leaves)
	}

	// Confirm one leaf proposal, then re-run execute -> unconfirmed drafts update in place without duplicating, confirmed stays untouched!
	confirmedLeafID := leafIDsBefore[0]
	submitAs("provider.leafplan.confirm", "provider.leafplan", confirmedLeafID, map[string]any{})
	submitAs("provider.planrun.execute", "provider.planrun", "RUN1", map[string]any{})
	branchesAfter := listAll("provider.branchplan")
	if len(branchesAfter) != 2 || branchesAfter[0]["id"] != branches[0]["id"] || branchesAfter[1]["id"] != branches[1]["id"] {
		t.Fatalf("expected idempotent branch draft upsert without new IDs, before=%+v after=%+v", branches, branchesAfter)
	}
	confirmedLeaf := readRec("provider.leafplan", confirmedLeafID)
	if confirmedLeaf["state"] != "confirmed" {
		t.Fatalf("confirmed proposal must remain confirmed across re-runs: %+v", confirmedLeaf)
	}

	// Introduce a cycle edge (N-RAW -> N-TOP) and verify cycle refusal!
	submitAs("provider.graphlink.create", "provider.graphlink", "ECYCLE", map[string]any{"parent": "N-RAW", "child": "N-TOP", "ratio": 1, "loss": 0})
	cycleSub := target.submission("provider.planrun.execute", "provider.planrun", "RUN1", map[string]any{})
	cycleSub.PrincipalId = admin.ID
	if _, cycleErr := target.Submit(admin, cycleSub, invariantClock); cycleErr == nil || cycleErr.Code != pb.ErrorCode_ERROR_CODE_CONFLICT {
		t.Fatalf("expected cycle refusal with CONFLICT, got %+v", cycleErr)
	}

	// Replay byte-equivalence check.
	replayed := target.restore(t, target.entries, seat)
	replayed.Files = target.files()
	replayed.invalidateReadImage()
	if !reflect.DeepEqual(target.recordImage(t), replayed.recordImage(t)) {
		t.Fatal("replay record image differs")
	}
}

func mustEntityInfo(t *testing.T, tenant *Tenant, typ string) platform.RecordTypeInfo {
	t.Helper()
	info, ok := tenant.Entity(typ)
	if !ok {
		t.Fatalf("entity %s not found", typ)
	}
	return info
}
