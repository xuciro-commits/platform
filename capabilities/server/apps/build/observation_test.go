package build

import "platformserver/internal/construction"

import (
	"bytes"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"platformserver/platform"
)

func TestObservationSourceUsesOriginalReleaseAndEnvironmentOwner(t *testing.T) {
	b, rows := lifecycleBuild()
	connection := construction.Connection{Record: platform.Record{ID: "source-connection-id"}, Name: "timeseries", Title: "External observations", Kind: "http", State: "ready", Address: "https://source.example", Secret: "SOURCE_SECRET"}
	rows.rows[reflect.TypeFor[construction.Connection]()] = []any{connection}
	source := construction.Observation{Record: platform.Record{ID: "temperature-source"}, Name: "temperature", Title: "Temperature", Connection: connection.ID, Object: "build.receipt", DeviceField: "item", Signal: "temperature", Unit: "C", Path: "/observations", WindowSeconds: 60, ExpectedRetentionSeconds: 3600, ExpectedGranularitySeconds: 1, Roles: []string{construction.Builder}}
	asset, err := b.integrationAsset(platform.AssetObservation, source, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{connection.ID, connection.Address, connection.Secret} {
		if strings.Contains(string(asset.Body), private) {
			t.Fatalf("portable observation leaked %s", private)
		}
	}
	if !slices.Contains(asset.Requires, platform.AssetRef{App: construction.ID, Kind: platform.AssetConnection, Name: connection.Name}) || len(asset.Provides) != 1 || asset.Provides[0].Side != platform.CapabilityPure {
		t.Fatal("observation omitted its original dependency or pure read", asset)
	}
	accepted, err := retainIntegration(source, asset)
	if err != nil {
		t.Fatal(err)
	}
	live := accepted.(construction.Observation)
	live.State, live.Publisher = "published", "target-builder"
	original := live.Published
	live.Signal, live.Path = "unsaved-signal", "/unsaved"
	rows.rows[reflect.TypeFor[construction.Observation]()] = []any{live}
	definition, err := b.ExecutionObservation(live)
	if err != nil || definition.Signal != "temperature" || definition.Path != "/observations" {
		t.Fatal("mutable editor replaced immutable observation", definition, err)
	}
	pinned, err := b.ResolvePinned(asset.Ref, asset.SourceVersion)
	if err != nil || !bytes.Equal(platform.Raw(pinned), []byte(original)) {
		t.Fatal("retained observation bytes changed", pinned, err)
	}
	target, targetRows := lifecycleBuild()
	targetConnection := connection
	targetConnection.ID, targetConnection.Address, targetConnection.Secret = "target-connection-id", "https://target.example", "TARGET_SECRET"
	targetRows.rows[reflect.TypeFor[construction.Connection]()] = []any{targetConnection}
	seed := source
	seed.ID, seed.Connection, seed.Signal = "target-observation", targetConnection.ID, "unsaved-target-signal"
	targetRows.rows[reflect.TypeFor[construction.Observation]()] = []any{seed}
	connectionAsset, err := b.integrationAsset(platform.AssetConnection, connection, 1)
	if err != nil {
		t.Fatal(err)
	}
	context := platform.AssetReleaseContext{Member: target.host.(lifecycleHost).member}
	requirements, err := target.AssetEnvironmentRequirements(asset)
	if err != nil || len(requirements) != 1 || requirements[0].Ref != connectionAsset.Ref || requirements[0].Profile != "http" {
		t.Fatal("observation invented a separate environment binding", requirements, err)
	}
	plans, err := target.PrepareReleaseAssets([]platform.ReleaseAsset{connectionAsset, asset}, context)
	if err != nil {
		t.Fatal(err)
	}
	for _, plan := range plans {
		if plan.Ref != asset.Ref {
			continue
		}
		var installed construction.Observation
		if json.Unmarshal(plan.Image, &installed) != nil || installed.ID != seed.ID || installed.Connection != targetConnection.ID || installed.Signal != source.Signal {
			t.Fatal("target draft or endpoint replaced candidate declaration", installed)
		}
		if err := target.ValidateAssetInstallation(plan); err != nil {
			t.Fatal(err)
		}
		if err := target.ApplyAssetInstallation(plan, platform.AssetApplyContext{Private: true}); err != nil {
			t.Fatal("accepted observation could not recover", err)
		}
		return
	}
	t.Fatal("observation omitted its original owner installation")
}

func TestObservationSourceRejectsWritableOrMissingActionInputs(t *testing.T) {
	b, rows := lifecycleBuild()
	connection := construction.Connection{Record: platform.Record{ID: "connection"}, Name: "timeseries", Title: "Time series", Kind: "http", State: "ready"}
	rows.rows[reflect.TypeFor[construction.Connection]()] = []any{connection}
	source := construction.Observation{Name: "temperature", Title: "Temperature", Connection: connection.ID, Object: "build.receipt", DeviceField: "item", Signal: "temperature", Unit: "C", Path: "/observations", WindowSeconds: 60, ExpectedRetentionSeconds: 3600, ExpectedGranularitySeconds: 1, Roles: []string{construction.Builder}, Decision: &platform.ObservationDecision{When: platform.ExprLiteralValue("boolean", true), Action: platform.AssetRef{App: construction.ID, Kind: platform.AssetAction, Name: "build.receipt.warn"}, Inputs: map[string]platform.Expr{"points": platform.ExprLiteralValue("text", "raw")}}}
	if _, err := b.integrationAsset(platform.AssetObservation, source, 1); err == nil {
		t.Fatal("undeclared action input accepted")
	}
	source.Decision = nil
	source.Path = "https://external.example/points"
	if _, err := b.integrationAsset(platform.AssetObservation, source, 1); err == nil {
		t.Fatal("portable observation retained an environment URL")
	}
}
