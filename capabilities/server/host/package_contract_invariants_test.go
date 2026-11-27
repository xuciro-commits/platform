package host

import (
	"context"
	"encoding/json"
	"platformserver/services/actions"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/devops/packages"
	"platformserver/services/ontology"
	build "platformserver/services/resources/authoring"
	controlowner "platformserver/services/security/control"
)

func TestInvariantK50PackageContractClosure(t *testing.T) {
	source := newInvariantTenant(t, "package-contract-source")
	makePackage := func(id, name string, object map[string]any, contracts *platform.PackageContracts) platform.PackageDescriptor {
		t.Helper()
		source.accept(t, "build.object.create", build.ObjectType, id, object)
		root := platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "invariantproject." + name}
		candidateID := invariantPublishData(t, source, source.member(t), []build.JointDraftRef{{Kind: platform.AssetObject, ID: id}}, id)
		candidate, err := PublicationResolver().ReadCandidate(candidateID, source.releases.Raw(candidateID))
		if err != nil {
			t.Fatal(err)
		}
		return platform.PackageDescriptor{ID: id, Version: "1.0.0", Namespace: id, Title: id, Contracts: contracts, Blueprint: &platform.PackageBlueprint{Roots: []platform.AssetRef{root}, Assets: candidate.Assets}}
	}
	const contract = "sample.post/1"
	providerObject := invariantObject("provider", "Provider")
	providerObject["protocols"] = []protocol.ProtocolBinding{{Name: "sample.post", Version: 1, Actions: map[string]string{"post": "create"}}}
	provider := makePackage("provider", "provider", providerObject, &platform.PackageContracts{Provides: []string{contract}})
	providers := source.router().Providers(contract)
	if len(providers) != 1 {
		t.Fatalf("source provider: %+v", providers)
	}
	source.accept(t, "build.contract.create", build.ContractType, "CONTRACT", map[string]any{"name": "sample.post", "major": 1, "title": "Posting", "actions": []protocol.ContractAction{{Name: "post", Title: "Post", Payload: providers[0].Provision.Protocol.Actions[0].Payload}}})
	source.accept(t, "build.contract.publish", build.ContractType, "CONTRACT", map[string]any{})
	const consumer = "invariantproject.optional"
	source.accept(t, "build.object.create", build.ObjectType, "optional", invariantObject("optional", "Optional"))
	source.accept(t, "build.object.publish", build.ObjectType, "optional", map[string]any{})
	action := ontology.Action{Name: "post", Title: "Post", Roles: []string{ontology.BuilderRole}, Uses: []string{contract + "#post"}, Transaction: &platform.BusinessTxn{Subject: consumer, Steps: []platform.TxnStep{{Name: "send", Kind: "request", Request: &platform.TxnRequest{Deferred: true, Use: contract + "#post", Payload: []platform.TxnSet{{Field: "note", Expr: &platform.Expr{Kind: "text", Lit: &platform.ExprLiteral{Value: platform.Raw("posted")}}}}}}}}}
	source.accept(t, "build.actiontype.create", build.ActionTypeType, "POST", map[string]any{"name": consumer + ".post", "object": consumer, "definition": action})
	source.accept(t, "build.actiontype.publish", build.ActionTypeType, "POST", map[string]any{})
	source.accept(t, "build.object.publish", build.ObjectType, "optional", map[string]any{})
	assets, err := source.App(build.ID).(*build.Build).ReleaseAssets()
	if err != nil {
		t.Fatal(err)
	}
	roots := []platform.AssetRef{{App: build.ID, Kind: platform.AssetObject, Name: consumer}, {App: build.ID, Kind: protocol.AssetContract, Name: "sample.post"}}
	candidate, err := PublicationResolver().Candidate(roots, assets)
	if err != nil {
		t.Fatal(err)
	}
	optional := platform.PackageDescriptor{ID: "optional", Version: "1.0.0", Namespace: "optional", Title: "Optional", Contracts: &platform.PackageContracts{Optional: []string{contract}}, Blueprint: &platform.PackageBlueprint{Roots: roots, Assets: candidate.Assets}}
	required := makePackage("required", "required", invariantObject("required", "Required"), &platform.PackageContracts{Requires: []string{contract}})

	seat := protocol.Seat{Subjects: []string{"contract-admin"}, Member: platform.Member{ID: "contract-admin", Roles: map[string]string{"platform": "admin", "invariantproject": ontology.BuilderRole}}}
	target := newInvariantTenant(t, "package-contract-target", seat)
	admin, _ := target.Member("contract-admin")
	inspect := func(d platform.PackageDescriptor) protocol.PackagePreview {
		t.Helper()
		view, err := target.InspectPackage(context.Background(), protocol.PackageInspectRequest{Content: platform.Raw(d)})
		if err != nil {
			t.Fatal(err)
		}
		return view
	}
	submit := func(d platform.PackageDescriptor, schema string, payload any) *platform.Error {
		t.Helper()
		s := target.submission(schema, protocol.PackageType, d.ID, payload)
		s.Authority, s.PrincipalId = "platform", admin.ID
		_, refusal := target.Submit(admin, s, invariantClock)
		return refusal
	}
	install := func(d platform.PackageDescriptor) {
		t.Helper()
		view := inspect(d)
		if !view.Precheck.OK {
			t.Fatalf("cannot install %s: %+v", d.ID, view.Precheck)
		}
		if refusal := submit(d, packages.SchemaPackageImport, map[string]any{"id": d.ID, "digest": view.Digest}); refusal != nil {
			t.Fatalf("import %s: %v", d.ID, refusal)
		}
		if refusal := submit(d, packages.SchemaPackageInstall, map[string]any{"id": d.ID, "version": d.Version, "namespace": d.Namespace}); refusal != nil {
			t.Fatalf("install %s: %v", d.ID, refusal)
		}
	}
	if check := inspect(required).Precheck; check.OK || !strings.Contains(strings.Join(check.Problems, " "), contract) {
		t.Fatalf("missing required contract was not refused: %+v", check)
	}
	install(optional)
	status := func(tenant *invariantTenant, providers int, bound bool) {
		t.Helper()
		member, _ := tenant.Member(admin.ID)
		prep, refusal := tenant.PackagePreparation(t.Context(), member, optional.ID, optional.Version, invariantClock)
		if refusal != nil || len(prep.Contracts) != 1 || prep.Contracts[0].Contract != contract || prep.Contracts[0].Required || len(prep.Contracts[0].Providers) != providers || prep.Contracts[0].Bound != bound || !slices.Contains(prep.Contracts[0].Entries, consumer+".post") {
			t.Fatalf("optional contract availability: %+v, %#v", prep, refusal)
		}
	}
	const installedConsumer = "optional.optional"
	status(target, 0, false)
	offered := func(tenant *invariantTenant) bool {
		return slices.ContainsFunc(tenant.Catalog(tenant.member(t)), func(a platform.Action) bool { return a.Schema == installedConsumer+".post" })
	}
	if offered(target) {
		t.Fatal("unbound optional entry must be hidden")
	}
	target.accept(t, installedConsumer+".create", installedConsumer, "C1", map[string]any{"note": "standalone"})
	install(provider)
	status(target, 1, false)
	bind := target.submission(controlowner.SchemaProtocolBind, controlowner.ProtocolType, contract, map[string]any{"provider": build.ID})
	bind.PrincipalId, bind.Authority = admin.ID, "platform"
	if _, refusal := target.Submit(admin, bind, invariantClock); refusal != nil {
		t.Fatalf("bind: %s", refusal.Message)
	}
	if !offered(target) {
		t.Fatal("bound optional entry is absent")
	}
	status(target, 1, true)
	prep, refusal := target.PackagePreparation(t.Context(), admin, provider.ID, provider.Version, invariantClock)
	if refusal != nil || len(prep.Contracts) != 1 || !prep.Contracts[0].Provided || !slices.Equal(prep.Contracts[0].Consumers, []string{optional.ID}) || !slices.Contains(prep.Contracts[0].Entries, consumer+".post") {
		t.Fatalf("provider review did not expose consumer entries: %+v %v", prep, refusal)
	}
	post := target.submission(installedConsumer+".post", installedConsumer, "C1", map[string]any{})
	if _, refusal := target.Submit(target.member(t), post, invariantClock); refusal != nil {
		t.Fatalf("post: %s", refusal.Message)
	}
	if _, refusal := target.Submit(target.member(t), post, invariantClock); refusal != nil {
		t.Fatalf("duplicate post: %s", refusal.Message)
	}

	effects := func() []platform.Effect {
		var out []platform.Effect
		for _, x := range target.Effects(invariantClock) {
			if x.Endpoint == actions.DeferredRequestEndpoint {
				out = append(out, x)
			}
		}
		return out
	}
	pending := effects()
	if len(pending) != 1 || pending[0].State != "pending" || strings.Contains(string(target.recordImage(t)["provider.provider"]), "posted") {
		t.Fatalf("expected durable pending request before delivery: %+v", pending)
	}
	effectID := pending[0].ID
	unbind := target.submission(controlowner.SchemaProtocolBind, controlowner.ProtocolType, contract, map[string]any{"provider": ""})
	unbind.PrincipalId, unbind.Authority = admin.ID, "platform"
	if _, refusal := target.Submit(admin, unbind, invariantClock); refusal != nil {
		t.Fatal(refusal)
	}
	target.Dispatch(invariantClock)
	failed := effects()
	if len(failed) != 1 || failed[0].ID != effectID || failed[0].State != "rejected" || failed[0].Error == "" {
		t.Fatalf("unavailable provider must leave a retryable refusal: %+v", failed)
	}
	if !strings.Contains(string(target.recordImage(t)[installedConsumer]), "standalone") {
		t.Fatal("the source fact was lost")
	}
	rebound := target.submission(controlowner.SchemaProtocolBind, controlowner.ProtocolType, contract, map[string]any{"provider": build.ID})
	rebound.PrincipalId, rebound.Authority = admin.ID, "platform"
	if _, refusal := target.Submit(admin, rebound, invariantClock); refusal != nil {
		t.Fatal(refusal)
	}
	retry := target.submission(controlowner.SchemaEffectRetry, controlowner.EffectType, effectID, map[string]any{})
	retry.PrincipalId, retry.Authority = admin.ID, "platform"
	if _, refusal := target.Submit(admin, retry, invariantClock.Add(time.Second)); refusal != nil {
		t.Fatalf("retry: %v", refusal)
	}
	recovery := target.restore(t, target.entries, seat)
	recovery.Files = target.files()
	recovery.invalidateReadImage()
	pendingRetry := effects()[0]
	out := recovery.router().DeliverDeferredRequest(pendingRetry, invariantClock.Add(2*time.Second), recovery.Member, recovery.ReplayKnownSubmission, recovery.Submit)
	if out.Result != "delivered" {
		t.Fatalf("independent provider acceptance: %+v", out)
	}
	restarted := recovery.restore(t, append(slices.Clone(target.entries), recovery.entries...), seat)
	restarted.Files = target.files()
	restarted.invalidateReadImage()
	restarted.Dispatch(invariantClock.Add(2 * time.Second))
	var recoveredPosts []json.RawMessage
	if raw := restarted.recordImage(t)["provider.provider"]; json.Unmarshal(raw, &recoveredPosts) != nil || len(recoveredPosts) != 1 {
		t.Fatalf("provider accepted before settlement must not execute again after replay: %s", raw)
	}
	if resumed := restarted.Effects(invariantClock); !slices.ContainsFunc(resumed, func(x platform.Effect) bool { return x.ID == effectID && x.State == "delivered" && x.Attempts == 2 }) {
		t.Fatalf("replayed effect did not settle: %+v", resumed)
	}
	target.Dispatch(invariantClock.Add(2 * time.Second))
	settled := effects()
	if len(settled) != 1 || settled[0].State != "delivered" || settled[0].Attempts != 2 {
		t.Fatalf("retry should deliver the original request: %+v", settled)
	}
	target.Dispatch(invariantClock.Add(3 * time.Second))
	raw := target.recordImage(t)["provider.provider"]
	var posted []json.RawMessage
	if json.Unmarshal(raw, &posted) != nil || len(posted) != 1 || !strings.Contains(string(posted[0]), "posted") {
		t.Fatalf("expected exactly one provider fact: %s", raw)
	}
	install(required)
	if refusal := submit(provider, packages.SchemaPackageDrain, map[string]any{"id": provider.ID}); refusal == nil || !strings.Contains(refusal.Message, contract) {
		t.Fatalf("required consumer did not block provider removal: %v", refusal)
	}
	for _, d := range []platform.PackageDescriptor{required, provider} {
		for _, schema := range []string{packages.SchemaPackageDrain, packages.SchemaPackageRetire} {
			if refusal := submit(d, schema, map[string]any{"id": d.ID}); refusal != nil {
				t.Fatalf("%s %s: %v", schema, d.ID, refusal)
			}
		}
	}
	status(target, 0, false)
	if offered(target) {
		t.Fatal("retired provider leaves optional entry offered")
	}
	if _, _, refusal := target.Invoke(target.member(t), contract, "post", "retired-post", platform.Raw(map[string]any{"note": "retired"}), "retired-post", invariantClock); refusal == nil {
		t.Fatal("retired provider still accepts calls")
	}
	target.accept(t, installedConsumer+".create", installedConsumer, "C2", map[string]any{"note": "still standalone"})
	replayed := target.restore(t, target.entries, seat)
	replayed.Files = target.files()
	replayed.invalidateReadImage()
	status(replayed, 0, false)
	if offered(replayed) || !reflect.DeepEqual(target.recordImage(t), replayed.recordImage(t)) || !reflect.DeepEqual(target.Effects(invariantClock), replayed.Effects(invariantClock)) {
		t.Fatal("package replay differs")
	}
	install(provider)
	status(target, 1, true)
	if !offered(target) {
		t.Fatal("reinstall did not restore the bound entry")
	}
}

func TestInvariantK50OptionalDeferredRequestAndKey(t *testing.T) {
	source := newInvariantTenant(t, "optional-request-source")
	const contract = "sample.post/1"
	const ledgerType = "invariantproject.entry"
	source.accept(t, "build.object.create", build.ObjectType, "ENTRY", map[string]any{
		"name": "entry", "title": "Entry", "ledger": true,
		"protocols": []protocol.ProtocolBinding{{Name: "sample.post", Version: 1, Actions: map[string]string{"post": "create", "reverse": "reverse"}}},
		"fields":    []map[string]any{{"name": "bucket", "title": "Bucket", "type": "text", "required": true}, {"name": "qty", "title": "Quantity", "type": "integer", "required": true}},
		"balances":  []map[string]any{{"name": "balance", "title": "Balance", "keys": []string{"bucket"}, "measures": []string{"qty"}}},
	})
	source.accept(t, "build.object.publish", build.ObjectType, "ENTRY", nil)
	providers := source.router().Providers(contract)
	if len(providers) != 1 {
		t.Fatalf("ledger protocol provider: %+v", providers)
	}
	var actions []protocol.ContractAction
	for _, a := range providers[0].Provision.Protocol.Actions {
		name := strings.TrimPrefix(a.Schema, contract+"#")
		actions = append(actions, protocol.ContractAction{Name: name, Title: a.Title, Payload: a.Payload})
	}
	source.accept(t, "build.contract.create", build.ContractType, "CONTRACT", map[string]any{"name": "sample.post", "major": 1, "title": "Posting", "actions": actions})
	source.accept(t, "build.contract.publish", build.ContractType, "CONTRACT", map[string]any{})

	const docType = "invariantproject.voucher"
	docObj := invariantObject("voucher", "Voucher")
	docObj["states"] = []ontology.State{{Name: "draft", Title: "Draft"}, {Name: "posted", Title: "Posted"}, {Name: "voided", Title: "Voided"}}
	source.accept(t, "build.object.create", build.ObjectType, "VOUCHER", docObj)
	source.accept(t, "build.object.publish", build.ObjectType, "VOUCHER", nil)

	keyExpr := &platform.Expr{Op: "concat", Kind: "text", Args: []platform.Expr{
		{Kind: "text", Lit: &platform.ExprLiteral{Value: platform.Raw("DOC-")}},
		platform.ExprReference("text", "id"),
	}}
	postAct := ontology.Action{
		Name: "post", Title: "Post", From: []string{"draft"}, Roles: []string{ontology.BuilderRole}, Uses: []string{contract + "#post"},
		Transaction: &platform.BusinessTxn{Subject: docType, Steps: []platform.TxnStep{
			{Name: "mark", Kind: "update", Target: platform.TxnRef{Type: docType, ID: "$subject"}, Changes: []platform.TxnSet{{Field: "state", Expr: &platform.Expr{Kind: "choice", Lit: &platform.ExprLiteral{Value: platform.Raw("posted")}}}}},
			{Name: "send", Kind: "request", Request: &platform.TxnRequest{
				Deferred: true, Optional: true, Key: keyExpr, Use: contract + "#post",
				Payload: []platform.TxnSet{
					{Field: "bucket", Expr: &platform.Expr{Kind: "text", Lit: &platform.ExprLiteral{Value: platform.Raw("MAIN")}}},
					{Field: "qty", Expr: &platform.Expr{Kind: "integer", Lit: &platform.ExprLiteral{Value: platform.Raw(10)}}},
				},
			}},
		}},
	}
	voidAct := ontology.Action{
		Name: "void", Title: "Void", From: []string{"posted"}, Roles: []string{ontology.BuilderRole}, Uses: []string{contract + "#reverse"},
		Transaction: &platform.BusinessTxn{Subject: docType, Steps: []platform.TxnStep{
			{Name: "mark", Kind: "update", Target: platform.TxnRef{Type: docType, ID: "$subject"}, Changes: []platform.TxnSet{{Field: "state", Expr: &platform.Expr{Kind: "choice", Lit: &platform.ExprLiteral{Value: platform.Raw("voided")}}}}},
			{Name: "undo", Kind: "request", Request: &platform.TxnRequest{
				Deferred: true, Optional: true, Key: keyExpr, Use: contract + "#reverse",
			}},
		}},
	}
	for id, def := range map[string]ontology.Action{"POST": postAct, "VOID": voidAct} {
		source.accept(t, "build.actiontype.create", build.ActionTypeType, id, map[string]any{"name": docType + "." + def.Name, "object": docType, "definition": def})
		source.accept(t, "build.actiontype.publish", build.ActionTypeType, id, nil)
	}
	source.accept(t, "build.object.publish", build.ObjectType, "VOUCHER", nil)

	assets, err := source.App(build.ID).(*build.Build).ReleaseAssets()
	if err != nil {
		t.Fatal(err)
	}
	providerCand, err := PublicationResolver().Candidate([]platform.AssetRef{{App: build.ID, Kind: platform.AssetObject, Name: ledgerType}}, assets)
	if err != nil {
		t.Fatal(err)
	}
	providerPkg := platform.PackageDescriptor{ID: "provider", Version: "1.0.0", Namespace: "provider", Title: "Provider", Contracts: &platform.PackageContracts{Provides: []string{contract}}, Blueprint: &platform.PackageBlueprint{Roots: []platform.AssetRef{{App: build.ID, Kind: platform.AssetObject, Name: ledgerType}}, Assets: providerCand.Assets}}

	consumerRoots := []platform.AssetRef{{App: build.ID, Kind: platform.AssetObject, Name: docType}, {App: build.ID, Kind: protocol.AssetContract, Name: "sample.post"}}
	consumerCand, err := PublicationResolver().Candidate(consumerRoots, assets)
	if err != nil {
		t.Fatal(err)
	}
	consumerPkg := platform.PackageDescriptor{ID: "consumer", Version: "1.0.0", Namespace: "consumer", Title: "Consumer", Contracts: &platform.PackageContracts{Optional: []string{contract}}, Blueprint: &platform.PackageBlueprint{Roots: consumerRoots, Assets: consumerCand.Assets}}

	seat := protocol.Seat{Subjects: []string{"opt-admin"}, Member: platform.Member{ID: "opt-admin", Roles: map[string]string{"platform": "admin", "invariantproject": ontology.BuilderRole}}}
	target := newInvariantTenant(t, "optional-request-target", seat)
	admin, _ := target.Member("opt-admin")
	submitPkg := func(id, schema string, payload any) {
		t.Helper()
		s := target.submission(schema, protocol.PackageType, id, payload)
		s.Authority, s.PrincipalId = "platform", admin.ID
		if _, refusal := target.Submit(admin, s, invariantClock); refusal != nil {
			t.Fatalf("%s %s: %v", schema, id, refusal)
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

	install(consumerPkg)
	const installedDoc = "consumer.voucher"
	const installedLedger = "provider.entry"
	findPost := func(l *invariantTenant) (platform.Action, bool) {
		for _, a := range l.Catalog(l.member(t)) {
			if a.Schema == installedDoc+".post" {
				return a, true
			}
		}
		return platform.Action{}, false
	}
	act, ok := findPost(target)
	if !ok || !slices.Equal(act.Unconnected, []string{"Posting"}) {
		t.Fatalf("expected visible optional action with Unconnected=[Posting], got ok=%v act=%+v", ok, act)
	}
	target.accept(t, installedDoc+".create", installedDoc, "C1", map[string]any{"note": "standalone"})
	target.accept(t, installedDoc+".post", installedDoc, "C1", map[string]any{})
	if len(target.Effects(invariantClock)) != 0 {
		t.Fatalf("unbound optional request must not enqueue effects: %+v", target.Effects(invariantClock))
	}

	install(providerPkg)
	bind := target.submission(controlowner.SchemaProtocolBind, controlowner.ProtocolType, contract, map[string]any{"provider": build.ID})
	bind.PrincipalId, bind.Authority = admin.ID, "platform"
	if _, refusal := target.Submit(admin, bind, invariantClock); refusal != nil {
		t.Fatalf("bind: %v", refusal)
	}
	if act, ok = findPost(target); !ok || len(act.Unconnected) != 0 {
		t.Fatalf("bound optional action must clear Unconnected: ok=%v act=%+v", ok, act)
	}

	target.accept(t, installedDoc+".create", installedDoc, "C2", map[string]any{"note": "connected"})
	target.accept(t, installedDoc+".post", installedDoc, "C2", map[string]any{})
	target.Dispatch(invariantClock.Add(time.Second))
	factView, refusal := target.RecordOf(t.Context(), target.member(t), installedLedger, "DOC-C2", invariantClock)
	if refusal != nil || !strings.Contains(string(platform.Raw(factView.Record)), `"qty":10`) {
		t.Fatalf("expected deterministic ledger fact DOC-C2, got %+v %v", factView, refusal)
	}

	target.accept(t, installedDoc+".void", installedDoc, "C2", map[string]any{})
	target.Dispatch(invariantClock.Add(2 * time.Second))
	factAfter, refusal := target.RecordOf(t.Context(), target.member(t), installedLedger, "DOC-C2", invariantClock)
	if refusal != nil || !strings.Contains(string(platform.Raw(factAfter.Record)), `"reversed":true`) {
		t.Fatalf("expected DOC-C2 marked reversed, got %s %v", platform.Raw(factAfter.Record), refusal)
	}
	balPage, refusal := target.Records(t.Context(), target.member(t), installedLedger+".balance", platform.Query{}, invariantClock)
	if refusal != nil || len(balPage.Records) != 1 {
		t.Fatalf("expected balance row after void, got %s %v", platform.Raw(balPage), refusal)
	}
	var balRow map[string]any
	if json.Unmarshal(platform.Raw(balPage.Records[0]), &balRow) != nil || balRow["qty"] != nil && balRow["qty"] != float64(0) {
		t.Fatalf("expected balance returned to 0 after void, got %v", balRow)
	}

	submitPkg(providerPkg.ID, packages.SchemaPackageDrain, map[string]any{"id": providerPkg.ID})
	submitPkg(providerPkg.ID, packages.SchemaPackageRetire, map[string]any{"id": providerPkg.ID})
	if act, ok = findPost(target); !ok || !slices.Equal(act.Unconnected, []string{"Posting"}) {
		t.Fatalf("retired provider must restore Unconnected=[Posting]: ok=%v act=%+v", ok, act)
	}
	target.accept(t, installedDoc+".create", installedDoc, "C3", map[string]any{"note": "after retire"})
	target.accept(t, installedDoc+".post", installedDoc, "C3", map[string]any{})

	replayed := target.restore(t, target.entries, seat)
	replayed.Files = target.files()
	replayed.invalidateReadImage()
	if !reflect.DeepEqual(target.recordImage(t), replayed.recordImage(t)) {
		t.Fatal("replay record image differs")
	}
}
