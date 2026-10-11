package platformserver

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
		tenant := composeTenant(t, id, []Seat{seatOf(releaseFabricBuilder(id), build.ID+":"+build.Builder), seatOf("user", build.ID+":"+build.User), seatOf("publisher", build.ID+":"+build.Publisher)}, work.New(id), flow.New(id), build.New(id))
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
		decide(t, e.tenant, releaseFabricBuilder(e.tenant.ID), build.ID, build.ConnectionType+".create", build.ConnectionType, id, map[string]any{"name": "external", "title": "Environment connection", "kind": "http", "address": address + "/", "secret": "local-" + e.tenant.ID, "allowPrivate": true}, at)
		decide(t, e.tenant, releaseFabricBuilder(e.tenant.ID), build.ID, build.ConnectionType+".check", build.ConnectionType, id, map[string]any{}, at)
		e.tenant.CheckConnections(at)
		c, _ := platform.Get[build.Connection](e.tenant.automation(build.ID, false), id)
		if c.State != "ready" {
			t.Fatalf("connection did not become a usable local binding: %+v", c.Last)
		}
	}
	connection(dev, "dev-connection", devServer.URL)
	typ := publishObject(t, dev.tenant, "dev-builder", "O", map[string]any{"name": "receipt", "title": domain + " receipt", "fields": []map[string]any{{"name": "item", "title": "Item", "type": "text"}, {"name": "qty", "title": "Quantity", "type": "integer"}, {"name": "document", "title": "Delivery document", "type": "text"}}, "states": []map[string]any{{"name": "open", "title": "Open"}, {"name": "received", "title": "Received"}}, "actions": []map[string]any{{"name": "receive", "title": "Receive", "from": []string{"open"}, "to": "received", "roles": []string{build.User, build.Builder}}}}, at)
	decide(t, dev.tenant, "dev-builder", build.ID, build.MatchType+".create", build.MatchType, "dev-matching", map[string]any{"name": "receiptidentity", "title": "Match received items", "object": typ, "keys": []build.MatchKey{{Fields: []string{"item"}, Normalize: "folded"}}}, at)
	decide(t, dev.tenant, "dev-builder", build.ID, build.MatchType+".publish", build.MatchType, "dev-matching", map[string]any{}, at)
	for _, ds := range []struct{ id, name string }{{"dev-raw", "raw"}, {"dev-clean", "clean"}} {
		decide(t, dev.tenant, "dev-builder", build.ID, build.DatasetType+".create", build.DatasetType, ds.id, map[string]any{"name": ds.name, "title": ds.name + " rows"}, at)
	}
	decide(t, dev.tenant, "dev-builder", build.ID, build.SourceType+".create", build.SourceType, "dev-source", map[string]any{"name": "feed", "title": "External rows", "connection": "dev-connection", "profile": "json", "url": "rows", "since": "seq", "dataset": "dev-raw"}, at)
	decide(t, dev.tenant, "dev-builder", build.ID, build.SourceType+".publish", build.SourceType, "dev-source", map[string]any{}, at)
	dev.tenant.PullSources(at)
	for _, p := range []struct {
		id, name, input, output string
		object                  bool
	}{{"dev-clean-pipeline", "cleaning", "dev-raw", "dev-clean", false}, {"dev-land-pipeline", "landing", "dev-clean", typ, true}} {
		definition := map[string]any{"name": p.name, "title": p.name, "input": p.input, "steps": []build.Step{{Kind: "select", Columns: []string{"id", "sku", "qty"}}, {Kind: "cast", Column: "qty", Type: "number"}, {Kind: "rename", From: "sku", To: "item"}}}
		if p.object {
			definition["outputObject"], definition["key"] = p.output, "id"
			definition["steps"] = []build.Step{}
		} else {
			definition["outputDataset"] = p.output
		}
		decide(t, dev.tenant, "dev-builder", build.ID, build.PipelineType+".create", build.PipelineType, p.id, definition, at)
		decide(t, dev.tenant, "dev-builder", build.ID, build.PipelineType+".publish", build.PipelineType, p.id, map[string]any{}, at)
	}
	dev.tenant.RunPipelines(at)
	dev.tenant.RunPipelines(at.Add(time.Second))
	decide(t, dev.tenant, "dev-builder", build.ID, build.WritebackType+".create", build.WritebackType, "dev-writeback", map[string]any{"name": "receiptdelivery", "title": "Receipt delivery", "connection": "dev-connection", "object": typ, "on": "receive", "path": "receipts", "result": []build.SourceField{{From: "document", To: "document"}}}, at)
	decide(t, dev.tenant, "dev-builder", build.ID, build.WritebackType+".publish", build.WritebackType, "dev-writeback", map[string]any{}, at)
	decide(t, dev.tenant, "dev-builder", build.ID, build.PageType+".create", build.PageType, "P", map[string]any{"name": "desk", "title": "Receiving desk", "object": typ, "list": []string{"item", "qty", "state", "document"}, "detail": []string{"item", "qty", "state", "document"}}, at)
	decide(t, dev.tenant, "dev-builder", build.ID, build.ProcessType+".create", build.ProcessType, "F", map[string]any{"name": "receipthold", "title": "Keep receipt work in flight", "object": typ, "when": "received", "steps": []build.ProcessStep{{Name: "hold", Kind: "wait", UntilSeconds: 3600, Next: "done"}, {Name: "done", Kind: "end"}}}, at)
	resources := []platform.AssetRef{{App: build.ID, Kind: platform.AssetFlow, Name: "build.receipthold"}, {App: build.ID, Kind: platform.AssetSource, Name: "feed"}, {App: build.ID, Kind: platform.AssetPipeline, Name: "cleaning"}, {App: build.ID, Kind: platform.AssetPipeline, Name: "landing"}, {App: build.ID, Kind: platform.AssetWriteback, Name: "receiptdelivery"}, {App: build.ID, Kind: platform.AssetMatching, Name: "receiptidentity"}}
	decide(t, dev.tenant, "dev-builder", build.ID, build.AppType+".create", build.AppType, "A", map[string]any{"name": "receiving", "title": domain + " receiving", "pages": []string{"desk"}, "resources": resources}, at)
	drafts := []build.JointDraftRef{{Kind: platform.AssetObject, ID: "O"}, {Kind: platform.AssetPage, ID: "P"}, {Kind: platform.AssetApp, ID: "A"}, {Kind: platform.AssetFlow, ID: "F"}}
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
	devData, _ := platform.Get[build.Dataset](dev.tenant.automation(build.ID, false), "dev-raw")
	devSource, _ := platform.Get[build.Source](dev.tenant.automation(build.ID, false), "dev-source")
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
	decide(t, wrong.tenant, "target-builder", build.ID, build.ConnectionType+".create", build.ConnectionType, "wrong-connection", map[string]any{"name": "external", "title": "Wrong binding kind", "kind": "odata", "address": prodServer.URL + "/", "secret": "local-" + wrong.tenant.ID, "allowPrivate": true}, at)
	decide(t, wrong.tenant, "target-builder", build.ID, build.ConnectionType+".check", build.ConnectionType, "wrong-connection", map[string]any{}, at)
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
	decide(t, incompatible.tenant, "user", build.ID, legacyType+".create", legacyType, "existing", map[string]any{"legacy": "PRESERVE"}, at)
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
	c := prod.tenant.automation(build.ID, false)
	datasets, _, _ := platform.Find[build.Dataset](c, platform.Query{})
	sources, _, _ := platform.Find[build.Source](c, platform.Query{})
	if len(datasets) != 2 || len(sources) != 1 || sources[0].Connection != "prod-connection" || sources[0].Cursor != "" || sources[0].Last != nil || sources[0].Puller != "target-builder" {
		t.Fatalf("target did not resolve local references or copied source runtime: datasets=%+v sources=%+v", datasets, sources)
	}
	for _, dataset := range datasets {
		if dataset.Version != 0 || dataset.Last != nil {
			t.Fatal("promotion copied dataset contents/version")
		}
	}
	connectionRow, _ := platform.Get[build.Connection](c, "prod-connection")
	if connectionRow.Address != prodServer.URL+"/" || connectionRow.Secret != "local-"+prod.tenant.ID {
		t.Fatal("promotion replaced the target environment binding")
	}
	decide(t, prod.tenant, "target-builder", build.ID, build.SourceType+".pull", build.SourceType, sources[0].ID, map[string]any{}, at)
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
	decide(t, prod.tenant, "user", build.ID, typ+".receive", typ, "IN-1", map[string]any{}, at)
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
	decide(t, prod.tenant, "target-builder", build.ID, build.SourceType+".pull", build.SourceType, sources[0].ID, map[string]any{}, pullAt)
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
	beforeSource, _ := platform.Get[build.Source](prod.tenant.automation(build.ID, false), sources[0].ID)
	beforeDataset := map[string]int{}
	for _, dataset := range datasets {
		current, _ := platform.Get[build.Dataset](prod.tenant.automation(build.ID, false), dataset.ID)
		beforeDataset[dataset.ID] = current.Version
	}
	decide(t, dev.tenant, "dev-builder", build.ID, build.PipelineType+".edit", build.PipelineType, "dev-clean-pipeline", map[string]any{"title": "Clean rows v2", "expectations": []build.Expectation{{Column: "qty", Rule: "range", Value: "0..10"}}}, at)
	decide(t, dev.tenant, "dev-builder", build.ID, build.PipelineType+".publish", build.PipelineType, "dev-clean-pipeline", map[string]any{}, at)
	decide(t, dev.tenant, "dev-builder", build.ID, build.WritebackType+".edit", build.WritebackType, "dev-writeback", map[string]any{"result": []build.SourceField{{From: "document", To: "item"}}}, at)
	decide(t, dev.tenant, "dev-builder", build.ID, build.WritebackType+".publish", build.WritebackType, "dev-writeback", map[string]any{}, at)
	decide(t, dev.tenant, "dev-builder", build.ID, build.SourceType+".edit", build.SourceType, "dev-source", map[string]any{"title": "External rows v2"}, at)
	decide(t, dev.tenant, "dev-builder", build.ID, build.SourceType+".publish", build.SourceType, "dev-source", map[string]any{}, at)
	v2 := freeze("save-v2")
	if v2.ID == candidate.ID {
		t.Fatal("changed integration definition did not change the candidate")
	}
	if _, err := PromoteCandidate(dev.tenant, prod.tenant, v2.ID, "deliver-v2", true, memberOf(t, prod.tenant, "target-builder"), at); err != nil {
		t.Fatalf("supported definition upgrade: %v", err)
	}
	view, _ = prod.tenant.RecordOf(memberOf(t, prod.tenant, "user"), typ, "IN-1", at)
	afterRow, _ := json.Marshal(view.Record)
	afterSource, _ := platform.Get[build.Source](prod.tenant.automation(build.ID, false), sources[0].ID)
	if beforeSource.Cursor != "9" || beforeSource.Cursor != afterSource.Cursor || beforeSource.Last == nil || afterSource.Last == nil || !beforeSource.Last.At.Equal(afterSource.Last.At) {
		t.Fatal("definition upgrade reset source runtime/cursor")
	}
	for datasetID, version := range beforeDataset {
		current, _ := platform.Get[build.Dataset](prod.tenant.automation(build.ID, false), datasetID)
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
	finishedSource, _ := platform.Get[build.Source](prod.tenant.automation(build.ID, false), sources[0].ID)
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
	pipelines, _, _ := platform.Find[build.Pipeline](prod.tenant.automation(build.ID, false), platform.Query{})
	checkedPipeline := false
	for _, pipeline := range pipelines {
		if pipeline.Name != "cleaning" {
			continue
		}
		checkedPipeline = true
		owner := prod.tenant.app(build.ID).(*build.Build)
		accepted, err := owner.ExecutionPipeline(pipeline)
		if err != nil || accepted.Title != "Clean rows v2" || len(accepted.Expectations) != 1 || accepted.Expectations[0].Value != "0..10" {
			t.Fatalf("target execution did not select the upgraded integration definition: %+v %v", accepted, err)
		}
		decide(t, prod.tenant, "target-builder", build.ID, build.PipelineType+".edit", build.PipelineType, pipeline.ID, map[string]any{"title": "Unpublished target edit", "expectations": []build.Expectation{{Column: "qty", Rule: "range", Value: "50..60"}}}, at)
		edited, _ := platform.Get[build.Pipeline](prod.tenant.automation(build.ID, false), pipeline.ID)
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
	decide(t, prod.tenant, "user", build.ID, typ+".edit", typ, "IN-1", map[string]any{"item": "AFTER-SNAPSHOT"}, at)
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
