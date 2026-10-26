package platformserver

import "platformserver/internal/construction"

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"platformkernel/kernel"
	"platformserver/apps/build"
	"platformserver/apps/flow"
	"platformserver/apps/work"
	"platformserver/journal"
	"platformserver/platform"
)

type releaseEnvironmentOwner struct {
	platform.AssetLifecycleOwner
	ledger    *platform.Ledger
	ref       platform.AssetRef
	requested platform.AssetEnvironmentRequirement
}

func (o *releaseEnvironmentOwner) Manifest() platform.Manifest {
	return platform.Manifest{ID: o.ref.App}
}
func (o *releaseEnvironmentOwner) AcceptedLedger() *platform.Ledger { return o.ledger }
func (o *releaseEnvironmentOwner) AssetKinds() []platform.AssetKind {
	return []platform.AssetKind{platform.AssetConnection}
}
func (o *releaseEnvironmentOwner) AssetEnvironmentRequirements(platform.ReleaseAsset) ([]platform.AssetEnvironmentRequirement, error) {
	return []platform.AssetEnvironmentRequirement{{Ref: o.ref, Profile: "owned-profile", Credential: true}}, nil
}
func (o *releaseEnvironmentOwner) ResolveAssetEnvironment(requirement platform.AssetEnvironmentRequirement) (platform.AssetEnvironmentTarget, error) {
	o.requested = requirement
	return platform.AssetEnvironmentTarget{TargetRecord: "owned-record", Credential: "local-credential"}, nil
}

func TestReleaseEnvironmentUsesOwnerDescriptorAndHostCredential(t *testing.T) {
	ref := platform.AssetRef{App: "connection-owner", Kind: platform.AssetConnection, Name: "external"}
	owner := &releaseEnvironmentOwner{ref: ref, ledger: platform.NewLedger("environment-target", ref.App, platform.NewCatalog())}
	registry, err := platform.NewAssetRegistry(runtimeAssetOwner{App: owner, lifecycle: owner})
	if err != nil {
		t.Fatal(err)
	}
	asset := platform.ReleaseAsset{Ref: ref, ContractVersion: 1, SourceVersion: "1", Body: json.RawMessage(`{"name":"external"}`)}
	var checked string
	target := composeTenant(t, "environment-target", nil)
	target.Secrets = func(name string) ([]byte, bool) { checked = name; return []byte("configured"), true }
	context, err := target.assetReleaseContextLocked(registry, []platform.ReleaseAsset{asset}, platform.Member{ID: "reviewer"}, time.Now(), false)
	if err != nil || len(context.Bindings) != 1 || context.Bindings[0].Ref != ref || context.Bindings[0].TargetRecord != "owned-record" || checked != "local-credential" {
		t.Fatal(context, checked, err)
	}
	if owner.requested.Profile != "owned-profile" || !owner.requested.Credential {
		t.Fatal("host changed owner requirements", owner.requested)
	}
	if bytes.Contains(platform.Raw(context.Bindings), []byte("credential")) {
		t.Fatal("transient credential name entered target bindings")
	}
	target.Secrets = func(string) ([]byte, bool) { return nil, false }
	if _, err := target.assetReleaseContextLocked(registry, []platform.ReleaseAsset{asset}, platform.Member{}, time.Now(), false); err == nil || !strings.Contains(err.Error(), "has no credential") {
		t.Fatal("host credential absence did not refuse release", err)
	}
}

// The two domain tasks share the real source/pipeline/writeback and release
// owners. Their complete definition closure is delivered into a clean target;
// source data and environment credentials are never that closure.
func TestIntegrationReleaseAcrossEnvironments(t *testing.T) {
	for _, domain := range []string{"hospitality", "manufacturing"} {
		t.Run(domain, func(t *testing.T) { checkIntegrationReleaseAcrossEnvironments(t, domain, nil) })
	}
}

func TestJournalIntegrationReleaseAcrossEnvironments(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	store, err := OpenJournal(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, domain := range []string{"hospitality", "manufacturing"} {
		t.Run(domain, func(t *testing.T) { checkIntegrationReleaseAcrossEnvironments(t, domain, store) })
	}
}

func TestIntegrationDraftEditsUseOnlyExplicitlySelectedModels(t *testing.T) {
	const id = "integration-authoring"
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	tn := builderTenant(t, id, work.New(id))
	member := memberOf(t, tn, "builder")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(server.Close)
	decide(t, tn, "builder", construction.ID, construction.ConnectionType+".create", construction.ConnectionType, "CONNECTION", map[string]any{"name": "authoringconnection", "title": "Authoring connection", "kind": "http", "address": server.URL + "/", "allowPrivate": true}, at)
	decide(t, tn, "builder", construction.ID, construction.ConnectionType+".check", construction.ConnectionType, "CONNECTION", map[string]any{}, at)
	tn.CheckConnections(at)
	connection, _ := platform.Get[construction.Connection](tn.automation(construction.ID, false), "CONNECTION")
	if connection.State != "ready" {
		t.Fatalf("connection did not become ready: %+v", connection.Last)
	}
	decide(t, tn, "builder", construction.ID, construction.ConnectionType+".create", construction.ConnectionType, "UNCHECKED", map[string]any{"name": "uncheckedauthoring", "title": "Unchecked authoring connection", "kind": "http", "address": server.URL + "/", "allowPrivate": true}, at)
	decide(t, tn, "builder", construction.ID, construction.DatasetType+".create", construction.DatasetType, "DATASET", map[string]any{"name": "authoringrows", "title": "Authoring rows"}, at)
	object := publishObject(t, tn, "builder", "OBJECT", map[string]any{"name": "integrationauthoring", "title": "Integration authoring", "fields": []construction.Field{{Name: "note", Title: "Note", Type: "text"}}}, at)
	const nextObject = "build.integrationnext"
	integrations := []struct {
		typ, id string
		kind    platform.AssetKind
		payload map[string]any
		edit    map[string]any
	}{
		{construction.SourceType, "SOURCE", platform.AssetSource,
			map[string]any{"name": "authoringfeed", "title": "Authoring feed", "connection": "CONNECTION", "url": "rows", "object": object, "key": "id", "mapping": []construction.SourceField{{From: "note", To: "note"}}},
			map[string]any{"mapping": []construction.SourceField{{From: "note", To: "extra"}}}},
		{construction.PipelineType, "PIPELINE", platform.AssetPipeline,
			map[string]any{"name": "authoringpipeline", "title": "Authoring pipeline", "input": "DATASET", "outputObject": object, "key": "id"},
			map[string]any{"outputObject": nextObject}},
		{construction.WritebackType, "WRITEBACK", platform.AssetWriteback,
			map[string]any{"name": "authoringdelivery", "title": "Authoring delivery", "connection": "CONNECTION", "object": object, "on": "edit", "path": "receipts", "result": []construction.SourceField{{From: "document", To: "note"}}},
			map[string]any{"result": []construction.SourceField{{From: "document", To: "extra"}}}},
		{build.MatchType, "MATCH", platform.AssetMatching,
			map[string]any{"name": "authoringidentity", "title": "Authoring identity", "object": object, "keys": []construction.MatchKey{{Fields: []string{"note"}}}},
			map[string]any{"keys": []construction.MatchKey{{Fields: []string{"extra"}}}}},
	}
	for _, integration := range integrations {
		decide(t, tn, "builder", construction.ID, integration.typ+".create", integration.typ, integration.id, integration.payload, at)
		decide(t, tn, "builder", construction.ID, integration.typ+".publish", integration.typ, integration.id, map[string]any{}, at)
	}
	if why := refuse(t, tn, "builder", construction.ID, construction.SourceType+".edit", construction.SourceType, "SOURCE", map[string]any{"connection": "UNCHECKED"}, at); !strings.Contains(why, "not ready") {
		t.Fatalf("published source bypassed the declared connection check: %s", why)
	}
	before := platform.Raw(tn.Definitions(member))
	decide(t, tn, "builder", construction.ID, construction.ObjectType+".edit", construction.ObjectType, "OBJECT", map[string]any{"fields": []construction.Field{{Name: "note", Title: "Note", Type: "text"}, {Name: "extra", Title: "Extra", Type: "text"}}}, at)
	decide(t, tn, "builder", construction.ID, construction.ObjectType+".create", construction.ObjectType, "NEXT", map[string]any{"name": "integrationnext", "title": "Next integration target", "fields": []construction.Field{{Name: "extra", Title: "Extra", Type: "text"}}}, at)
	for _, integration := range integrations {
		decide(t, tn, "builder", construction.ID, integration.typ+".edit", integration.typ, integration.id, integration.edit, at)
		if why := refuse(t, tn, "builder", construction.ID, integration.typ+".publish", integration.typ, integration.id, map[string]any{}, at); why == "ok" {
			t.Fatalf("direct %s publication used an unselected saved model", integration.typ)
		}
		if preview, err := tn.PreviewReleaseDrafts(member, []construction.JointDraftRef{{Kind: integration.kind, ID: integration.id}}); err == nil && preview.Diagnostic == "" {
			t.Fatalf("%s-only candidate used an unselected saved model", integration.typ)
		}
	}
	// A saved change of matching target cannot make its still-running rule
	// disappear and allow a second published rule on the original object.
	decide(t, tn, "builder", construction.ID, build.MatchType+".edit", build.MatchType, "MATCH", map[string]any{"object": nextObject}, at)
	decide(t, tn, "builder", construction.ID, build.MatchType+".create", build.MatchType, "DUPLICATE", map[string]any{"name": "duplicateidentity", "title": "Duplicate identity", "object": object, "keys": []construction.MatchKey{{Fields: []string{"note"}}}}, at)
	if why := refuse(t, tn, "builder", construction.ID, build.MatchType+".publish", build.MatchType, "DUPLICATE", map[string]any{}, at); !strings.Contains(why, "already has") {
		t.Fatalf("matching draft hid the still-published rule: %s", why)
	}
	decide(t, tn, "builder", construction.ID, build.MatchType+".edit", build.MatchType, "MATCH", map[string]any{"object": object}, at)
	owner := tn.app(construction.ID).(*build.Build)
	checkInstalled := func() {
		t.Helper()
		caller := tn.automation(construction.ID, false)
		source, _ := platform.Get[construction.Source](caller, "SOURCE")
		pipeline, _ := platform.Get[construction.Pipeline](caller, "PIPELINE")
		writeback, _ := platform.Get[construction.Writeback](caller, "WRITEBACK")
		matching, _ := platform.Get[construction.Match](caller, "MATCH")
		fixedSource, sourceErr := owner.ExecutionSource(source)
		fixedPipeline, pipelineErr := owner.ExecutionPipeline(pipeline)
		fixedWriteback, writebackErr := owner.ExecutionWriteback(writeback)
		fixedMatching, matchErr := owner.ExecutionMatching(matching)
		if sourceErr != nil || pipelineErr != nil || writebackErr != nil || matchErr != nil || fixedSource.Mapping[0].To != "note" || fixedPipeline.OutputObject != object || fixedWriteback.Result[0].To != "note" || fixedMatching.Keys[0].Fields[0] != "note" {
			t.Fatalf("saving integration drafts changed execution: %v %v %v %v", sourceErr, pipelineErr, writebackErr, matchErr)
		}
		if !bytes.Equal(before, platform.Raw(tn.Definitions(member))) {
			t.Fatal("integration authoring changed installed definitions before activation")
		}
	}
	checkInstalled()
	selection := []construction.JointDraftRef{{Kind: platform.AssetObject, ID: "OBJECT"}, {Kind: platform.AssetObject, ID: "NEXT"}, {Kind: platform.AssetConnection, ID: "CONNECTION"}, {Kind: platform.AssetDataset, ID: "DATASET"}}
	for _, integration := range integrations {
		selection = append(selection, construction.JointDraftRef{Kind: integration.kind, ID: integration.id})
	}
	decide(t, tn, "builder", construction.ID, construction.SourceType+".edit", construction.SourceType, "SOURCE", map[string]any{"mapping": []construction.SourceField{{From: "note", To: "missing"}}}, at)
	if preview, err := tn.PreviewReleaseDrafts(member, selection); err == nil && preview.Diagnostic == "" {
		t.Fatal("joint integration candidate skipped the selected model's field checks")
	}
	decide(t, tn, "builder", construction.ID, construction.SourceType+".edit", construction.SourceType, "SOURCE", integrations[0].edit, at)
	preview, err := tn.PreviewReleaseDrafts(member, selection)
	if err != nil || preview.Diagnostic != "" || preview.CandidateID == "" {
		t.Fatalf("explicit integration/model selection could not form a candidate: %+v %v", preview, err)
	}
	candidate, err := tn.SaveReleaseCandidates(member, selection, preview.CandidateID, "save-integration-authoring", at)
	if err != nil {
		t.Fatal(err)
	}
	checkInstalled()
	if _, err := tn.ActivateRelease(member, candidate, "activate-integration-authoring", at); err != nil {
		t.Fatal(err)
	}
	caller := tn.automation(construction.ID, false)
	source, _ := platform.Get[construction.Source](caller, "SOURCE")
	pipeline, _ := platform.Get[construction.Pipeline](caller, "PIPELINE")
	writeback, _ := platform.Get[construction.Writeback](caller, "WRITEBACK")
	matching, _ := platform.Get[construction.Match](caller, "MATCH")
	fixedSource, sourceErr := owner.ExecutionSource(source)
	fixedPipeline, pipelineErr := owner.ExecutionPipeline(pipeline)
	fixedWriteback, writebackErr := owner.ExecutionWriteback(writeback)
	fixedMatching, matchErr := owner.ExecutionMatching(matching)
	if sourceErr != nil || pipelineErr != nil || writebackErr != nil || matchErr != nil || fixedSource.Mapping[0].To != "extra" || fixedPipeline.OutputObject != nextObject || fixedWriteback.Result[0].To != "extra" || fixedMatching.Keys[0].Fields[0] != "extra" {
		t.Fatalf("activation did not install the exact selected integration drafts: %v %v %v %v", sourceErr, pipelineErr, writebackErr, matchErr)
	}
}

func releaseFabricBuilder(id string) string {
	if strings.HasSuffix(id, "-dev") {
		return "dev-builder"
	}
	return "target-builder"
}

type releaseFabricEnvironment struct {
	tenant  *Tenant
	entries []Entry
	fail    bool
	crash   bool
}

func checkIntegrationReleaseAcrossEnvironments(t *testing.T, domain string, store *journal.Postgres) {
	t.Helper()
	at := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	prefix := fmt.Sprintf("integration-release-%s-%d", domain, time.Now().UnixNano())
	var sourceReads, targetReads, targetWrites atomic.Int32
	var targetCredential atomic.Value
	var blockNextTarget atomic.Bool
	targetStarted, resumeTarget := make(chan struct{}), make(chan struct{})
	var resumeOnce sync.Once
	releaseTarget := func() { resumeOnce.Do(func() { close(resumeTarget) }) }
	t.Cleanup(releaseTarget)
	server := func(target bool) *httptest.Server {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == "/rows" {
				item := "SOURCE-ONLY"
				sequence := 5
				if target {
					item = "TARGET-ONLY"
					sequence = 9
					targetReads.Add(1)
					targetCredential.Store(r.Header.Get("Authorization"))
					if blockNextTarget.CompareAndSwap(true, false) {
						close(targetStarted)
						select {
						case <-resumeTarget:
						case <-r.Context().Done():
							return
						}
					}
				} else {
					sourceReads.Add(1)
				}
				_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "IN-1", "sku": item, "qty": "3", "seq": sequence}})
				return
			}
			if r.Method == http.MethodPost {
				if target {
					targetWrites.Add(1)
				}
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"document":"DELIVERED"}`))
				return
			}
			_, _ = w.Write([]byte(`{}`))
		}))
		t.Cleanup(s.Close)
		return s
	}
	devServer, prodServer := server(false), server(true)
	compose := func(id string) *Tenant {
		tenant := composeTenant(t, id, []Seat{seatOf(releaseFabricBuilder(id), construction.ID+":"+construction.Builder), seatOf("user", construction.ID+":"+construction.User), seatOf("publisher", construction.ID+":"+construction.Publisher)}, work.New(id), flow.New(id), build.New(id))
		tenant.Secrets = func(name string) ([]byte, bool) {
			return []byte("Bearer " + name), strings.HasPrefix(name, "local-")
		}
		return tenant
	}
	open := func(id string) *releaseFabricEnvironment {
		e := &releaseFabricEnvironment{tenant: compose(id)}
		if store != nil {
			if _, err := store.Entries(context.Background(), id, 0); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				store.Pool().Exec(context.Background(), `delete from snapshots where tenant=$1`, id)
				store.Pool().Exec(context.Background(), `delete from journal where tenant=$1`, id)
			})
		}
		e.tenant.AcceptResult = func(entry Entry, key, hash string) ([]byte, error) {
			if e.fail {
				return nil, errors.New("injected release append failure")
			}
			raw := entry.Body
			if store != nil {
				var err error
				raw, err = store.AppendAccepted(context.Background(), id, entry, key, hash)
				if err != nil {
					return nil, err
				}
				entry.Body = raw
			}
			e.entries = append(e.entries, entry)
			if e.crash {
				panic("crash after release append")
			}
			return raw, nil
		}
		return e
	}
	durableEntries := func(e *releaseFabricEnvironment, after int) []Entry {
		if store == nil {
			return e.entries[after:]
		}
		reopened, err := OpenJournal(context.Background(), os.Getenv("PLATFORM_TEST_DATABASE"))
		if err != nil {
			t.Fatal(err)
		}
		defer reopened.Close()
		entries, err := reopened.Entries(context.Background(), e.tenant.ID, int64(after))
		if err != nil || len(entries) != len(e.entries)-after {
			t.Fatalf("durable recovery log: %d entries: %v", len(entries), err)
		}
		return entries
	}
	dev, prod := open(prefix+"-dev"), open(prefix+"-prod")
	connection := func(e *releaseFabricEnvironment, id, address string) {
		decide(t, e.tenant, releaseFabricBuilder(e.tenant.ID), construction.ID, construction.ConnectionType+".create", construction.ConnectionType, id, map[string]any{"name": "external", "title": "Environment connection", "kind": "http", "address": address + "/", "secret": "local-" + e.tenant.ID, "allowPrivate": true}, at)
		decide(t, e.tenant, releaseFabricBuilder(e.tenant.ID), construction.ID, construction.ConnectionType+".check", construction.ConnectionType, id, map[string]any{}, at)
		e.tenant.CheckConnections(at)
		c, _ := platform.Get[construction.Connection](e.tenant.automation(construction.ID, false), id)
		if c.State != "ready" {
			t.Fatalf("connection did not become a usable local binding: %+v", c.Last)
		}
	}
	connection(dev, "dev-connection", devServer.URL)
	typ := publishObject(t, dev.tenant, "dev-builder", "O", map[string]any{"name": "receipt", "title": domain + " receipt", "fields": []map[string]any{{"name": "item", "title": "Item", "type": "text"}, {"name": "qty", "title": "Quantity", "type": "integer"}, {"name": "document", "title": "Delivery document", "type": "text"}}, "states": []map[string]any{{"name": "open", "title": "Open"}, {"name": "received", "title": "Received"}}, "actions": []map[string]any{{"name": "receive", "title": "Receive", "from": []string{"open"}, "to": "received", "roles": []string{construction.User, construction.Builder}}}}, at)
	decide(t, dev.tenant, "dev-builder", construction.ID, build.MatchType+".create", build.MatchType, "dev-matching", map[string]any{"name": "receiptidentity", "title": "Match received items", "object": typ, "keys": []construction.MatchKey{{Fields: []string{"item"}, Normalize: "folded"}}}, at)
	decide(t, dev.tenant, "dev-builder", construction.ID, build.MatchType+".publish", build.MatchType, "dev-matching", map[string]any{}, at)
	for _, ds := range []struct{ id, name string }{{"dev-raw", "raw"}, {"dev-clean", "clean"}} {
		decide(t, dev.tenant, "dev-builder", construction.ID, construction.DatasetType+".create", construction.DatasetType, ds.id, map[string]any{"name": ds.name, "title": ds.name + " rows"}, at)
	}
	decide(t, dev.tenant, "dev-builder", construction.ID, construction.SourceType+".create", construction.SourceType, "dev-source", map[string]any{"name": "feed", "title": "External rows", "connection": "dev-connection", "profile": "json", "url": "rows", "since": "seq", "dataset": "dev-raw"}, at)
	decide(t, dev.tenant, "dev-builder", construction.ID, construction.SourceType+".publish", construction.SourceType, "dev-source", map[string]any{}, at)
	dev.tenant.PullSources(at)
	for _, p := range []struct {
		id, name, input, output string
		object                  bool
	}{{"dev-clean-pipeline", "cleaning", "dev-raw", "dev-clean", false}, {"dev-land-pipeline", "landing", "dev-clean", typ, true}} {
		definition := map[string]any{"name": p.name, "title": p.name, "input": p.input, "steps": []construction.Step{{Kind: "select", Columns: []string{"id", "sku", "qty"}}, {Kind: "cast", Column: "qty", Type: "number"}, {Kind: "rename", From: "sku", To: "item"}}}
		if p.object {
			definition["outputObject"], definition["key"] = p.output, "id"
			definition["steps"] = []construction.Step{}
		} else {
			definition["outputDataset"] = p.output
		}
		decide(t, dev.tenant, "dev-builder", construction.ID, construction.PipelineType+".create", construction.PipelineType, p.id, definition, at)
		decide(t, dev.tenant, "dev-builder", construction.ID, construction.PipelineType+".publish", construction.PipelineType, p.id, map[string]any{}, at)
	}
	dev.tenant.RunPipelines(at)
	dev.tenant.RunPipelines(at.Add(time.Second))
	decide(t, dev.tenant, "dev-builder", construction.ID, construction.WritebackType+".create", construction.WritebackType, "dev-writeback", map[string]any{"name": "receiptdelivery", "title": "Receipt delivery", "connection": "dev-connection", "object": typ, "on": "receive", "path": "receipts", "result": []construction.SourceField{{From: "document", To: "document"}}}, at)
	decide(t, dev.tenant, "dev-builder", construction.ID, construction.WritebackType+".publish", construction.WritebackType, "dev-writeback", map[string]any{}, at)
	decide(t, dev.tenant, "dev-builder", construction.ID, construction.PageType+".create", construction.PageType, "P", map[string]any{"name": "desk", "title": "Receiving desk", "object": typ, "list": []string{"item", "qty", "state", "document"}, "detail": []string{"item", "qty", "state", "document"}}, at)
	decide(t, dev.tenant, "dev-builder", construction.ID, construction.ProcessType+".create", construction.ProcessType, "F", map[string]any{"name": "receipthold", "title": "Keep receipt work in flight", "object": typ, "when": "received", "steps": []construction.ProcessStep{{Name: "hold", Kind: "wait", UntilSeconds: 3600, Next: "done"}, {Name: "done", Kind: "end"}}}, at)
	resources := []platform.AssetRef{{App: construction.ID, Kind: platform.AssetFlow, Name: "build.receipthold"}, {App: construction.ID, Kind: platform.AssetSource, Name: "feed"}, {App: construction.ID, Kind: platform.AssetPipeline, Name: "cleaning"}, {App: construction.ID, Kind: platform.AssetPipeline, Name: "landing"}, {App: construction.ID, Kind: platform.AssetWriteback, Name: "receiptdelivery"}, {App: construction.ID, Kind: platform.AssetMatching, Name: "receiptidentity"}}
	decide(t, dev.tenant, "dev-builder", construction.ID, construction.AppType+".create", construction.AppType, "A", map[string]any{"name": "receiving", "title": domain + " receiving", "pages": []string{"desk"}, "resources": resources}, at)
	drafts := []construction.JointDraftRef{{Kind: platform.AssetObject, ID: "O"}, {Kind: platform.AssetPage, ID: "P"}, {Kind: platform.AssetApp, ID: "A"}, {Kind: platform.AssetFlow, ID: "F"}}
	freeze := func(key string) platform.ReleaseCandidate {
		builder := memberOf(t, dev.tenant, "dev-builder")
		preview, err := dev.tenant.PreviewReleaseDrafts(builder, drafts)
		if err != nil || preview.Diagnostic != "" {
			t.Fatalf("complete integration closure preview: %+v %v", preview, err)
		}
		if _, err := dev.tenant.SaveReleaseCandidates(builder, drafts, preview.CandidateID, key, at); err != nil {
			t.Fatal(err)
		}
		if _, err := dev.tenant.SealCandidate(preview.CandidateID, at); err != nil {
			t.Fatal(err)
		}
		candidate, err := platform.ReadCandidate(preview.CandidateID, dev.tenant.releases.raw(preview.CandidateID))
		if err != nil {
			t.Fatal(err)
		}
		return candidate
	}
	candidate := freeze("save-v1")
	seen := map[string]bool{}
	for _, asset := range candidate.Assets {
		seen[string(asset.Ref.Kind)+":"+asset.Ref.Name] = true
	}
	for _, required := range []string{"connection:external", "dataset:raw", "dataset:clean", "source:feed", "pipeline:cleaning", "pipeline:landing", "writeback:receiptdelivery", "matching:receiptidentity"} {
		if !seen[required] {
			t.Fatalf("application release omitted required integration %s", required)
		}
	}
	if bytes.Contains(candidate.Bytes, []byte(devServer.URL)) || bytes.Contains(candidate.Bytes, []byte("local-"+dev.tenant.ID)) || bytes.Contains(candidate.Bytes, []byte("dev-builder")) || bytes.Contains(candidate.Bytes, []byte("dev-connection")) || bytes.Contains(candidate.Bytes, []byte("SOURCE-ONLY")) {
		t.Fatal("portable candidate contains source environment bindings or runtime data")
	}
	devData, _ := platform.Get[construction.Dataset](dev.tenant.automation(construction.ID, false), "dev-raw")
	devSource, _ := platform.Get[construction.Source](dev.tenant.automation(construction.ID, false), "dev-source")
	if devSource.Cursor != "5" {
		t.Fatal("source fixture lacks a non-empty runtime cursor")
	}
	if devData.Version != 1 {
		t.Fatal("source fixture did not hold real source data")
	}
	before := snapshot(prod.tenant)
	if _, err := PromoteCandidate(dev.tenant, prod.tenant, candidate.ID, "missing-binding", true, memberOf(t, prod.tenant, "target-builder"), at); err == nil || !strings.Contains(strings.ToLower(err.Error()), "binding") {
		t.Fatalf("target without a local binding was not refused: %v", err)
	}
	if snapshot(prod.tenant) != before || (targetReads.Load() != 0 || targetWrites.Load() != 0) {
		t.Fatal("missing binding left target drafts, data, release pointer or outside reads")
	}
	wrong := open(prefix + "-wrong")
	decide(t, wrong.tenant, "target-builder", construction.ID, construction.ConnectionType+".create", construction.ConnectionType, "wrong-connection", map[string]any{"name": "external", "title": "Wrong binding kind", "kind": "odata", "address": prodServer.URL + "/", "secret": "local-" + wrong.tenant.ID, "allowPrivate": true}, at)
	decide(t, wrong.tenant, "target-builder", construction.ID, construction.ConnectionType+".check", construction.ConnectionType, "wrong-connection", map[string]any{}, at)
	wrong.tenant.CheckConnections(at)
	wrongBefore := snapshot(wrong.tenant)
	if _, err := PromoteCandidate(dev.tenant, wrong.tenant, candidate.ID, "wrong-binding", true, memberOf(t, wrong.tenant, "target-builder"), at); err == nil || err.Error() == "" {
		t.Fatal("target with an incompatible binding was not explicitly refused")
	}
	if snapshot(wrong.tenant) != wrongBefore || (targetReads.Load() != 0 || targetWrites.Load() != 0) {
		t.Fatal("invalid binding created target drafts, release pointer or outside reads")
	}
	ambiguous := open(prefix + "-ambiguous")
	connection(ambiguous, "first-binding", prodServer.URL)
	connection(ambiguous, "second-binding", prodServer.URL)
	ambiguousBefore := snapshot(ambiguous.tenant)
	if _, err := PromoteCandidate(dev.tenant, ambiguous.tenant, candidate.ID, "ambiguous-binding", true, memberOf(t, ambiguous.tenant, "target-builder"), at); err == nil || err.Error() == "" {
		t.Fatal("ambiguous environment binding picked an arbitrary connection")
	}
	if snapshot(ambiguous.tenant) != ambiguousBefore || (targetReads.Load() != 0 || targetWrites.Load() != 0) {
		t.Fatal("ambiguous binding exposed part of target installation")
	}
	unauthorized := open(prefix + "-unauthorized")
	connection(unauthorized, "local-connection", prodServer.URL)
	unauthorizedBefore := snapshot(unauthorized.tenant)
	if _, err := PromoteCandidate(dev.tenant, unauthorized.tenant, candidate.ID, "missing-execution-right", true, memberOf(t, unauthorized.tenant, "publisher"), at); err == nil {
		t.Fatal("release publisher acquired original integration execution permissions")
	}
	if snapshot(unauthorized.tenant) != unauthorizedBefore || unauthorized.tenant.ActiveRelease() != "" || (targetReads.Load() != 0 || targetWrites.Load() != 0) {
		t.Fatal("execution permission refusal exposed target installation")
	}
	scoped := open(prefix + "-scoped")
	connection(scoped, "local-connection", prodServer.URL)
	scopedMember := memberOf(t, scoped.tenant, "target-builder")
	scopedMember.Scopes = []string{"build.object.*", "build.page.*", "build.app.*", "build.process.*", "build.connection.*", "build.source.*", "build.dataset.*", "build.pipeline.*", "build.writeback.*", "build.match.*"}
	scopedBefore := snapshot(scoped.tenant)
	if _, err := PromoteCandidate(dev.tenant, scoped.tenant, candidate.ID, "missing-data-write-scope", true, scopedMember, at); err == nil {
		t.Fatal("metadata-scoped builder acquired business data write permissions")
	}
	if snapshot(scoped.tenant) != scopedBefore || scoped.tenant.ActiveRelease() != "" || (targetReads.Load() != 0 || targetWrites.Load() != 0) {
		t.Fatal("data write scope refusal exposed target installation")
	}
	// A complete binding does not make a later owner/storage failure atomic by
	// itself. Missing app/page/integration drafts must remain private throughout
	// the complete preparation, including validation against existing data.
	incompatible := open(prefix + "-incompatible")
	connection(incompatible, "local-connection", prodServer.URL)
	legacyType := publishObject(t, incompatible.tenant, "target-builder", "legacy-object", map[string]any{"name": "receipt", "title": "Existing receipt", "fields": []map[string]any{{"name": "legacy", "title": "Legacy value", "type": "text"}}}, at)
	decide(t, incompatible.tenant, "user", construction.ID, legacyType+".create", legacyType, "existing", map[string]any{"legacy": "PRESERVE"}, at)
	incompatibleBefore := snapshot(incompatible.tenant)
	if _, err := PromoteCandidate(dev.tenant, incompatible.tenant, candidate.ID, "incompatible-storage", true, memberOf(t, incompatible.tenant, "target-builder"), at); err == nil || err.Error() == "" {
		t.Fatal("unsupported existing storage change was not refused")
	}
	if snapshot(incompatible.tenant) != incompatibleBefore || incompatible.tenant.ActiveRelease() != "" || (targetReads.Load() != 0 || targetWrites.Load() != 0) {
		t.Fatal("later owner validation failure exposed missing drafts or changed existing data")
	}
	connection(prod, "prod-connection", prodServer.URL)
	before = snapshot(prod.tenant)
	prod.fail = true
	if _, err := PromoteCandidate(dev.tenant, prod.tenant, candidate.ID, "deliver-v1", true, memberOf(t, prod.tenant, "target-builder"), at); err == nil {
		t.Fatal("append failure reported delivery success")
	}
	if snapshot(prod.tenant) != before || prod.tenant.ActiveRelease() != "" || (targetReads.Load() != 0 || targetWrites.Load() != 0) {
		t.Fatal("failed release acceptance exposed target drafts or requested a source pull")
	}
	prod.fail = false
	cut := len(prod.entries)
	image, _, err := prod.tenant.Snapshot(func() int64 { return int64(cut) })
	if err != nil {
		t.Fatal(err)
	}
	prod.crash = true
	var appendErr error
	func() {
		defer func() {
			if recover() == nil {
				var rejection *kernel.Error
				if errors.As(appendErr, &rejection) {
					t.Fatalf("release did not reach append-before-apply boundary: %v: %s", appendErr, rejection.Message)
				}
				t.Fatalf("release did not reach append-before-apply boundary: %v", appendErr)
			}
		}()
		_, appendErr = PromoteCandidate(dev.tenant, prod.tenant, candidate.ID, "deliver-v1", true, memberOf(t, prod.tenant, "target-builder"), at)
	}()
	prod.crash = false
	if len(prod.entries) != cut+1 || snapshot(prod.tenant) != before {
		t.Fatal("promotion materialized drafts before the single saved result applied")
	}
	restored := compose(prod.tenant.ID)
	if err := restored.Restore(image); err != nil {
		t.Fatal(err)
	}
	if err := restored.Replay(durableEntries(prod, cut)); err != nil {
		t.Fatal(err)
	}
	persist := prod.tenant.AcceptResult
	prod.tenant = restored
	prod.tenant.AcceptResult = persist
	if prod.tenant.ActiveRelease() != candidate.ID || (targetReads.Load() != 0 || targetWrites.Load() != 0) {
		t.Fatal("recovery failed to install the frozen closure without outside calls")
	}
	acceptedCount, installedImage := len(prod.entries), snapshot(prod.tenant)
	retry, err := PromoteCandidate(dev.tenant, prod.tenant, candidate.ID, "deliver-v1", true, memberOf(t, prod.tenant, "target-builder"), at.Add(time.Hour))
	if err != nil || retry.Candidate != candidate.ID || !retry.Active || len(prod.entries) != acceptedCount || snapshot(prod.tenant) != installedImage {
		t.Fatalf("accepted promotion retry bootstrapped or accepted again: %+v %v", retry, err)
	}
	// Replay is the definition handoff only. Target data comes exclusively from
	// its own bound connection, whose row IDs need not equal the source IDs.
	c := prod.tenant.automation(construction.ID, false)
	datasets, _, _ := platform.Find[construction.Dataset](c, platform.Query{})
	sources, _, _ := platform.Find[construction.Source](c, platform.Query{})
	if len(datasets) != 2 || len(sources) != 1 || sources[0].Connection != "prod-connection" || sources[0].Cursor != "" || sources[0].Last != nil || sources[0].Puller != "target-builder" {
		t.Fatalf("target did not resolve local references or copied source runtime: datasets=%+v sources=%+v", datasets, sources)
	}
	for _, dataset := range datasets {
		if dataset.Version != 0 || dataset.Last != nil {
			t.Fatal("promotion copied dataset contents/version")
		}
	}
	connectionRow, _ := platform.Get[construction.Connection](c, "prod-connection")
	if connectionRow.Address != prodServer.URL+"/" || connectionRow.Secret != "local-"+prod.tenant.ID {
		t.Fatal("promotion replaced the target environment binding")
	}
	decide(t, prod.tenant, "target-builder", construction.ID, construction.SourceType+".pull", construction.SourceType, sources[0].ID, map[string]any{}, at)
	prod.tenant.PullSources(at)
	prod.tenant.RunPipelines(at)
	prod.tenant.RunPipelines(at.Add(time.Second))
	view, refusal := prod.tenant.RecordOf(memberOf(t, prod.tenant, "user"), typ, "IN-1", at)
	if refusal != nil {
		t.Fatal(refusal)
	}
	body, _ := json.Marshal(view.Record)
	if !bytes.Contains(body, []byte("TARGET-ONLY")) || bytes.Contains(body, []byte("SOURCE-ONLY")) {
		t.Fatalf("target business record came from wrong environment: %s", body)
	}
	if targetCredential.Load() != "Bearer local-"+prod.tenant.ID {
		t.Fatal("target source did not resolve its own environment credential")
	}
	decide(t, prod.tenant, "user", construction.ID, typ+".receive", typ, "IN-1", map[string]any{}, at)
	prod.tenant.Work(at)
	view, refusal = prod.tenant.RecordOf(memberOf(t, prod.tenant, "user"), typ, "IN-1", at)
	if refusal != nil {
		t.Fatal(refusal)
	}
	body, _ = json.Marshal(view.Record)
	if bytes.Contains(body, []byte("DELIVERED")) || targetWrites.Load() != 0 {
		t.Fatal("writeback did not remain pending before definition upgrade")
	}
	pending, known := platform.Get[flow.FlowInstance](prod.tenant.automation(flow.ID, false), "build.receipthold:IN-1")
	if !known || pending.State != "waiting" || pending.Release != candidate.ID || pending.Version != 1 {
		t.Fatalf("released in-flight instance has wrong binding: %+v", pending)
	}
	// Complete definition upgrade keeps existing target content and old dataset
	// versions. It changes no storage shape or environment binding.
	beforeRow := slices.Clone(body)
	pullAt := at.Add(2 * time.Second)
	decide(t, prod.tenant, "target-builder", construction.ID, construction.SourceType+".pull", construction.SourceType, sources[0].ID, map[string]any{}, pullAt)
	blockNextTarget.Store(true)
	pullDone := make(chan struct{})
	go func() {
		defer close(pullDone)
		prod.tenant.PullSources(pullAt)
	}()
	defer releaseTarget()
	select {
	case <-targetStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("source call did not reach its captured definition boundary")
	}
	beforeSource, _ := platform.Get[construction.Source](prod.tenant.automation(construction.ID, false), sources[0].ID)
	beforeDataset := map[string]int{}
	for _, dataset := range datasets {
		current, _ := platform.Get[construction.Dataset](prod.tenant.automation(construction.ID, false), dataset.ID)
		beforeDataset[dataset.ID] = current.Version
	}
	decide(t, dev.tenant, "dev-builder", construction.ID, construction.PipelineType+".edit", construction.PipelineType, "dev-clean-pipeline", map[string]any{"title": "Clean rows v2", "expectations": []construction.Expectation{{Column: "qty", Rule: "range", Value: "0..10"}}}, at)
	decide(t, dev.tenant, "dev-builder", construction.ID, construction.PipelineType+".publish", construction.PipelineType, "dev-clean-pipeline", map[string]any{}, at)
	decide(t, dev.tenant, "dev-builder", construction.ID, construction.WritebackType+".edit", construction.WritebackType, "dev-writeback", map[string]any{"result": []construction.SourceField{{From: "document", To: "item"}}}, at)
	decide(t, dev.tenant, "dev-builder", construction.ID, construction.WritebackType+".publish", construction.WritebackType, "dev-writeback", map[string]any{}, at)
	decide(t, dev.tenant, "dev-builder", construction.ID, construction.SourceType+".edit", construction.SourceType, "dev-source", map[string]any{"title": "External rows v2"}, at)
	decide(t, dev.tenant, "dev-builder", construction.ID, construction.SourceType+".publish", construction.SourceType, "dev-source", map[string]any{}, at)
	v2 := freeze("save-v2")
	if v2.ID == candidate.ID {
		t.Fatal("changed integration definition did not change the candidate")
	}
	if _, err := PromoteCandidate(dev.tenant, prod.tenant, v2.ID, "deliver-v2", true, memberOf(t, prod.tenant, "target-builder"), at); err != nil {
		t.Fatalf("supported definition upgrade: %v", err)
	}
	view, _ = prod.tenant.RecordOf(memberOf(t, prod.tenant, "user"), typ, "IN-1", at)
	afterRow, _ := json.Marshal(view.Record)
	afterSource, _ := platform.Get[construction.Source](prod.tenant.automation(construction.ID, false), sources[0].ID)
	if beforeSource.Cursor != "9" || beforeSource.Cursor != afterSource.Cursor || beforeSource.Last == nil || afterSource.Last == nil || !beforeSource.Last.At.Equal(afterSource.Last.At) {
		t.Fatal("definition upgrade reset source runtime/cursor")
	}
	for datasetID, version := range beforeDataset {
		current, _ := platform.Get[construction.Dataset](prod.tenant.automation(construction.ID, false), datasetID)
		if current.Version != version {
			t.Fatal("definition upgrade rewrote dataset runtime versions")
		}
	}
	afterPending, _ := platform.Get[flow.FlowInstance](prod.tenant.automation(flow.ID, false), pending.ID)
	if afterPending.Release != pending.Release || afterPending.Version != pending.Version || afterPending.State != pending.State {
		t.Fatal("definition upgrade moved an in-flight instance onto new release bytes")
	}
	if !bytes.Equal(beforeRow, afterRow) || prod.tenant.ActiveRelease() != v2.ID {
		t.Fatal("definition upgrade changed existing business bytes")
	}
	releaseTarget()
	select {
	case <-pullDone:
	case <-time.After(5 * time.Second):
		t.Fatal("source call did not finish after its title-only upgrade")
	}
	finishedSource, _ := platform.Get[construction.Source](prod.tenant.automation(construction.ID, false), sources[0].ID)
	if finishedSource.Last == nil || !finishedSource.Last.At.Equal(pullAt) || finishedSource.Last.DefinitionVersion != beforeSource.Last.DefinitionVersion || finishedSource.Requested {
		t.Fatalf("compatible title upgrade did not complete the captured source version: %+v", finishedSource)
	}
	// The pending v1 call keeps its accepted answer mapping even when v2 maps
	// the same reply into another field. It finishes against its saved contract.
	prod.tenant.Dispatch(at)
	view, refusal = prod.tenant.RecordOf(memberOf(t, prod.tenant, "user"), typ, "IN-1", at)
	if refusal != nil {
		t.Fatal(refusal)
	}
	completed, _ := json.Marshal(view.Record)
	if !bytes.Contains(completed, []byte("DELIVERED")) || !bytes.Contains(completed, []byte("TARGET-ONLY")) || targetWrites.Load() != 1 {
		t.Fatalf("pending writeback lost its original result mapping during upgrade: %s", completed)
	}
	pipelines, _, _ := platform.Find[construction.Pipeline](prod.tenant.automation(construction.ID, false), platform.Query{})
	checkedPipeline := false
	for _, pipeline := range pipelines {
		if pipeline.Name != "cleaning" {
			continue
		}
		checkedPipeline = true
		owner := prod.tenant.app(construction.ID).(*build.Build)
		accepted, err := owner.ExecutionPipeline(pipeline)
		if err != nil || accepted.Title != "Clean rows v2" || len(accepted.Expectations) != 1 || accepted.Expectations[0].Value != "0..10" {
			t.Fatalf("target execution did not select the upgraded integration definition: %+v %v", accepted, err)
		}
		decide(t, prod.tenant, "target-builder", construction.ID, construction.PipelineType+".edit", construction.PipelineType, pipeline.ID, map[string]any{"title": "Unpublished target edit", "expectations": []construction.Expectation{{Column: "qty", Rule: "range", Value: "50..60"}}}, at)
		edited, _ := platform.Get[construction.Pipeline](prod.tenant.automation(construction.ID, false), pipeline.ID)
		accepted, err = owner.ExecutionPipeline(edited)
		if err != nil || accepted.Title != "Clean rows v2" || len(accepted.Expectations) != 1 || accepted.Expectations[0].Value != "0..10" {
			t.Fatal("mutable target editor draft replaced the accepted integration definition")
		}
	}
	if !checkedPipeline {
		t.Fatal("target is missing its upgraded cleaning pipeline")
	}
	tailCut := len(prod.entries)
	checkpoint, _, err := prod.tenant.Snapshot(func() int64 { return int64(tailCut) })
	if err != nil {
		t.Fatal(err)
	}
	if store != nil {
		const code = "w2-integration-release"
		if err := store.SaveSnapshot(context.Background(), prod.tenant.ID, int64(tailCut), code, checkpoint); err != nil {
			t.Fatal(err)
		}
		position, persisted, exists, err := store.Snapshot(context.Background(), prod.tenant.ID, code)
		if err != nil || !exists || position != int64(tailCut) || !bytes.Equal(persisted, checkpoint) {
			t.Fatalf("durable release checkpoint: position=%d exists=%v error=%v", position, exists, err)
		}
		checkpoint = persisted
	}
	decide(t, prod.tenant, "user", construction.ID, typ+".edit", typ, "IN-1", map[string]any{"item": "AFTER-SNAPSHOT"}, at)
	final := compose(prod.tenant.ID)
	if err := final.Restore(checkpoint); err != nil {
		t.Fatal(err)
	}
	if err := final.Replay(durableEntries(prod, tailCut)); err != nil {
		t.Fatal(err)
	}
	if final.ActiveRelease() != v2.ID || final.releases.raw(candidate.ID) == nil {
		t.Fatal("snapshot/tail lost current or historical candidate")
	}
	full := compose(prod.tenant.ID)
	if err := full.Replay(durableEntries(prod, 0)); err != nil {
		t.Fatal(err)
	}
	if snapshot(full) != snapshot(prod.tenant) {
		t.Fatal("full accepted log recovery changed released integration or runtime data")
	}
}
