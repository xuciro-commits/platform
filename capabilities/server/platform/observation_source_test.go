package platform

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"
)

func observationDefinitionForTest() (ObservationDefinition, EntityInfo) {
	object := AssetRef{App: "build", Kind: AssetObject, Name: "build.device"}
	entity := EntityInfo{App: object.App, Type: object.Name, Fields: []FieldInfo{{Name: "device", Type: "text"}, {Name: "limit", Type: "decimal"}, {Name: "state", Type: "choice"}}}
	definition := ObservationDefinition{Name: "temperature", Title: "Temperature", Connection: AssetRef{App: "build", Kind: AssetConnection, Name: "telemetry"}, Object: object, DeviceField: "device", Signal: "temperature", Unit: "C", Path: "/summary", WindowSeconds: 120, ExpectedRetentionSeconds: 86400, ExpectedGranularitySeconds: 1, Roles: []string{"user"}, Decision: &ObservationDecision{Action: AssetRef{App: "build", Kind: AssetAction, Name: "build.device.alert"}, When: Expr{Op: "and", Kind: "boolean", Args: []Expr{
		{Op: "=", Kind: "boolean", Args: []Expr{ExprReference("choice", "record.state"), ExprLiteralValue("choice", "normal")}},
		{Op: ">", Kind: "boolean", Args: []Expr{ExprReference("decimal", "observation.latest"), ExprReference("decimal", "record.limit")}},
		{Op: ">", Kind: "boolean", Args: []Expr{{Op: "rate", Kind: "decimal", Window: &ExprWindowReference{Source: "observation"}}, ExprLiteralValue("decimal", "0.1")}},
	}}, Inputs: map[string]Expr{"latest": ExprReference("decimal", "observation.latest"), "delta": {Op: "delta", Kind: "decimal", Window: &ExprWindowReference{Source: "observation"}}, "count": {Op: "over-window", Kind: "integer", Window: &ExprWindowReference{Source: "observation", Aggregate: "count"}}}}}
	return definition, entity
}

func observationWindowForTest() ObservationWindow {
	since := time.Date(2026, 10, 15, 1, 0, 0, 0, time.UTC)
	return ObservationWindow{Since: since, Until: since.Add(120 * time.Second), Count: 10_000_000, FirstAt: since.Add(time.Second), LastAt: since.Add(101 * time.Second), First: "20.00000000000000000001", Last: "40.00000000000000000003", Min: "20", Max: "41", Sum: "300000000", Mean: "30", Unit: "C"}
}

func TestObservationCurrentPointPreservesQualityAndExactValue(t *testing.T) {
	at := time.Now().UTC()
	sample := DriverSample{Device: "D1", Signal: "temperature", Unit: "C", Type: "decimal", Value: "23.50000000000000001", Quality: "good", At: at, ReceivedAt: at}
	if err := CheckObservationLatest(sample, "D1", "temperature", "C"); err != nil {
		t.Fatal(err)
	}
	for _, quality := range []string{"bad", "stale", "uncertain"} {
		missing := sample
		missing.Value, missing.Quality = nil, quality
		if err := CheckObservationLatest(missing, "D1", "temperature", "C"); err != nil {
			t.Fatalf("lost a missing %s reading: %v", quality, err)
		}
	}
	for name, change := range map[string]func(*DriverSample){
		"other device": func(s *DriverSample) { s.Device = "D2" },
		"other unit":   func(s *DriverSample) { s.Unit = "F" },
		"rounded JSON": func(s *DriverSample) { s.Value = 23.5 },
		"not finite":   func(s *DriverSample) { s.Value = "NaN" },
		"missing good": func(s *DriverSample) { s.Value = nil },
		"bad value":    func(s *DriverSample) { s.Quality = "bad" },
	} {
		invalid := sample
		change(&invalid)
		if CheckObservationLatest(invalid, "D1", "temperature", "C") == nil {
			t.Fatal("accepted invalid current reading:", name)
		}
	}
}

func TestObservationThresholdTrendAndCompactExactInputs(t *testing.T) {
	definition, entity := observationDefinitionForTest()
	window := observationWindowForTest()
	matched, inputs, err := EvaluateObservation(definition, entity, Raw(map[string]any{"id": "DEVICE", "device": "D-1", "limit": "35", "state": "normal"}), window, window.Until)
	if err != nil || !matched {
		t.Fatalf("threshold + trend failed: matched=%v inputs=%v err=%v", matched, inputs, err)
	}
	if string(inputs["latest"]) != "40.00000000000000000003" || string(inputs["delta"]) != "20.00000000000000000002" || string(inputs["count"]) != "10000000" {
		t.Fatalf("accepted scalar precision was lost: %s %s %s", inputs["latest"], inputs["delta"], inputs["count"])
	}
	context, err := ObservationRuleContext(definition, Raw(map[string]any{"id": "DEVICE", "state": "normal", "limit": "35", "unrelated": []int{1, 2, 3}}), window)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(context)
	if len(raw) > 2048 || strings.Contains(string(raw), "unrelated") || len(context.Sets) != 0 || len(context.Windows) != 1 {
		t.Fatal("rule context copied raw arrays or scaled with sample count")
	}
	matched, _, err = EvaluateObservation(definition, entity, Raw(map[string]any{"id": "DEVICE", "device": "D-1", "limit": "35", "state": "alert"}), window, window.Until)
	if err != nil || matched {
		t.Fatalf("original state failed to suppress repeated warning: %v %v", matched, err)
	}
}

func TestObservationNoDataDoesNotInventValues(t *testing.T) {
	definition, entity := observationDefinitionForTest()
	window := observationWindowForTest()
	window.Count = 0
	window.FirstAt = time.Time{}
	window.LastAt = time.Time{}
	window.First, window.Last, window.Min, window.Max, window.Sum, window.Mean = "", "", "", "", "", ""
	matched, inputs, err := EvaluateObservation(definition, entity, Raw(map[string]any{"id": "DEVICE", "state": "normal", "limit": "35"}), window, window.Until)
	if err != nil || matched || len(inputs) != 0 {
		t.Fatalf("no-data generated a decision: %v %v %v", matched, inputs, err)
	}
	schema, _ := ObservationRuleSchema(definition, entity)
	context, _ := ObservationRuleContext(definition, Raw(map[string]any{"id": "DEVICE"}), window)
	if context.Refs["observation.lastat"].Status != "missing" {
		t.Fatal("an empty window invented a sample time")
	}
	for _, op := range []string{"delta", "rate"} {
		program, err := CompileExpr(Expr{Op: op, Kind: "decimal", Window: &ExprWindowReference{Source: "observation"}}, schema)
		if err != nil {
			t.Fatal(err)
		}
		result, err := program.Eval(context)
		if err != nil || result.Value.Status != "missing" {
			t.Fatalf("%s invented an empty value: %+v %v", op, result, err)
		}
	}
	program, err := CompileExpr(Expr{Op: "over-window", Kind: "integer", Window: &ExprWindowReference{Source: "observation", Aggregate: "count"}}, schema)
	if err != nil {
		t.Fatal(err)
	}
	result, err := program.Eval(context)
	if err != nil || string(result.Value.Data) != `"0"` {
		t.Fatalf("empty count was not zero: %+v %v", result, err)
	}
}

func TestObservationUsesSampleTimeForFreshEvidence(t *testing.T) {
	definition, entity := observationDefinitionForTest()
	entity.Fields = append(entity.Fields, FieldInfo{Name: "requestedat", Type: "datetime"})
	definition.Decision.When = Expr{Op: ">", Kind: "boolean", Args: []Expr{ExprReference("datetime", "observation.lastat"), ExprReference("datetime", "record.requestedat")}}
	window := observationWindowForTest()
	for _, offset := range []time.Duration{time.Second, 0, -time.Second} {
		matched, _, err := EvaluateObservation(definition, entity, Raw(map[string]any{"id": "DEVICE", "requestedat": window.LastAt.Add(offset)}), window, window.Until)
		if err != nil || matched != (offset < 0) {
			t.Fatalf("sample time %s vs request %s: matched=%v err=%v", window.LastAt, window.LastAt.Add(offset), matched, err)
		}
	}
}

func TestObservationRejectsInvalidSourcesWindowsAndRawInputs(t *testing.T) {
	definition, entity := observationDefinitionForTest()
	lookup := func(typ string) (EntityInfo, bool) { return entity, typ == entity.Type }
	if err := CheckObservationDefinition(definition, lookup); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*ObservationDefinition){
		"address":              func(d *ObservationDefinition) { d.Path = "https://example.test/summary" },
		"missing device":       func(d *ObservationDefinition) { d.DeviceField = "missing" },
		"foreign object owner": func(d *ObservationDefinition) { d.Object.App = "other" },
		"undeclared record": func(d *ObservationDefinition) {
			copy := *d.Decision
			copy.When = ExprReference("boolean", "record.undeclared")
			d.Decision = &copy
		},
		"raw set input": func(d *ObservationDefinition) {
			copy := *d.Decision
			copy.Inputs = map[string]Expr{"raw": ExprLiteralValue("set", []int{1, 2})}
			d.Decision = &copy
		},
		"unknown window": func(d *ObservationDefinition) {
			copy := *d.Decision
			copy.When = Expr{Op: ">", Kind: "boolean", Args: []Expr{{Op: "delta", Kind: "decimal", Window: &ExprWindowReference{Source: "arbitrary"}}, ExprLiteralValue("decimal", "0")}}
			d.Decision = &copy
		},
	} {
		t.Run(name, func(t *testing.T) {
			bad := definition
			change(&bad)
			if CheckObservationDefinition(bad, lookup) == nil {
				t.Fatal("invalid observation declaration accepted")
			}
		})
	}
	for name, change := range map[string]func(*ObservationWindow){
		"NaN": func(w *ObservationWindow) { w.Last = "NaN" }, "fraction": func(w *ObservationWindow) { w.Last = "1/3" }, "infinite exponent": func(w *ObservationWindow) { w.Last = "1e999999" }, "outside time": func(w *ObservationWindow) { w.LastAt = w.Until }, "reverse time": func(w *ObservationWindow) { w.LastAt = w.FirstAt.Add(-time.Second) }, "wrong range": func(w *ObservationWindow) { w.Min = "42" }, "wrong count": func(w *ObservationWindow) { w.Count = -1 }, "fake empty": func(w *ObservationWindow) { w.Count = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			window := observationWindowForTest()
			change(&window)
			if window.Check() == nil {
				t.Fatal("invalid external summary accepted")
			}
		})
	}
	window := observationWindowForTest()
	if CheckObservationPoints([]ObservationPoint{{At: window.FirstAt, Value: "1/3"}}, window, 1) == nil {
		t.Fatal("nondecimal point accepted")
	}
	if CheckObservationWindowRange(window, window.Since.Add(time.Second), window.Until, window.Unit) == nil {
		t.Fatal("different requested time window accepted")
	}
}

func TestObservationDescriptionSealsOnlyDeclaredResultCapabilities(t *testing.T) {
	definition, _ := observationDefinitionForTest()
	asset, err := WithDependencies(ReleaseAsset{Ref: AssetRef{App: "build", Kind: AssetObservation, Name: definition.Name}, SourceVersion: "1", ContractVersion: 1, Body: Raw(definition)})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(asset.Requires, []AssetRef{definition.Decision.Action, definition.Connection, definition.Object}) || len(asset.Provides) != 2 {
		t.Fatalf("observation description is incomplete: %+v", asset)
	}
	if asset.Provides[0].Side != CapabilityAuthoritative || asset.Provides[1].Side != CapabilityPure {
		t.Fatalf("observation read and decision effects conflated: %+v", asset.Provides)
	}
	if json.Unmarshal([]byte(`{"name":"forbidden","points":[{"at":"2026-10-15T00:00:00Z","value":"1"}]}`), new(ObservationDefinition)) == nil {
		t.Fatal("portable definition accepted raw observations")
	}
}
