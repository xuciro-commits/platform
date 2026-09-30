package flow

import (
	"encoding/json"
	"maps"
	"slices"
	"testing"
	"time"

	"platformkernel/kernel"
	"platformserver/internal/host"
	"platformserver/platform"
)

type scopeHost struct{ host.Host }

func (scopeHost) Automation(c platform.Caller, app string) platform.Caller { c.App = app; return c }

// A saved token/frame is the scheduler truth. Resuming does not reevaluate
// the collection or earlier node outputs, even when the input has changed.
func TestScopedLoopRestoresCursorAndOrderedOutputs(t *testing.T) {
	frozen := 0
	now := time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)
	steps := []platform.Step{
		{Name: "each", Next: "return", Loop: &platform.Loop{Body: "map", MaxIterations: 8, Concurrency: 2, Items: func(platform.Caller, *platform.Run) ([]json.RawMessage, *kernel.Error) {
			frozen++
			return []json.RawMessage{json.RawMessage(`"a"`), json.RawMessage(`"b"`), json.RawMessage(`"c"`)}, nil
		}}},
		{Name: "map", Next: "hold", Evaluate: &platform.Evaluate{Run: func(_ platform.Caller, r *platform.Run) (json.RawMessage, *kernel.Error) {
			return r.Frames[len(r.Frames)-1].Item, nil
		}}},
		{Name: "hold", Next: "finish", Wait: &platform.Wait{At: func(_ platform.Caller, r *platform.Run) time.Time { return r.Now.Add(time.Hour) }}},
		{Name: "finish", End: true},
		{Name: "return", End: true},
	}
	f := New("scopes")
	f.host = scopeHost{}
	d := &flowDef{app: "example", Flow: platform.Flow{Name: "scope", Title: "Scoped", Version: 1, Start: platform.Start{Manual: true}, Steps: steps}, steps: map[string]*platform.Step{}}
	for i := range d.Steps {
		d.steps[d.Steps[i].Name] = &d.Steps[i]
	}
	f.defs["example.scope"] = []*flowDef{d}
	ss := f.session(platform.Caller{}, now)
	x := ss.create(d, "run", "key", map[string]any{}, "member", "")
	ss.advance(x)
	if frozen != 1 || len(x.Tokens) != 3 || x.Tokens[0].Loop.Next != 2 {
		t.Fatalf("collection was not frozen and suspended: %+v", x)
	}
	saved, _ := json.Marshal(x)
	var recovered FlowInstance
	if err := json.Unmarshal(saved, &recovered); err != nil {
		t.Fatal(err)
	}
	resume := func(index int) {
		t.Helper()
		next := f.session(platform.Caller{}, now.Add(time.Hour))
		next.changed[recovered.ID] = &recovered
		i := slices.IndexFunc(recovered.Tokens, func(tok Token) bool {
			return len(tok.Frames) > 0 && tok.Frames[len(tok.Frames)-1].Index == index && tok.Waits == "wait"
		})
		if i < 0 {
			t.Fatalf("iteration %d is not pending: %+v", index, recovered.Tokens)
		}
		next.next(&recovered, recovered.Tokens[i].ID, "")
		next.advance(&recovered)
	}
	// Complete in physical order b, a, c. Logical output remains a, b, c.
	resume(1)
	resume(0)
	resume(2)
	if recovered.State != "done" || frozen != 1 {
		t.Fatalf("resume refroze work or did not finish: %+v", recovered)
	}
	var output struct {
		Items []map[string]json.RawMessage `json:"items"`
		Count int                          `json:"count"`
	}
	if json.Unmarshal(recovered.Outputs["each"], &output) != nil || output.Count != 3 || string(output.Items[0]["map"]) != `"a"` || string(output.Items[1]["map"]) != `"b"` || string(output.Items[2]["map"]) != `"c"` {
		t.Fatalf("iteration outputs lost scope/order: %s", recovered.Outputs["each"])
	}
}

func TestLongCollectionYieldsSavedTokensInsteadOfFailing(t *testing.T) {
	steps := []platform.Step{{Name: "each", Next: "end", Loop: &platform.Loop{Body: "item", MaxIterations: 2000, Concurrency: 1, Items: func(platform.Caller, *platform.Run) ([]json.RawMessage, *kernel.Error) {
		items := make([]json.RawMessage, 1200)
		for i := range items {
			items[i] = json.RawMessage(`true`)
		}
		return items, nil
	}}}, {Name: "item", End: true}, {Name: "end", End: true}}
	f := New("yield")
	f.host = scopeHost{}
	d := &flowDef{app: "example", Flow: platform.Flow{Name: "many", Title: "Many", Version: 1, Start: platform.Start{Manual: true}, Steps: steps}, steps: map[string]*platform.Step{}}
	for i := range d.Steps {
		d.steps[d.Steps[i].Name] = &d.Steps[i]
	}
	f.defs["example.many"] = []*flowDef{d}
	ss := f.session(platform.Caller{}, time.Now())
	x := ss.create(d, "run", "key", map[string]any{}, "member", "")
	ss.advance(x)
	if x.State == "stuck" || !slices.ContainsFunc(x.Tokens, func(tok Token) bool { return tok.Waits == "yield" }) {
		t.Fatalf("ordinary bounded work was classified as an infinite loop: %+v", x)
	}
	snapshot, _ := json.Marshal(x)
	var recovered FlowInstance
	json.Unmarshal(snapshot, &recovered)
	continuation := f.session(platform.Caller{}, time.Now())
	for i := range recovered.Tokens {
		if recovered.Tokens[i].Waits == "yield" {
			recovered.Tokens[i].Waits = "ready"
		}
	}
	continuation.advance(&recovered)
	if recovered.State != "done" {
		t.Fatalf("saved continuation did not finish: %+v", recovered)
	}
}

func TestParallelPathsKeepInputsAndJoinResultsByBranch(t *testing.T) {
	steps := []platform.Step{{Name: "fork", All: []string{"left", "right"}, Next: "end"}, {Name: "left", Evaluate: &platform.Evaluate{Run: func(_ platform.Caller, r *platform.Run) (json.RawMessage, *kernel.Error) {
		return r.Outputs["input"], nil
	}}}, {Name: "right", Evaluate: &platform.Evaluate{Run: func(_ platform.Caller, r *platform.Run) (json.RawMessage, *kernel.Error) {
		return r.Outputs["input"], nil
	}}}, {Name: "end", End: true}}
	f := New("parallel")
	f.host = scopeHost{}
	d := &flowDef{app: "example", Flow: platform.Flow{Name: "parallel", Title: "Parallel", Version: 1, Start: platform.Start{Manual: true}, Steps: steps}, steps: map[string]*platform.Step{}}
	for i := range d.Steps {
		d.steps[d.Steps[i].Name] = &d.Steps[i]
	}
	f.defs["example.parallel"] = []*flowDef{d}
	ss := f.session(platform.Caller{}, time.Now())
	x := ss.create(d, "run", "key", nil, "member", "")
	x.Tokens[0].Outputs = maps.Clone(map[string]json.RawMessage{"input": json.RawMessage(`{"ok":true}`)})
	ss.advance(x)
	var output map[string]map[string]json.RawMessage
	if x.State != "done" || json.Unmarshal(x.Outputs["fork"], &output) != nil || len(output) != 2 || string(output["left"]["left"]) != `{"ok":true}` || string(output["right"]["right"]) != `{"ok":true}` {
		t.Fatalf("fork discarded data or coalesced branch IDs: %s", x.Outputs["fork"])
	}
}

func TestWhileReturnsSavedStateToItsNextIteration(t *testing.T) {
	steps := []platform.Step{{Name: "while", Next: "done", Loop: &platform.Loop{Body: "increment", MaxIterations: 4, Concurrency: 1, Initial: func(platform.Caller, *platform.Run) (json.RawMessage, *kernel.Error) {
		return json.RawMessage(`{"n":0}`), nil
	}, While: func(_ platform.Caller, r *platform.Run) (bool, *kernel.Error) {
		var value struct {
			N int `json:"n"`
		}
		json.Unmarshal(r.Frames[len(r.Frames)-1].Item, &value)
		return value.N < 3, nil
	}}}, {Name: "increment", Next: "pause", Evaluate: &platform.Evaluate{Run: func(_ platform.Caller, r *platform.Run) (json.RawMessage, *kernel.Error) {
		var value struct {
			N int `json:"n"`
		}
		json.Unmarshal(r.Frames[len(r.Frames)-1].Item, &value)
		return platform.Raw(map[string]int{"n": value.N + 1}), nil
	}}}, {Name: "pause", Next: "return", Wait: &platform.Wait{At: func(_ platform.Caller, r *platform.Run) time.Time { return r.Now.Add(time.Hour) }}}, {Name: "return", End: true, Output: func(_ platform.Caller, r *platform.Run) (json.RawMessage, *kernel.Error) {
		return r.Outputs["increment"], nil
	}}, {Name: "done", End: true}}
	f := New("while")
	f.host = scopeHost{}
	d := &flowDef{app: "example", Flow: platform.Flow{Name: "while", Title: "While", Version: 1, Start: platform.Start{Manual: true}, Steps: steps}, steps: map[string]*platform.Step{}}
	for i := range d.Steps {
		d.steps[d.Steps[i].Name] = &d.Steps[i]
	}
	f.defs["example.while"] = []*flowDef{d}
	ss := f.session(platform.Caller{}, time.Now())
	x := ss.create(d, "run", "key", nil, "member", "")
	ss.advance(x)
	image, _ := json.Marshal(x)
	var restored FlowInstance
	json.Unmarshal(image, &restored)
	for index := 0; index < 3; index++ {
		i := slices.IndexFunc(restored.Tokens, func(tok Token) bool { return tok.Waits == "wait" })
		if i < 0 {
			t.Fatalf("while lost pending iteration %d", index)
		}
		next := f.session(platform.Caller{}, time.Now())
		next.next(&restored, restored.Tokens[i].ID, "")
		next.advance(&restored)
	}
	if restored.State != "done" {
		t.Fatalf("while did not terminate from its saved body return: %+v", restored)
	}
	var output struct {
		Count int `json:"count"`
	}
	json.Unmarshal(restored.Outputs["while"], &output)
	if output.Count != 3 {
		t.Fatalf("while reused wrong state: %s", restored.Outputs["while"])
	}
}

func TestStoredAIWaitAndParallelTokensDecodeToCurrentNativePositions(t *testing.T) {
	var instance FlowInstance
	if err := json.Unmarshal([]byte(`{"id":"run","flow":"build.review","version":1,"state":"waiting","data":"{\"infer\":\"CALL\"}","tokens":[{"id":1,"step":"parallel","waits":"join"},{"id":2,"step":"_function_infer","waits":"wait","branch":"parallel"}]}`), &instance); err != nil {
		t.Fatal(err)
	}
	token := instance.Tokens[1]
	if token.Parent != 1 || token.Step != "infer" || token.Waits != "invocation" || token.Child != "CALL" {
		t.Fatalf("stored wait did not map to current native token: %+v", token)
	}
}

func TestLoopControlAndFirstSuccessfulRaceUseExistingTokens(t *testing.T) {
	for _, control := range []string{"break", "continue"} {
		t.Run(control, func(t *testing.T) {
			steps := []platform.Step{{Name: "each", Next: "done", Loop: &platform.Loop{Body: "control", MaxIterations: 4, Concurrency: 2, Items: func(platform.Caller, *platform.Run) ([]json.RawMessage, *kernel.Error) {
				return []json.RawMessage{json.RawMessage(`1`), json.RawMessage(`2`), json.RawMessage(`3`)}, nil
			}}}, {Name: "control", LoopControl: control}, {Name: "done", End: true}}
			f := New(control)
			f.host = scopeHost{}
			d := &flowDef{app: "example", Flow: platform.Flow{Name: control, Title: control, Version: 1, Start: platform.Start{Manual: true}, Steps: steps}, steps: map[string]*platform.Step{}}
			for i := range d.Steps {
				d.steps[d.Steps[i].Name] = &d.Steps[i]
			}
			f.defs["example."+control] = []*flowDef{d}
			ss := f.session(platform.Caller{}, time.Now())
			x := ss.create(d, "run", "key", nil, "member", "")
			ss.advance(x)
			var result struct {
				Count   int  `json:"count"`
				Stopped bool `json:"stopped"`
			}
			json.Unmarshal(x.Outputs["each"], &result)
			if x.State != "done" || control == "break" && !result.Stopped || control == "continue" && result.Count != 3 {
				t.Fatalf("loop control escaped or abandoned native tokens: %+v %s", x, x.Outputs["each"])
			}
		})
	}
	steps := []platform.Step{{Name: "race", Any: []string{"refused", "success"}, Next: "done"}, {Name: "refused", NoRetry: true, Evaluate: &platform.Evaluate{Run: func(platform.Caller, *platform.Run) (json.RawMessage, *kernel.Error) {
		return nil, &kernel.Error{Message: "refused"}
	}}}, {Name: "success", Evaluate: &platform.Evaluate{Run: func(platform.Caller, *platform.Run) (json.RawMessage, *kernel.Error) {
		return json.RawMessage(`{"ok":true}`), nil
	}}}, {Name: "done", End: true}}
	f := New("race")
	f.host = scopeHost{}
	d := &flowDef{app: "example", Flow: platform.Flow{Name: "race", Title: "Race", Version: 1, Start: platform.Start{Manual: true}, Steps: steps}, steps: map[string]*platform.Step{}}
	for i := range d.Steps {
		d.steps[d.Steps[i].Name] = &d.Steps[i]
	}
	f.defs["example.race"] = []*flowDef{d}
	ss := f.session(platform.Caller{}, time.Now())
	x := ss.create(d, "run", "key", nil, "member", "")
	ss.advance(x)
	var result map[string]json.RawMessage
	json.Unmarshal(x.Outputs["race"], &result)
	if x.State != "done" || result["success"] == nil {
		t.Fatalf("a refused candidate won the first-success race: %+v", x)
	}
}
