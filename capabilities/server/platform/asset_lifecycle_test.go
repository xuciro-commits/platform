package platform

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
)

type lifecycleFixture struct {
	App
	id            string
	ledger        *Ledger
	kinds         []AssetKind
	assets        []ReleaseAsset
	pinned        ReleaseAsset
	requirements  []AssetEnvironmentRequirement
	plans         []AssetInstallation
	drafts        []AssetDraft
	prepared      int
	applied       int
	validated     int
	applyContext  AssetApplyContext
	prepareError  error
	validateError error
	bootstrap     func([]ReleaseAsset) (func(), error)
}

func newLifecycleFixture(id string, kinds ...AssetKind) *lifecycleFixture {
	catalog := NewCatalog(Action{Schema: id + ".metadata.create", Target: id + ".metadata", New: true}, Action{Schema: id + ".metadata.update", Target: id + ".metadata"})
	return &lifecycleFixture{id: id, kinds: kinds, ledger: NewLedger("target", id, catalog, id+".metadata")}
}
func (o *lifecycleFixture) Manifest() Manifest {
	return Manifest{ID: o.id, Version: "1", Actions: o.ledger.Catalog}
}
func (o *lifecycleFixture) AcceptedLedger() *Ledger                { return o.ledger }
func (o *lifecycleFixture) AssetKinds() []AssetKind                { return o.kinds }
func (o *lifecycleFixture) ReleaseAssets() ([]ReleaseAsset, error) { return o.assets, nil }
func (o *lifecycleFixture) ResolvePinned(AssetRef, string, ...string) (ReleaseAsset, error) {
	return o.pinned, nil
}
func (o *lifecycleFixture) AssetEnvironmentRequirements(ReleaseAsset) ([]AssetEnvironmentRequirement, error) {
	return o.requirements, nil
}
func (o *lifecycleFixture) ResolveAssetEnvironment(requirement AssetEnvironmentRequirement) (AssetEnvironmentTarget, error) {
	return AssetEnvironmentTarget{TargetRecord: requirement.Ref.Name}, nil
}
func (o *lifecycleFixture) BeginAssetBootstrap(assets []ReleaseAsset) (func(), error) {
	if o.bootstrap != nil {
		return o.bootstrap(assets)
	}
	return func() {}, nil
}
func (o *lifecycleFixture) ReleaseDrafts([]ReleaseAsset, AssetReleaseContext) ([]AssetDraft, error) {
	return o.drafts, nil
}
func (o *lifecycleFixture) PrepareReleaseAssets([]ReleaseAsset, AssetReleaseContext) ([]AssetInstallation, error) {
	o.prepared++
	return o.plans, o.prepareError
}
func (o *lifecycleFixture) DecodeAssetInstallation(schema string, image json.RawMessage) (AssetInstallation, error) {
	if len(o.plans) == 0 {
		return AssetInstallation{}, errors.New("unsupported installation")
	}
	plan := o.plans[0]
	plan.Schema = schema
	plan.Image = image
	return plan, nil
}
func (o *lifecycleFixture) ValidateAssetInstallation(AssetInstallation) error {
	o.validated++
	return o.validateError
}
func (o *lifecycleFixture) ApplyAssetInstallation(_ AssetInstallation, context AssetApplyContext) error {
	o.applied++
	o.applyContext = context
	return nil
}

func fixtureLifecycleAsset(app string, kind AssetKind, name string) ReleaseAsset {
	body, _ := json.Marshal(map[string]any{"name": name, "z": 2, "a": 1})
	return ReleaseAsset{Ref: AssetRef{App: app, Kind: kind, Name: name}, ContractVersion: 1, SourceVersion: "1", Body: body}
}
func TestAssetRegistryRejectsDuplicateAndForeignOwnership(t *testing.T) {
	owner := newLifecycleFixture("builder", AssetConnection)
	registry, err := NewAssetRegistry(owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(newLifecycleFixture("builder", AssetConnection)); err == nil {
		t.Fatal("duplicate lifecycle owner accepted")
	}
	if _, err := NewAssetRegistry(newLifecycleFixture("invalid", AssetConnection, AssetConnection)); err == nil {
		t.Fatal("duplicate kind accepted")
	}
	foreignLedger := newLifecycleFixture("builder", AssetConnection)
	foreignLedger.ledger = NewLedger("target", "foreign", NewCatalog())
	if _, err := NewAssetRegistry(foreignLedger); err == nil {
		t.Fatal("owner registered another app's ledger")
	}
	if _, err := registry.Owner(AssetRef{App: "builder", Kind: AssetDataset, Name: "records"}); err == nil {
		t.Fatal("undeclared kind acquired an owner")
	}
	owner.assets = []ReleaseAsset{fixtureLifecycleAsset("foreign", AssetConnection, "connection")}
	if _, err := registry.ReleaseAssets(); err == nil {
		t.Fatal("owner released a foreign asset")
	}
	owner.assets = []ReleaseAsset{fixtureLifecycleAsset("builder", AssetConnection, "connection"), fixtureLifecycleAsset("builder", AssetConnection, "connection")}
	if _, err := registry.ReleaseAssets(); err == nil {
		t.Fatal("duplicate descriptor silently replaced its predecessor")
	}
}
func TestAssetRegistryPinsAndOwnsCompleteDescriptorBytes(t *testing.T) {
	owner := newLifecycleFixture("builder", AssetConnection)
	asset := fixtureLifecycleAsset("builder", AssetConnection, "connection")
	owner.assets = []ReleaseAsset{asset}
	owner.pinned = asset
	registry, err := NewAssetRegistry(owner)
	if err != nil {
		t.Fatal(err)
	}
	owned, err := registry.ReleaseAssets()
	if err != nil {
		t.Fatal(err)
	}
	owner.assets[0].Body[0] = 'x'
	if !json.Valid(owned[0].Body) {
		t.Fatal("released descriptor aliases its owner")
	}
	owner.pinned = fixtureLifecycleAsset("builder", AssetConnection, "connection")
	if _, err := registry.ResolvePinned(asset.Ref, "2"); err == nil {
		t.Fatal("retained lookup fell back to latest")
	}
	owner.pinned.SourceVersion = "2"
	if _, err := registry.ResolvePinned(asset.Ref, "2"); err != nil {
		t.Fatal(err)
	}
	owner.pinned.Ref.App = "foreign"
	if _, err := registry.ResolvePinned(asset.Ref, "2"); err == nil {
		t.Fatal("retained lookup changed its owner")
	}
}
func TestAssetRegistryRequiresExplicitEnvironmentBeforePreparing(t *testing.T) {
	owner := newLifecycleFixture("builder", AssetConnection)
	asset := fixtureLifecycleAsset("builder", AssetConnection, "connection")
	owner.requirements = []AssetEnvironmentRequirement{{Ref: asset.Ref, Profile: "postgres", Credential: true}}
	owner.plans = []AssetInstallation{{Ref: asset.Ref, Schema: "builder.connection.publish", Record: "builder.metadata/connection", Image: json.RawMessage(`{"name":"connection"}`)}}
	registry, err := NewAssetRegistry(owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.PrepareReleaseAssets([]ReleaseAsset{asset}, AssetReleaseContext{}); err == nil || owner.prepared != 0 {
		t.Fatal("missing binding reached preparation")
	}
	context := AssetReleaseContext{Bindings: []AssetEnvironmentBinding{{Ref: asset.Ref, TargetRecord: "target-connection"}}}
	plans, err := registry.PrepareReleaseAssets([]ReleaseAsset{asset}, context)
	if err != nil || owner.prepared != 1 || owner.applied != 0 {
		t.Fatalf("preparation changed live installation: %v", err)
	}
	owner.plans[0].Image[0] = 'x'
	if !json.Valid(plans[0].Image) {
		t.Fatal("plan aliases mutable owner bytes")
	}
	if err := registry.ApplyInstallation(plans[0], AssetApplyContext{Private: true, Validate: false, Upgrade: true}); err != nil || owner.applied != 1 || !owner.applyContext.Private || owner.applyContext.Validate || !owner.applyContext.Upgrade {
		t.Fatalf("pure application lost its mode: %v", err)
	}
	context.Bindings = append(context.Bindings, context.Bindings[0])
	if _, err := registry.PrepareReleaseAssets([]ReleaseAsset{asset}, context); err == nil || owner.prepared != 1 {
		t.Fatal("duplicate binding reached owner preparation")
	}
}
func TestAssetRegistryRejectsForeignPlansAndDraftActions(t *testing.T) {
	owner := newLifecycleFixture("builder", AssetConnection)
	asset := fixtureLifecycleAsset("builder", AssetConnection, "connection")
	registry, err := NewAssetRegistry(owner)
	if err != nil {
		t.Fatal(err)
	}
	owner.plans = []AssetInstallation{{Ref: AssetRef{App: "foreign", Kind: AssetConnection, Name: "connection"}, Schema: "publish", Record: "builder.metadata/connection", Image: json.RawMessage(`{}`)}}
	if _, err := registry.PrepareReleaseAssets([]ReleaseAsset{asset}, AssetReleaseContext{}); err == nil || owner.applied != 0 {
		t.Fatal("foreign installation plan accepted")
	}
	owner.drafts = []AssetDraft{{Ref: asset.Ref, Type: "foreign.metadata", Name: "connection", Schema: "foreign.metadata.create", Image: json.RawMessage(`{}`)}}
	if _, err := registry.ReleaseDrafts([]ReleaseAsset{asset}, AssetReleaseContext{}); err == nil {
		t.Fatal("bootstrap used another owner's create action")
	}
	owner.drafts = []AssetDraft{{Ref: asset.Ref, Type: "builder.metadata", Name: "connection", Schema: "builder.metadata.update", Image: json.RawMessage(`{}`)}}
	if _, err := registry.ReleaseDrafts([]ReleaseAsset{asset}, AssetReleaseContext{}); err == nil {
		t.Fatal("bootstrap used an update instead of the original create action")
	}
	owner.drafts = []AssetDraft{{Ref: asset.Ref, Type: "builder.metadata", Name: "connection", Schema: "builder.metadata.create", Image: json.RawMessage(`{}`)}}
	if _, err := registry.ReleaseDrafts([]ReleaseAsset{asset}, AssetReleaseContext{}); err != nil {
		t.Fatal(err)
	}
	owner.drafts[0].Ref.Name = "unselected"
	if _, err := registry.ReleaseDrafts([]ReleaseAsset{asset}, AssetReleaseContext{}); err == nil {
		t.Fatal("bootstrap created an asset outside its candidate")
	}
}
func TestAssetRegistryHistoricalDecodeKeepsOriginalBytes(t *testing.T) {
	owner := newLifecycleFixture("builder", AssetConnection)
	asset := fixtureLifecycleAsset("builder", AssetConnection, "connection")
	owner.plans = []AssetInstallation{{Ref: asset.Ref, Record: "builder.metadata/connection"}}
	registry, err := NewAssetRegistry(owner)
	if err != nil {
		t.Fatal(err)
	}
	image := json.RawMessage(`{ "name" : "connection" }`)
	decoded, err := registry.DecodeAssetInstallation("builder", "builder.connection.publish", image)
	if err != nil || string(decoded.Image) != string(image) || decoded.Ref != asset.Ref || decoded.Record != "builder.metadata/connection" {
		t.Fatalf("legacy decoding replaced identity or bytes: %v", err)
	}
}

func TestAssetRegistrySeparatesImageReadsFromDependencyValidation(t *testing.T) {
	owner := newLifecycleFixture("builder", AssetConnection)
	asset := fixtureLifecycleAsset("builder", AssetConnection, "connection")
	owner.plans = []AssetInstallation{{Ref: asset.Ref, Schema: "builder.connection.publish", Record: "builder.metadata/connection", Image: json.RawMessage(`{"name":"connection"}`)}}
	owner.validateError = errors.New("dependency is not installed yet")
	registry, err := NewAssetRegistry(owner)
	if err != nil {
		t.Fatal(err)
	}
	plans, err := registry.PrepareReleaseAssets([]ReleaseAsset{asset}, AssetReleaseContext{})
	if err != nil || owner.validated != 0 {
		t.Fatalf("preparation ran dependency policy before installation: %v", err)
	}
	if _, err := registry.DecodeAssetInstallation("builder", plans[0].Schema, plans[0].Image); err != nil || owner.validated != 0 {
		t.Fatalf("historical reading ran current policy: %v", err)
	}
	if err := registry.ApplyInstallation(plans[0], AssetApplyContext{Private: true, Validate: true}); err == nil || owner.validated != 1 || owner.applied != 0 {
		t.Fatal("private validation refusal reached installation")
	}
	if err := registry.ApplyInstallation(plans[0], AssetApplyContext{Private: true}); err != nil || owner.validated != 1 || owner.applied != 1 {
		t.Fatalf("pure recovery re-ran current policy: %v", err)
	}
	forged := plans[0]
	forged.Record = "builder.metadata/another"
	if err := registry.ApplyInstallation(forged, AssetApplyContext{}); err == nil || owner.applied != 1 {
		t.Fatal("pure recovery accepted an image with another record identity")
	}
	owner.validateError = nil
	if err := registry.ApplyInstallation(plans[0], AssetApplyContext{Private: true, Validate: true}); err != nil || owner.validated != 2 || owner.applied != 2 {
		t.Fatalf("dependency-ready trial did not validate once and apply: %v", err)
	}
}

func TestAssetRegistryPinsSourceAndPageContentIndependently(t *testing.T) {
	owner := newLifecycleFixture("builder", AssetPage)
	page := Page{Name: "desk", Title: "Original desk", Object: AssetRef{App: "builder", Kind: AssetObject, Name: "builder.row"}, Layout: "list-detail"}
	asset, err := PageReleaseAsset("builder", "1", page)
	if err != nil {
		t.Fatal(err)
	}
	owner.pinned = asset
	registry, err := NewAssetRegistry(owner)
	if err != nil {
		t.Fatal(err)
	}
	content, err := PageContentVersion(page)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.ResolvePinned(asset.Ref, "1", content); err != nil {
		t.Fatal(err)
	}
	page.Title = "Different desk"
	changed, _ := PageContentVersion(page)
	if _, err := registry.ResolvePinned(asset.Ref, "1", changed); err == nil {
		t.Fatal("same source version silently selected different retained content")
	}
	if _, err := registry.ResolvePinned(asset.Ref, content); err == nil {
		t.Fatal("content digest was treated as the source version")
	}
	if _, err := registry.ResolvePinned(asset.Ref, "1", content, content); err == nil {
		t.Fatal("multiple content identities accepted")
	}
}

func TestAssetBootstrapReleasesOwnerScopesOnFailureAndCompletion(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "completion", true: "failure"}[fail], func(t *testing.T) {
			var calls []string
			a := newLifecycleFixture("a", AssetConnection)
			b := newLifecycleFixture("b", AssetConnection)
			for _, owner := range []*lifecycleFixture{a, b} {
				owner.bootstrap = func(assets []ReleaseAsset) (func(), error) {
					if len(assets) != 1 || assets[0].Ref.App != owner.id {
						t.Fatal("bootstrap scope received another owner's shapes")
					}
					calls = append(calls, "begin-"+owner.id)
					end := func() { calls = append(calls, "end-"+owner.id) }
					if fail && owner == b {
						return end, errors.New("owner preparation failed")
					}
					return end, nil
				}
			}
			registry, err := NewAssetRegistry(b, a)
			if err != nil {
				t.Fatal(err)
			}
			end, err := registry.BeginBootstrap([]ReleaseAsset{fixtureLifecycleAsset("b", AssetConnection, "connection"), fixtureLifecycleAsset("a", AssetConnection, "connection")})
			if fail {
				if err == nil || end != nil {
					t.Fatal("failed bootstrap retained an open owner scope")
				}
			} else {
				if err != nil || end == nil {
					t.Fatalf("bootstrap did not start: %v", err)
				}
				end()
				end() // a deferred cleanup may follow an explicit early cleanup
			}
			if !slices.Equal(calls, []string{"begin-a", "begin-b", "end-b", "end-a"}) {
				t.Fatalf("owner scope cleanup order: %v", calls)
			}
		})
	}
}
