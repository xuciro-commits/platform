package platformserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"platformserver/apps/ai"
	"platformserver/apps/build"
	"platformserver/apps/flow"
	"platformserver/apps/work"
	"platformserver/gateway"
	"platformserver/platform"
)

// Both cities install the same supplied declaration. Only the original
// connection's environment configuration changes; there is no industry app.
func TestHeatingIndustryPackageTwoCitiesAndRetainedRepair(t *testing.T) {
	directory := os.Getenv("PLATFORM_PROTOCOL_PACKAGES")
	if directory == "" {
		t.Skip("set PLATFORM_PROTOCOL_PACKAGES to the real output of make packages")
	}
	manifests, err := filepath.Glob(filepath.Join(directory, "protocol.simulator-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	var native PackageDescriptor
	for _, manifest := range manifests {
		raw, err := os.ReadFile(manifest)
		var descriptor PackageDescriptor
		if err != nil || json.Unmarshal(raw, &descriptor) != nil {
			t.Fatalf("native package manifest: %s: %v", manifest, err)
		}
		if descriptor.ID == "protocol.simulator" && descriptor.Version > native.Version {
			native = descriptor
		}
	}
	if native.Runtime == nil {
		t.Fatal("make packages must provide the native simulator package")
	}
	target, err := native.Runtime.Target()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(filepath.Join(directory, "artifacts", strings.TrimPrefix(target.Artifact, "sha256:")))
	if err != nil {
		t.Fatal(err)
	}
	declaration, err := os.ReadFile("../../deploy/packages/heating.json")
	if err != nil {
		t.Fatal(err)
	}
	var heating PackageDescriptor
	if err := json.Unmarshal(declaration, &heating); err != nil || heating.Blueprint == nil {
		t.Fatalf("heating declaration: %v", err)
	}
	// Isolate the actual source declaration and executable from other package
	// tests that deliberately add a later version to their own directories.
	directory = t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "artifacts"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string][]byte{
		"simulator.json": platform.Raw(native), "heating.json": declaration,
		filepath.Join("artifacts", strings.TrimPrefix(target.Artifact, "sha256:")): binary,
	} {
		if err := os.WriteFile(filepath.Join(directory, name), raw, 0700); err != nil {
			t.Fatal(err)
		}
	}
	index, err := LoadPackageIndex(directory)
	if err != nil {
		t.Fatal(err)
	}
	var firstArtifact []byte
	for _, city := range []string{"harbin", "jinan"} {
		for _, backend := range journalsUnderTest(t) {
			t.Run(city+"/"+backend.name, func(t *testing.T) {
				ctx := context.Background()
				id := fmt.Sprintf("heating-%s-%s-%d", city, backend.name, time.Now().UnixNano())
				t.Cleanup(func() { backend.cleanup(t, id) })
				compose := func() *Tenant {
					return composeTenant(t, id, []Seat{seatOf("builder", "build:builder", "platform:admin", "ai:admin"), seatOf("operator", "build:user", "flow:member", "ai:user"), seatOf("reviewer", "build:user")}, build.New(id), work.New(id), flow.New(id), ai.New(id))
				}
				tn := compose()
				testConsoleOf(tn).BindPackageIndex(index)
				journal := backend.open(t, id)
				defer func() { journal.Close() }()
				if _, err := journal.Entries(ctx, id, 0); err != nil {
					t.Fatal(err)
				}
				tn.attachJournal(ctx, journal)
				at := time.Now().UTC().Truncate(time.Second)
				provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/v1/models" {
						_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]string{"id": "health"}}})
						return
					}
					if r.URL.Path != "/v1/chat/completions" {
						http.NotFound(w, r)
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]any{
						"choices": []any{map[string]any{"message": map[string]string{"content": `{"summary":"The accepted device evidence needs inspection.","nextsteps":"Check the latest observation and inspect the controller locally."}`}}},
						"usage":   map[string]any{"prompt_tokens": 12, "completion_tokens": 24, "cost": 0.001},
					})
				}))
				defer provider.Close()
				decide(t, tn, "builder", ai.ID, ai.SchemaProviderAdd, ai.ProviderType, "heating", map[string]any{"kind": "local", "baseUrl": provider.URL + "/v1"}, at)
				decide(t, tn, "builder", ai.ID, ai.SchemaModelEnable, ai.ModelType, "heating/health", map[string]any{"access": "users"}, at)
				for _, p := range []PackageDescriptor{native, heating} {
					decide(t, tn, "builder", PlatformApp, SchemaPackageInstall, PackageType, p.ID, map[string]any{"id": p.ID, "version": p.Version}, at)
				}
				if _, ok := tn.entity("build.device"); ok {
					t.Fatal("installing the package published its business definitions")
				}
				drafts, err := tn.CreatePackageDrafts(memberOf(t, tn, "builder"), heating.ID, heating.Version, "create-heating", at)
				if err != nil {
					t.Fatal(err)
				}
				if _, installed := tn.entity("build.device"); installed || tn.ActiveRelease() != "" {
					t.Fatal("creating the industry drafts published their runtime")
				}
				selection := make([]build.JointDraftRef, 0, len(drafts.Drafts))
				ids := map[platform.AssetRef]string{}
				for _, draft := range drafts.Drafts {
					selection = append(selection, build.JointDraftRef{Kind: draft.Ref.Kind, ID: draft.ID})
					ids[draft.Ref] = draft.ID
				}
				service, err := gateway.New(gateway.Config{Directory: directory, StateDir: t.TempDir(), Token: "heating-delivery-test", Samples: io.Discard,
					Resolve: func(binding platform.DriverBinding) (platform.DriverRuntime, error) {
						return testConsoleOf(tn).ResolveDriver(binding.Package, binding.Version, binding.Digest, binding.Profile)
					}})
				if err != nil {
					t.Fatal(err)
				}
				defer service.Close()
				server := httptest.NewServer(service)
				defer server.Close()
				secret := func(name string) ([]byte, bool) {
					return []byte("Bearer heating-delivery-test"), name == "heating-gateway" || name == "property-notice"
				}
				tn.Secrets = secret
				messages := make(chan map[string]any, 8)
				property := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodPost && r.URL.Path == "/alarms" {
						var notification map[string]any
						if err := json.NewDecoder(r.Body).Decode(&notification); err != nil || r.Header.Get("Authorization") != "Bearer heating-delivery-test" {
							t.Errorf("property notification: body=%+v error=%v", notification, err)
							w.WriteHeader(http.StatusBadRequest)
							return
						}
						messages <- notification
					}
					_, _ = io.WriteString(w, `{}`)
				}))
				defer property.Close()
				propertyID := ids[platform.AssetRef{App: build.ID, Kind: platform.AssetConnection, Name: "propertyfeed"}]
				decide(t, tn, "builder", build.ID, build.ConnectionType+".edit", build.ConnectionType, propertyID, map[string]any{"address": property.URL + "/", "secret": "property-notice", "allowPrivate": true}, at)
				decide(t, tn, "builder", build.ID, build.ConnectionType+".check", build.ConnectionType, propertyID, map[string]any{}, at)
				var configuration platform.DriverConfiguration
				raw, err := os.ReadFile("../../deploy/examples/heating/" + city + ".json")
				if err != nil || json.Unmarshal(raw, &configuration) != nil {
					t.Fatalf("city environment: %v", err)
				}
				binding := platform.DriverBinding{Package: native.ID, Version: native.Version, Digest: target.Artifact, Profile: "simulator", Configuration: configuration}
				connectionID := ids[platform.AssetRef{App: build.ID, Kind: platform.AssetConnection, Name: "heatingfield"}]
				decide(t, tn, "builder", build.ID, build.ConnectionType+".edit", build.ConnectionType, connectionID, map[string]any{"address": server.URL, "secret": "heating-gateway", "allowPrivate": true, "protocol": binding}, at)
				decide(t, tn, "builder", build.ID, build.ConnectionType+".check", build.ConnectionType, connectionID, map[string]any{}, at)
				tn.CheckConnections(at)
				connection, _ := platform.Get[build.Connection](tn.automation(build.ID, false), connectionID)
				if connection.Last == nil || !connection.Last.OK {
					t.Fatalf("native simulator connection: %+v", connection.Last)
				}
				save := func(key string) string {
					t.Helper()
					builder := memberOf(t, tn, "builder")
					preview, err := tn.PreviewReleaseDrafts(builder, selection)
					if err != nil || preview.Diagnostic != "" {
						t.Fatalf("heating joint review: %v %s", err, preview.Diagnostic)
					}
					saved, err := tn.SaveReleaseCandidates(builder, selection, preview.CandidateID, key, at)
					if err != nil {
						t.Fatal(err)
					}
					measured, err := tn.EvaluateRelease(builder, ReleaseEvaluationRequest{CandidateID: saved, PlanID: "heatingadvicequality", Key: key + ":advice"}, at)
					if err != nil {
						t.Fatal(err)
					}
					for _, effect := range tn.Effects(at) {
						var ask modelAsk
						if json.Unmarshal([]byte(effect.Body), &ask) != nil || !ask.Evaluation || !strings.HasPrefix(ask.Call, measured+":") {
							continue
						}
						outcome, usage := tn.sendModel(effect, at)
						if outcome.Result != "delivered" || usage == nil || !usage.CostReported {
							t.Fatalf("heating advice measurement: %+v %+v", outcome, usage)
						}
						tn.settleWithUsage(effect.ID, outcome, usage, at)
					}
					measurement, ok := platform.Get[build.Evaluation](tn.automation(build.ID, false), measured)
					if !ok || measurement.State != "passed" {
						t.Fatalf("heating advice did not pass its declared synthetic cases: %+v", measurement)
					}
					return saved
				}
				from := save("heating-v1")
				if _, err := tn.ActivateRelease(memberOf(t, tn, "builder"), from, "activate-heating", at); err != nil {
					t.Fatal(err)
				}
				var candidate platform.ReleaseCandidate
				if err := json.Unmarshal(tn.releases.raw(from), &candidate); err != nil {
					t.Fatal(err)
				}
				app := platform.AssetRef{App: build.ID, Kind: platform.AssetApp, Name: "heatingops"}
				artifact, err := platform.ArtifactOfApplication(app, candidate.Assets)
				if err != nil {
					t.Fatal(err)
				}
				if raw := platform.Raw(artifact); firstArtifact == nil {
					firstArtifact = bytes.Clone(raw)
				} else if !bytes.Equal(firstArtifact, raw) {
					t.Fatal("two city environments produced different application artifacts")
				}
				if city == "harbin" && backend.name == "file" {
					for _, asset := range heating.Blueprint.Assets {
						if asset.Ref.Kind != platform.AssetTestPlan {
							continue
						}
						plan, err := platform.ReadTestPlan(asset.Body)
						if err != nil {
							t.Fatal(err)
						}
						request := CandidateSimulationRequest{Roots: plan.Roots, At: plan.At, As: "operator"}
						for _, sample := range plan.Samples {
							request.Samples = append(request.Samples, SimulationSample{Type: sample.Type, Records: sample.Records})
						}
						for _, step := range plan.Steps {
							request.Steps = append(request.Steps, SimulationStep{Read: step.Read, Type: step.Type, ID: step.ID, Action: step.Action, Payload: json.RawMessage(step.Payload), Expect: step.Expect, AdvanceSeconds: step.AdvanceSeconds})
						}
						result, problem := tn.SimulateCandidate(memberOf(t, tn, "builder"), request)
						if problem != nil || result.Passed == nil || !*result.Passed || !result.Recovered {
							for i, step := range result.Steps {
								if step.Matched != nil && !*step.Matched {
									t.Errorf("scenario %s step %d accepted=%t: %s", plan.Name, i+1, step.Accepted, step.Refusal)
								}
							}
							t.Fatalf("portable scenario %s recovered=%t: %#v", plan.Name, result.Recovered, problem)
						}
					}
				}
				rows, failure := tn.Records(memberOf(t, tn, "operator"), "build.device", platform.Query{}, at)
				if failure != nil || len(rows.Records) != 0 {
					t.Fatalf("package/sample installation created live devices: %d %v", len(rows.Records), failure)
				}
				var samples platform.SampleSet
				for _, asset := range heating.Blueprint.Assets {
					if asset.Ref.Kind == platform.AssetSampleSet {
						if err := json.Unmarshal(asset.Body, &samples); err != nil {
							t.Fatal(err)
						}
					}
				}
				// Loading a saved sample is explicit business creation, never a
				// package installer privilege or a direct write to record storage.
				for _, group := range samples.Samples {
					for _, raw := range group.Records {
						var fields map[string]any
						if err := json.Unmarshal(raw, &fields); err != nil {
							t.Fatal(err)
						}
						rid, _ := fields["id"].(string)
						for _, name := range []string{"id", "revision", "created", "changed", "archived", "state"} {
							delete(fields, name)
						}
						decide(t, tn, "operator", build.ID, group.Type+".create", group.Type, rid, fields, at)
					}
				}
				// The live access pass must enter its normal lifecycle explicitly.
				decide(t, tn, "operator", build.ID, "build.accesspass.grant", "build.accesspass", "passvalid", map[string]any{}, at)
				decide(t, tn, "operator", build.ID, "build.accesspoint.checkpass", "build.accesspoint", "accesspointdemo", map[string]any{"pass": "passvalid", "holder": "operator-demo"}, at)
				binding.Configuration.Scenario = "anomaly"
				decide(t, tn, "builder", build.ID, build.ConnectionType+".edit", build.ConnectionType, connectionID, map[string]any{"protocol": binding}, at)
				decide(t, tn, "builder", build.ID, build.ConnectionType+".check", build.ConnectionType, connectionID, map[string]any{}, at)
				tn.CheckConnections(at)
				positionRef := platform.AssetRef{App: build.ID, Kind: platform.AssetObservation, Name: "heatingposition"}
				temperatureRef := platform.AssetRef{App: build.ID, Kind: platform.AssetObservation, Name: "heatingtemperature"}
				before := journal.Position(id)
				answer, failure := tn.EvaluateObservation(ctx, memberOf(t, tn, "operator"), temperatureRef, ObservationEvaluate{Record: "devicedemo", Until: at, Key: "temperature-alarm"}, at)
				if failure != nil || !answer.Matched || answer.Receipt == nil {
					t.Fatalf("real anomaly did not create business results: %+v %v", answer, failure)
				}
				entries, err := journal.Entries(ctx, id, before)
				if err != nil || len(entries) == 0 {
					t.Fatalf("accepted observation log: %v", err)
				}
				for _, entry := range entries {
					if bytes.Contains(entry.Body, []byte(`"receivedAt"`)) || bytes.Contains(entry.Body, []byte(`"points":[{"at":`)) {
						t.Fatal("raw samples entered the business journal")
					}
				}
				answer, failure = tn.EvaluateObservation(ctx, memberOf(t, tn, "operator"), temperatureRef, ObservationEvaluate{Record: "devicedemo", Until: at, Key: "temperature-repeat"}, at)
				if failure != nil || answer.Matched {
					t.Fatalf("repeated abnormal window was not suppressed: %+v %v", answer, failure)
				}
				alarms, failure := tn.Records(memberOf(t, tn, "operator"), "build.alarm", platform.Query{}, at)
				orders, orderFailure := tn.Records(memberOf(t, tn, "operator"), "build.workorder", platform.Query{}, at)
				if failure != nil || orderFailure != nil || len(alarms.Records) != 1 || len(orders.Records) != 1 {
					t.Fatalf("exception results alarms=%d workorders=%d errors=%v/%v", len(alarms.Records), len(orders.Records), failure, orderFailure)
				}
				deviceBefore, _ := tn.Held("build.device/devicedemo")
				acceptedDevice := bytes.Clone(platform.Raw(deviceBefore))
				decide(t, tn, "operator", build.ID, "build.function-call.start", build.FunctionCallType, "device-advice", map[string]any{"app": build.ID, "name": "heatingadvice", "source": "devicedemo"}, at)
				tn.Dispatch(at)
				advice, ok := platform.Get[build.FunctionRun](tn.automation(build.ID, false), "device-advice")
				deviceAfter, _ := tn.Held("build.device/devicedemo")
				if !ok || advice.State != "ready" || len(advice.SourceValues) != 4 || !bytes.Equal(acceptedDevice, platform.Raw(deviceAfter)) {
					t.Fatalf("advice was not a separate read-only result over accepted device evidence: %+v", advice)
				}
				select {
				case message := <-messages:
					if message["device"] != "devicedemo" || message["summary"] == "" || message["acceptedTemperature"] == nil || len(message) != 3 {
						t.Fatalf("property notice is not the compact accepted alarm: %+v", message)
					}
				default:
					t.Fatal("accepted alarm was not sent through the original writeback")
				}
				if len(messages) != 0 {
					t.Fatal("the repeated observation sent a duplicate property notice")
				}
				at = at.Add(2 * time.Second)
				tn.Work(at)
				instances, _, failure := platform.Find[platform.FlowInstance](tn.automation(flow.ID, false), platform.Query{})
				if failure != nil || len(instances) != 1 || instances[0].State != "waiting" || instances[0].Release != from {
					t.Fatalf("repair did not wait on its exact application version: %+v %v", instances, failure)
				}
				started := instances[0]
				tasks, _, failure := platform.Find[platform.WorkTask](tn.automation(work.ID, false), platform.Query{})
				if failure != nil || len(tasks) != 1 || tasks[0].State != "open" {
					t.Fatalf("repair task: %+v %v", tasks, failure)
				}

				// Add a field, rename an existing field and retire the old action
				// while the original operator confirmation is still outstanding.
				workorderRef := platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "build.workorder"}
				processRef := platform.AssetRef{App: build.ID, Kind: platform.AssetFlow, Name: "build.heatingrepair"}
				workorder, ok := platform.Get[build.Object](tn.automation(build.ID, false), ids[workorderRef])
				if !ok {
					t.Fatal("import omitted the workorder draft")
				}
				for i := range workorder.Fields {
					if workorder.Fields[i].Name == "description" {
						workorder.Fields[i].ReadOnly, workorder.Fields[i].AliasOf = true, "instructions"
					}
				}
				workorder.Fields = append(workorder.Fields, build.Field{Name: "instructions", Title: "Inspection instructions", Type: "longtext"}, build.Field{Name: "verified", Title: "Site configuration verified", Type: "boolean", Required: true})
				for i := range workorder.Actions {
					if workorder.Actions[i].Name == "complete" {
						workorder.Actions[i].Name, workorder.Actions[i].Title = "finishinspection", "Finish inspection"
					}
				}
				process, ok := platform.Get[build.Process](tn.automation(build.ID, false), ids[processRef])
				if !ok {
					t.Fatal("import omitted the repair workflow draft")
				}
				for i := range process.Steps {
					if process.Steps[i].Act == "build.workorder.complete" {
						process.Steps[i].Act = "build.workorder.finishinspection"
					}
				}
				decide(t, tn, "builder", build.ID, build.ObjectType+".edit", build.ObjectType, workorder.ID, map[string]any{"fields": workorder.Fields, "actions": workorder.Actions}, at)
				decide(t, tn, "builder", build.ID, build.ProcessType+".edit", build.ProcessType, process.ID, map[string]any{"steps": process.Steps}, at)
				deviceRef := platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "build.device"}
				device, _ := platform.Get[build.Object](tn.automation(build.ID, false), ids[deviceRef])
				for _, action := range device.Actions {
					if action.Transaction == nil {
						continue
					}
					for i := range action.Transaction.Steps {
						step := &action.Transaction.Steps[i]
						if step.Kind == "create" && step.Target.Type == "build.workorder" {
							for j := range step.Changes {
								if step.Changes[j].Field == "description" {
									step.Changes[j].Field = "instructions"
								}
							}
						}
					}
				}
				decide(t, tn, "builder", build.ID, build.ObjectType+".edit", build.ObjectType, device.ID, map[string]any{"actions": device.Actions}, at)
				// Upgrade the changed drafts; unchanged functions and test plans
				// retain their explicit published versions in the same closure.
				selection = []build.JointDraftRef{{Kind: platform.AssetObject, ID: workorder.ID}, {Kind: platform.AssetObject, ID: device.ID}, {Kind: platform.AssetFlow, ID: process.ID}, {Kind: platform.AssetApp, ID: drafts.ApplicationID}}
				for ref, pageID := range ids {
					if ref.Kind != platform.AssetPage {
						continue
					}
					page, _ := platform.Get[build.Page](tn.automation(build.ID, false), pageID)
					raw := platform.Raw(page)
					changed := bytes.ReplaceAll(raw, []byte("build.workorder.complete"), []byte("build.workorder.finishinspection"))
					if bytes.Equal(raw, changed) {
						continue
					}
					if err := json.Unmarshal(changed, &page); err != nil {
						t.Fatal(err)
					}
					decide(t, tn, "builder", build.ID, build.PageType+".edit", build.PageType, pageID, map[string]any{"actions": page.Actions, "sections": page.Sections, "document": page.Document}, at)
					selection = append(selection, build.JointDraftRef{Kind: platform.AssetPage, ID: pageID})
				}
				to := save("heating-v2")
				defaultValue := platform.ExprLiteralValue("boolean", true)
				plan := platform.MigrationPlan{From: from, To: to, Scope: platform.MigrationScope{Type: "build.workorder"}, Steps: []platform.MigrateStep{
					{Kind: "rename", Types: []string{"build.workorder"}, Batch: 1, Fields: []platform.MigrateField{{From: "description", To: "instructions"}}},
					{Kind: "default", Types: []string{"build.workorder"}, Batch: 1, Fields: []platform.MigrateField{{To: "verified", Expr: &defaultValue}}},
					{Kind: "retire-action", Types: []string{"build.workorder"}, Batch: 1, Action: "build.workorder.complete"},
				}}
				review, err := tn.ReviewMigrationPlan(memberOf(t, tn, "builder"), plan, at)
				if err != nil {
					t.Fatal(err)
				}
				decide(t, tn, "builder", build.ID, "build.migration.start", platform.MigrationRunType, "heating-evolution", map[string]any{"plan": plan, "review": review.ID, "reviewedAt": review.At}, at)
				for range plan.Steps {
					decide(t, tn, "builder", build.ID, "build.migration.advance", platform.MigrationRunType, "heating-evolution", map[string]any{}, at)
				}
				if _, err := tn.ActivateRelease(memberOf(t, tn, "builder"), to, "activate-heating-v2", at); err != nil {
					t.Fatal(err)
				}
				original := bytes.Clone(tn.releases.raw(from))
				files := tn.files()
				journal.Close()
				journal = backend.reopen(t, id)
				entries, err = journal.Entries(ctx, id, 0)
				if err != nil {
					t.Fatal(err)
				}
				tn = compose()
				tn.Files, tn.Secrets = files, secret
				testConsoleOf(tn).BindPackageIndex(index)
				tn.Outbound = func(*http.Request, bool) (*http.Response, error) {
					t.Error("recovery performed external I/O")
					return nil, fmt.Errorf("recovery must use retained results")
				}
				tn.AIClient = func(*http.Request) (*http.Response, error) {
					t.Error("recovery called the model provider")
					return nil, fmt.Errorf("recovery must use the accepted model result")
				}
				if err := tn.Replay(entries); err != nil {
					t.Fatal(err)
				}
				tn.Outbound, tn.AIClient = nil, nil
				tn.attachJournal(ctx, journal)
				waiting, ok := platform.Get[platform.FlowInstance](tn.automation(flow.ID, false), started.ID)
				if !ok || waiting.State != "waiting" || waiting.Version != started.Version || waiting.Release != from || waiting.Dependencies != started.Dependencies {
					t.Fatalf("reopened waiting repair changed its version: %+v", waiting)
				}
				decide(t, tn, "operator", work.ID, work.TaskType+".complete", work.TaskType, tasks[0].ID, map[string]any{"answer": "Repair confirmed"}, at)
				at = at.Add(2 * time.Second)
				tn.Work(at)
				ended, _ := platform.Get[platform.FlowInstance](tn.automation(flow.ID, false), started.ID)
				if ended.State != "done" || ended.Release != from || !bytes.Equal(original, tn.releases.raw(from)) {
					t.Fatalf("retained repair did not complete on its original version: %+v", ended)
				}
				order, ok := tn.Held(started.Subject)
				if !ok {
					t.Fatal("repair subject disappeared")
				}
				result := toMap(order)
				instructions, _ := result["instructions"].(string)
				if result["state"] != "done" || result["verified"] != true || instructions == "" || result["note"] != "Operator confirmed inspection and repair in the retained workflow" {
					t.Fatalf("old repair lost migrated data or used the retired catalog: %+v", result)
				}
				alarmID, _ := toMap(alarms.Records[0])["id"].(string)
				decide(t, tn, "operator", build.ID, "build.alarm.acknowledge", "build.alarm", alarmID, map[string]any{"note": "Inspection owner confirmed the accepted alarm"}, at)
				decide(t, tn, "operator", build.ID, "build.alarm.resolve", "build.alarm", alarmID, map[string]any{"note": "Repair confirmation reviewed; this exception is resolved"}, at)

				// A lost command reply stays uncertain even though the simulator
				// applied the setting; only a subsequent exact observation completes it.
				binding.Configuration.Scenario = "unknown"
				at = time.Now().UTC()
				decide(t, tn, "builder", build.ID, build.ConnectionType+".edit", build.ConnectionType, connectionID, map[string]any{"protocol": binding}, at)
				decide(t, tn, "builder", build.ID, build.ConnectionType+".check", build.ConnectionType, connectionID, map[string]any{}, at)
				tn.CheckConnections(at)
				decide(t, tn, "operator", build.ID, "build.circuit.setvalve", "build.circuit", "circuitdemo", map[string]any{"opening": 62}, at)
				requests, _, failure := platform.Find[platform.ApprovalRequest](tn.automation(work.ID, false), platform.Query{Domain: platform.Raw([]any{[]any{"state", "=", "pending"}})})
				if failure != nil || len(requests) != 1 {
					t.Fatalf("valve request bypassed approval: %+v %v", requests, failure)
				}
				decide(t, tn, "reviewer", work.ID, "work.approval.approve", work.ApprovalType, requests[0].ID, map[string]any{}, at)
				tn.Work(at.Add(time.Second))
				tn.Dispatch(at.Add(time.Second))
				run := currentDeviceDispatch(t, tn)
				if run.State != "unknown" || run.Attempt != 1 {
					t.Fatalf("lost valve reply was treated as completion: %+v", run)
				}
				tn.ReadDispatches(at.Add(3 * time.Second))
				if run := currentDeviceDispatch(t, tn); run.State != "unknown" {
					t.Fatalf("the unverified valve position completed: %+v", run)
				}
				at = time.Now().UTC().Add(2 * time.Second)
				answer, failure = tn.EvaluateObservation(ctx, memberOf(t, tn, "operator"), positionRef, ObservationEvaluate{Record: "circuitdemo", Until: at, Key: "verified-valve"}, at)
				if failure != nil || !answer.Matched {
					t.Fatalf("fresh exact position did not confirm the valve: %+v %v", answer, failure)
				}
				tn.ReadDispatches(at.Add(5 * time.Second))
				run = currentDeviceDispatch(t, tn)
				if run.State != "completed" || run.Attempt != 1 {
					t.Fatalf("confirmed position did not complete the dispatch: %+v", run)
				}
				position, _ := tn.Held("build.circuit/circuitdemo")
				got := toMap(position)
				observedAt, _ := time.Parse(time.RFC3339Nano, fmt.Sprint(got["observedat"]))
				requestedAt, _ := time.Parse(time.RFC3339Nano, fmt.Sprint(got["requestedat"]))
				if got["state"] != "steady" || !reflect.DeepEqual(got["actualopening"], got["requestedopening"]) || !observedAt.After(requestedAt) || !observedAt.After(run.StartedAt) {
					t.Fatalf("circuit result differs from the verified request: %+v", got)
				}
			})
		}
	}
}
