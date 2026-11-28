package host

import (
	"context"
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/devops/packages"
	"platformserver/services/objects"
	"platformserver/services/ontology"
	build "platformserver/services/resources/authoring"
	controlowner "platformserver/services/security/control"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"
)

func TestInvariantK56DeferredReplyAndPolymorphicProvenance(t *testing.T) {
	source := newInvariantTenant(t, "deferred-reply-source")
	lit := func(v any, kind string) platform.Expr {
		return platform.Expr{Kind: kind, Lit: &platform.ExprLiteral{Value: platform.Raw(v)}}
	}
	const (
		claimContract = "sample.claim/1"
		postContract  = "sample.post/1"
		bucketType    = "invariantproject.bucket"
		entryType     = "invariantproject.entry"
		claimType     = "invariantproject.claim"
		docType       = "invariantproject.orderdoc"
	)

	bucketSets := []platform.RecordSet{
		{Name: "entries", Object: entryType + ".balance", Direction: "inverse", From: "entries", Limit: 200, Order: []platform.RecordSetSort{{Expr: platform.ExprReference("text", "id")}}},
		{Name: "claims", Object: claimType, Direction: "inverse", From: "claims", Limit: 200, Order: []platform.RecordSetSort{{Expr: platform.ExprReference("text", "id")}}},
	}
	source.accept(t, "build.object.create", build.ObjectType, "BUCKET", map[string]any{
		"name": "bucket", "title": "Bucket",
		"fields": []map[string]any{{"name": "title", "title": "Title", "type": "text", "required": true, "search": true}},
	})
	source.accept(t, "build.object.publish", build.ObjectType, "BUCKET", nil)

	entryAmtFrom := platform.Expr{
		Kind: "integer", Op: "if",
		Args: []platform.Expr{
			{Kind: "boolean", Op: ">", Args: []platform.Expr{platform.ExprReference("integer", "qty"), lit(0, "integer")}},
			lit(0, "integer"),
			lit(-85, "integer"),
		},
	}
	source.accept(t, "build.object.create", build.ObjectType, "ENTRY", map[string]any{
		"name": "entry", "title": "Entry", "plural": "Entries", "ledger": true,
		"protocols": []protocol.ProtocolBinding{{Name: "sample.post", Version: 1, Actions: map[string]string{"post": "create", "reverse": "reverse"}}},
		"fields": []map[string]any{
			{"name": "bucket", "title": "Bucket", "type": "reference", "ref": bucketType, "inverse": "entries", "required": true},
			{"name": "qty", "title": "Quantity", "type": "integer", "required": true},
			{"name": "amount", "title": "Amount", "type": "integer", "required": true, "from": entryAmtFrom},
			{"name": "origintype", "title": "Origin type", "type": "text"},
			{"name": "originid", "title": "Origin", "type": "text", "refType": "origintype", "search": true},
		},
		"balances": []map[string]any{{"name": "balance", "title": "Balance", "keys": []string{"bucket"}, "measures": []string{"qty", "amount"}}},
	})
	source.accept(t, "build.object.publish", build.ObjectType, "ENTRY", nil)

	claimReads := &platform.RuleReadSpec{Collections: map[string]platform.RuleCollection{
		"entries": {App: build.ID, Object: entryType + ".balance", Set: "entries", Of: "$subject.bucket", Policy: "complete"},
		"claims":  {App: build.ID, Object: claimType, Set: "claims", Of: "$subject.bucket", Policy: "complete"},
	}}
	aggSum := func(src string, inner platform.Expr) platform.Expr {
		var out platform.Expr
		_ = json.Unmarshal(platform.Raw(map[string]any{"kind": "integer", "op": "sum", "agg": map[string]any{"source": src}, "in": inner}), &out)
		return out
	}
	sumEntries := aggSum("entries", platform.Expr{Kind: "integer", Op: "coalesce", Args: []platform.Expr{platform.ExprReference("integer", "item.qty"), lit(0, "integer")}})
	activeClaimCond := platform.Expr{Kind: "boolean", Op: "=", Args: []platform.Expr{platform.ExprReference("choice", "item.state"), lit("active", "choice")}}
	sumClaims := aggSum("claims", platform.Expr{Kind: "integer", Op: "if", Args: []platform.Expr{activeClaimCond, platform.ExprReference("integer", "item.qty"), lit(0, "integer")}})
	availExpr := platform.Expr{Kind: "integer", Op: "-", Args: []platform.Expr{sumEntries, sumClaims}}
	shortExpr := platform.Expr{
		Kind: "integer", Op: "if",
		Args: []platform.Expr{
			{Kind: "boolean", Op: ">", Args: []platform.Expr{platform.ExprReference("integer", "qty"), platform.ExprReference("integer", "available")}},
			{Kind: "integer", Op: "-", Args: []platform.Expr{platform.ExprReference("integer", "qty"), platform.ExprReference("integer", "available")}},
			lit(0, "integer"),
		},
	}
	claimGuard := platform.Expr{Kind: "boolean", Op: ">=", Args: []platform.Expr{platform.ExprReference("integer", "available"), platform.ExprReference("integer", "qty")}}
	releaseAct := ontology.Action{
		Name: "release", Title: "Release", From: []string{"active"}, Roles: []string{ontology.BuilderRole},
		Transaction: &platform.BusinessTxn{Subject: claimType, Steps: []platform.TxnStep{
			{Name: "mark", Kind: "update", Target: platform.TxnRef{Type: claimType, ID: "$subject"}, Changes: []platform.TxnSet{{Field: "state", Expr: func() *platform.Expr { e := lit("released", "choice"); return &e }()}}},
		}},
	}
	source.accept(t, "build.object.create", build.ObjectType, "CLAIM", map[string]any{
		"name": "claim", "title": "Claim", "plural": "Claims",
		"states": []ontology.State{{Name: "draft", Title: "Draft"}, {Name: "active", Title: "Active"}, {Name: "released", Title: "Released"}},
		"fields": []map[string]any{
			{"name": "bucket", "title": "Bucket", "type": "reference", "ref": bucketType, "inverse": "claims", "required": true},
			{"name": "qty", "title": "Quantity", "type": "integer", "required": true},
		},
	})
	source.accept(t, "build.object.publish", build.ObjectType, "CLAIM", nil)
	source.accept(t, "build.object.edit", build.ObjectType, "BUCKET", map[string]any{"sets": bucketSets})
	source.accept(t, "build.object.publish", build.ObjectType, "BUCKET", nil)
	source.accept(t, "build.actiontype.create", build.ActionTypeType, "CLAIM_RELEASE", map[string]any{"name": claimType + ".release", "object": claimType, "definition": releaseAct})
	source.accept(t, "build.actiontype.publish", build.ActionTypeType, "CLAIM_RELEASE", nil)
	source.accept(t, "build.object.edit", build.ObjectType, "CLAIM", map[string]any{
		"reads":     claimReads,
		"protocols": []protocol.ProtocolBinding{{Name: "sample.claim", Version: 1, Actions: map[string]string{"claim": "create", "release": "release"}}},
		"fields": []map[string]any{
			{"name": "bucket", "title": "Bucket", "type": "reference", "ref": bucketType, "inverse": "claims", "required": true},
			{"name": "qty", "title": "Quantity", "type": "integer", "required": true},
			{"name": "available", "title": "Available", "type": "integer", "from": availExpr},
			{"name": "shortage", "title": "Shortage", "type": "integer", "from": shortExpr},
		},
		"createTransaction": &platform.BusinessTxn{
			Subject: claimType,
			Guard:   &claimGuard,
			Message: "Shortage: requested {qty}, available {available}, short by {shortage}",
			Steps: []platform.TxnStep{
				{Name: "activate", Kind: "update", Target: platform.TxnRef{Type: claimType, ID: "$subject"}, Changes: []platform.TxnSet{{Field: "state", Expr: func() *platform.Expr { e := lit("active", "choice"); return &e }()}}},
			},
		},
	})
	source.accept(t, "build.object.publish", build.ObjectType, "CLAIM", nil)

	publishContract := func(id, name string) {
		t.Helper()
		providers := source.router().Providers(name + "/1")
		if len(providers) != 1 {
			t.Fatalf("providers %s: %+v", name, providers)
		}
		var acts []protocol.ContractAction
		for _, a := range providers[0].Provision.Protocol.Actions {
			acts = append(acts, protocol.ContractAction{Name: a.Schema, Title: a.Title, Payload: a.Payload})
		}
		source.accept(t, "build.contract.create", build.ContractType, id, map[string]any{"name": name, "major": 1, "title": name, "actions": acts})
		source.accept(t, "build.contract.publish", build.ContractType, id, nil)
	}
	publishContract("C_CLAIM", "sample.claim")
	publishContract("C_POST", "sample.post")

	source.accept(t, "build.object.create", build.ObjectType, "DOC", map[string]any{
		"name": "orderdoc", "title": "Order doc", "plural": "Order docs",
		"states": []ontology.State{{Name: "draft", Title: "Draft"}, {Name: "confirming", Title: "Confirming"}, {Name: "confirmed", Title: "Confirmed"}, {Name: "fulfilled", Title: "Fulfilled"}},
		"fields": []map[string]any{
			{"name": "title", "title": "Title", "type": "text", "required": true, "search": true},
			{"name": "bucket", "title": "Bucket", "type": "text", "required": true},
			{"name": "qty", "title": "Quantity", "type": "integer", "required": true},
			{"name": "shortage", "title": "Shortage", "type": "text", "readOnly": true},
			{"name": "settledamount", "title": "Settled amount", "type": "integer", "readOnly": true},
		},
	})
	source.accept(t, "build.object.publish", build.ObjectType, "DOC", nil)

	claimKey := platform.Expr{Op: "concat", Kind: "text", Args: []platform.Expr{lit("CLM-", "text"), platform.ExprReference("text", "id")}}
	postKey := platform.Expr{Op: "concat", Kind: "text", Args: []platform.Expr{lit("PST-", "text"), platform.ExprReference("text", "id")}}
	confirmAct := ontology.Action{
		Name: "confirm", Title: "Confirm", From: []string{"draft"}, Roles: []string{ontology.BuilderRole}, Uses: []string{claimContract + "#claim"},
		Transaction: &platform.BusinessTxn{Subject: docType, Steps: []platform.TxnStep{
			{Name: "wait", Kind: "update", Target: platform.TxnRef{Type: docType, ID: "$subject"}, Changes: []platform.TxnSet{{Field: "state", Expr: func() *platform.Expr { e := lit("confirming", "choice"); return &e }()}}},
			{Name: "askclaim", Kind: "request", Request: &platform.TxnRequest{
				Deferred: true, Optional: true, Key: &claimKey, Use: claimContract + "#claim", Reply: docType + ".claimreply",
				Payload: []platform.TxnSet{
					{Field: "bucket", Expr: func() *platform.Expr { e := platform.ExprReference("text", "bucket"); return &e }()},
					{Field: "qty", Expr: func() *platform.Expr { e := platform.ExprReference("integer", "qty"); return &e }()},
				},
			}},
		}},
	}
	acceptedCond := platform.Expr{Kind: "boolean", Op: "=", Args: []platform.Expr{platform.ExprReference("text", "input.outcome"), lit("accepted", "text")}}
	refusedCond := platform.Expr{Kind: "boolean", Op: "!=", Args: []platform.Expr{platform.ExprReference("text", "input.outcome"), lit("accepted", "text")}}
	claimReplyAct := ontology.Action{
		Name: "claimreply", Title: "Claim reply", ConfirmationOnly: true, From: []string{"confirming"}, Roles: []string{ontology.BuilderRole},
		Inputs: []ontology.Input{{Name: "outcome", Title: "Outcome", Type: "text", Required: true}, {Name: "reason", Title: "Reason", Type: "text"}},
		Transaction: &platform.BusinessTxn{Subject: docType, Steps: []platform.TxnStep{
			{Name: "ok", Kind: "update", When: &acceptedCond, Target: platform.TxnRef{Type: docType, ID: "$subject"}, Changes: []platform.TxnSet{{Field: "state", Expr: func() *platform.Expr { e := lit("confirmed", "choice"); return &e }()}}},
			{Name: "no", Kind: "update", When: &refusedCond, Target: platform.TxnRef{Type: docType, ID: "$subject"}, Changes: []platform.TxnSet{{Field: "shortage", Expr: func() *platform.Expr { e := platform.ExprReference("text", "input.reason"); return &e }()}}},
		}},
	}
	negQty := platform.Expr{Kind: "integer", Op: "-", Args: []platform.Expr{lit(0, "integer"), platform.ExprReference("integer", "qty")}}
	fulfillAct := ontology.Action{
		Name: "fulfill", Title: "Fulfill", From: []string{"confirmed"}, Roles: []string{ontology.BuilderRole}, Uses: []string{postContract + "#post", claimContract + "#release"},
		Transaction: &platform.BusinessTxn{Subject: docType, Steps: []platform.TxnStep{
			{Name: "done", Kind: "update", Target: platform.TxnRef{Type: docType, ID: "$subject"}, Changes: []platform.TxnSet{{Field: "state", Expr: func() *platform.Expr { e := lit("fulfilled", "choice"); return &e }()}}},
			{Name: "ship", Kind: "request", Request: &platform.TxnRequest{
				Deferred: true, Optional: true, Key: &postKey, Use: postContract + "#post", Reply: docType + ".fulfillreply",
				Payload: []platform.TxnSet{
					{Field: "bucket", Expr: func() *platform.Expr { e := platform.ExprReference("text", "bucket"); return &e }()},
					{Field: "qty", Expr: &negQty},
					{Field: "origintype", Expr: func() *platform.Expr { e := lit("consumer.orderdoc", "text"); return &e }()},
					{Field: "originid", Expr: func() *platform.Expr { e := platform.ExprReference("text", "id"); return &e }()},
				},
			}},
			{Name: "free", Kind: "request", Request: &platform.TxnRequest{
				Deferred: true, Optional: true, Key: &claimKey, Use: claimContract + "#release",
			}},
		}},
	}
	absAmt := platform.Expr{Kind: "integer", Op: "-", Args: []platform.Expr{lit(0, "integer"), {Kind: "integer", Op: "coalesce", Args: []platform.Expr{platform.ExprReference("integer", "input.amount"), lit(0, "integer")}}}}
	fulfillReplyAct := ontology.Action{
		Name: "fulfillreply", Title: "Fulfill reply", ConfirmationOnly: true, From: []string{"fulfilled"}, Roles: []string{ontology.BuilderRole},
		Inputs: []ontology.Input{{Name: "outcome", Title: "Outcome", Type: "text", Required: true}, {Name: "amount", Title: "Amount", Type: "integer"}},
		Transaction: &platform.BusinessTxn{Subject: docType, Steps: []platform.TxnStep{
			{Name: "rec", Kind: "update", When: &acceptedCond, Target: platform.TxnRef{Type: docType, ID: "$subject"}, Changes: []platform.TxnSet{{Field: "settledamount", Expr: &absAmt}}},
		}},
	}
	for _, item := range []struct {
		id  string
		def ontology.Action
	}{{"ACT_CR", claimReplyAct}, {"ACT_FR", fulfillReplyAct}, {"ACT_CFM", confirmAct}, {"ACT_FUL", fulfillAct}} {
		source.accept(t, "build.actiontype.create", build.ActionTypeType, item.id, map[string]any{"name": docType + "." + item.def.Name, "object": docType, "definition": item.def})
		source.accept(t, "build.actiontype.publish", build.ActionTypeType, item.id, nil)
	}
	source.accept(t, "build.object.publish", build.ObjectType, "DOC", nil)

	assets, err := source.App(build.ID).(*build.Build).ReleaseAssets()
	if err != nil {
		t.Fatal(err)
	}
	providerRoots := []platform.AssetRef{
		{App: build.ID, Kind: platform.AssetObject, Name: bucketType},
		{App: build.ID, Kind: platform.AssetObject, Name: entryType},
		{App: build.ID, Kind: platform.AssetObject, Name: claimType},
	}
	providerCand, err := PublicationResolver().Candidate(providerRoots, assets)
	if err != nil {
		t.Fatal(err)
	}
	providerPkg := platform.PackageDescriptor{ID: "provider", Version: "1.0.0", Namespace: "provider", Title: "Provider", Contracts: &platform.PackageContracts{Provides: []string{claimContract, postContract}}, Blueprint: &platform.PackageBlueprint{Roots: providerRoots, Assets: providerCand.Assets}}

	consumerRoots := []platform.AssetRef{
		{App: build.ID, Kind: platform.AssetObject, Name: docType},
		{App: build.ID, Kind: protocol.AssetContract, Name: "sample.claim"},
		{App: build.ID, Kind: protocol.AssetContract, Name: "sample.post"},
	}
	consumerCand, err := PublicationResolver().Candidate(consumerRoots, assets)
	if err != nil {
		t.Fatal(err)
	}
	consumerPkg := platform.PackageDescriptor{ID: "consumer", Version: "1.0.0", Namespace: "consumer", Title: "Consumer", Contracts: &platform.PackageContracts{Optional: []string{claimContract, postContract}}, Blueprint: &platform.PackageBlueprint{Roots: consumerRoots, Assets: consumerCand.Assets}}

	seat := protocol.Seat{Subjects: []string{"reply-admin"}, Member: platform.Member{ID: "reply-admin", Roles: map[string]string{"platform": "admin", "provider": ontology.BuilderRole, "consumer": ontology.BuilderRole}}}
	outsiderSeat := protocol.Seat{Subjects: []string{"consumer-only"}, Member: platform.Member{ID: "consumer-only", Roles: map[string]string{"consumer": ontology.BuilderRole}}}
	target := newInvariantTenant(t, "deferred-reply-target", seat, outsiderSeat)
	outsider, _ := target.Member("consumer-only")
	admin, _ := target.Member("reply-admin")
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
	for _, c := range []string{claimContract, postContract} {
		bind := target.submission(controlowner.SchemaProtocolBind, controlowner.ProtocolType, c, map[string]any{"provider": build.ID})
		bind.PrincipalId, bind.Authority = admin.ID, "platform"
		if _, refusal := target.Submit(admin, bind, invariantClock); refusal != nil {
			t.Fatalf("bind %s: %v", c, refusal)
		}
	}

	submitAs := func(schema, typ, id string, payload any) {
		t.Helper()
		s := target.submission(schema, typ, id, payload)
		s.PrincipalId = admin.ID
		if _, refusal := target.Submit(admin, s, invariantClock); refusal != nil {
			t.Fatalf("%s %s/%s: %v", schema, typ, id, refusal)
		}
	}
	submitAs("provider.bucket.create", "provider.bucket", "B1", map[string]any{"title": "Main"})
	submitAs("provider.entry.create", "provider.entry", "E-IN", map[string]any{"bucket": "B1", "qty": 4, "amount": 20})
	submitAs("consumer.orderdoc.create", "consumer.orderdoc", "D1", map[string]any{"title": "Order 1", "bucket": "B1", "qty": 3})
	submitAs("consumer.orderdoc.create", "consumer.orderdoc", "D2", map[string]any{"title": "Order 2", "bucket": "B1", "qty": 3})

	if _, refusal := target.Submit(admin, func() *pb.Submission {
		s := target.submission("consumer.orderdoc.claimreply", "consumer.orderdoc", "D1", map[string]any{"outcome": "accepted"})
		s.PrincipalId = admin.ID
		return s
	}(), invariantClock); refusal == nil {
		t.Fatal("human caller must not invoke confirmationOnly reply action directly")
	}

	var wg sync.WaitGroup
	for _, id := range []string{"D1", "D2"} {
		s := target.submission("consumer.orderdoc.confirm", "consumer.orderdoc", id, map[string]any{})
		s.PrincipalId = admin.ID
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, refusal := target.Submit(admin, s, invariantClock); refusal != nil {
				t.Errorf("confirm %s: %v", id, refusal)
			}
		}()
	}
	wg.Wait()
	for range 10 {
		target.Dispatch(invariantClock.Add(time.Second))
		target.Round(invariantClock.Add(time.Second), 4)
	}

	winner, loser := "", ""
	for _, id := range []string{"D1", "D2"} {
		view, refusal := target.RecordOf(t.Context(), admin, "consumer.orderdoc", id, invariantClock)
		if refusal != nil {
			t.Fatal(refusal)
		}
		var rec map[string]any
		_ = json.Unmarshal(platform.Raw(view.Record), &rec)
		switch rec["state"] {
		case "confirmed":
			winner = id
		case "confirming":
			loser = id
			wantMsg := "Shortage: requested 3, available 1, short by 2"
			if rec["shortage"] != wantMsg {
				t.Fatalf("loser %s shortage = %q, want %q", id, rec["shortage"], wantMsg)
			}
			if !slices.ContainsFunc(view.Deliveries, func(d objects.RecordDelivery) bool { return d.State == "rejected" && d.Message == wantMsg }) {
				t.Fatalf("loser %s deliveries missing rejected message: %+v", id, view.Deliveries)
			}
		default:
			t.Fatalf("unexpected state on %s: %+v", id, rec)
		}
	}
	if winner == "" || loser == "" {
		t.Fatalf("expected 1 confirmed and 1 confirming, got winner=%q loser=%q", winner, loser)
	}

	submitAs("consumer.orderdoc.fulfill", "consumer.orderdoc", winner, map[string]any{})
	for range 10 {
		target.Dispatch(invariantClock.Add(2 * time.Second))
		target.Round(invariantClock.Add(2*time.Second), 4)
	}
	winView, refusal := target.RecordOf(t.Context(), admin, "consumer.orderdoc", winner, invariantClock)
	if refusal != nil {
		t.Fatal(refusal)
	}
	var winRec map[string]any
	_ = json.Unmarshal(platform.Raw(winView.Record), &winRec)
	if winRec["settledamount"] != float64(85) {
		t.Fatalf("expected settledamount=85 from provider fact echo, got %+v", winRec)
	}
	linksView, refusal := target.objectOwner(target.Projection).RecordSection(admin, "consumer.orderdoc", winner, "links", invariantClock)
	if refusal != nil {
		t.Fatal(refusal)
	}
	if !slices.ContainsFunc(linksView.Related, func(r objects.Related) bool { return r.Type == "provider.entry" && r.Total == 1 }) {
		t.Fatalf("expected downstream provider.entry via refType on links section, got %+v", linksView.Related)
	}
	loserLinks, refusal := target.objectOwner(target.Projection).RecordSection(outsider, "consumer.orderdoc", loser, "links", invariantClock)
	if refusal != nil || loserLinks.Partial {
		t.Fatalf("outsider reading unlinked record must not get partial=true pollution: %+v %v", loserLinks, refusal)
	}
	winnerLinks, refusal := target.objectOwner(target.Projection).RecordSection(outsider, "consumer.orderdoc", winner, "links", invariantClock)
	if refusal != nil || !winnerLinks.Partial {
		t.Fatalf("outsider reading record with withheld refType link must get partial=true: %+v %v", winnerLinks, refusal)
	}

	submitPkg("provider", packages.SchemaPackageDrain, map[string]any{"id": "provider"})
	submitPkg("provider", packages.SchemaPackageRetire, map[string]any{"id": "provider"})
	submitAs("consumer.orderdoc.create", "consumer.orderdoc", "D3", map[string]any{"title": "Standalone", "bucket": "B1", "qty": 5})
	submitAs("consumer.orderdoc.confirm", "consumer.orderdoc", "D3", map[string]any{})
	d3View, refusal := target.RecordOf(t.Context(), admin, "consumer.orderdoc", "D3", invariantClock)
	if refusal != nil {
		t.Fatal(refusal)
	}
	var d3Rec map[string]any
	_ = json.Unmarshal(platform.Raw(d3View.Record), &d3Rec)
	if d3Rec["state"] != "confirmed" {
		t.Fatalf("expected retired optional provider to settle D3 directly to confirmed, got %+v", d3Rec)
	}

	replayed := target.restore(t, target.entries, seat, outsiderSeat)
	replayed.Files = target.files()
	replayed.invalidateReadImage()
	if !reflect.DeepEqual(target.recordImage(t), replayed.recordImage(t)) {
		t.Fatal("replay record image differs")
	}
}
