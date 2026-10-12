package platform

import (
	"strings"
	"testing"
)

// The CRM's group stay (accessors/crm.go SchemaPlan): the same declarations a
// form and the host both read.
func stayFields() []Field {
	minimum, maximum := 1.0, 20.0
	return []Field{
		{Name: "rooms", Type: "integer", Required: true, Constraints: &InputConstraints{Min: &minimum, Max: &maximum}},
		{Name: "arrive", Type: "date", Required: true, Constraints: &InputConstraints{}, Range: &DateRange{End: "depart"}},
		{Name: "depart", Type: "date", Required: true, Constraints: &InputConstraints{After: "arrive"}},
		{Name: "cutoff", Type: "date", Required: true, Constraints: &InputConstraints{Before: "arrive"}},
	}
}

func stayValues() map[string]any {
	return map[string]any{"rooms": 12.0, "arrive": "2026-11-02", "depart": "2026-11-05", "cutoff": "2026-10-30"}
}

func issueOn(issues []FieldIssue, path, code string) (FieldIssue, bool) {
	for _, issue := range issues {
		if issue.Code == code && len(issue.Path) == 1 && issue.Path[0] == path {
			return issue, true
		}
	}
	return FieldIssue{}, false
}

// A declaration is checked before it reaches a form or the host: unknown or
// mistyped endpoints, empty bounds, repeated range ends and cycles are refused
// with the field that owns the mistake.
func TestInputDeclarationsAreCheckedBeforeUse(t *testing.T) {
	if err := CheckInputs(stayFields()); err != nil {
		t.Fatalf("the stay declarations were refused: %v", err)
	}
	minimum, maximum, low, days := 1.0, 20.0, 2, 3
	bad := []struct {
		name   string
		fields []Field
		expect string
	}{
		{"duplicate", []Field{{Name: "a"}, {Name: "a"}}, "duplicate input"},
		{"bounds on text", []Field{{Name: "title", Type: "string", Constraints: &InputConstraints{Min: &minimum}}}, "nonnumeric bounds"},
		{"empty bounds", []Field{{Name: "rooms", Type: "integer", Constraints: &InputConstraints{Min: &maximum, Max: &minimum}}}, "empty bounds"},
		{"exclusive bound without one", []Field{{Name: "rooms", Type: "integer", Constraints: &InputConstraints{ExclusiveMin: true}}}, "empty bounds"},
		{"length on a number", []Field{{Name: "rooms", Type: "integer", Constraints: &InputConstraints{MinLength: &low}}}, "nontext length"},
		{"empty length bounds", []Field{{Name: "title", Type: "string", Constraints: &InputConstraints{MinLength: &days, MaxLength: &low}}}, "empty length bounds"},
		{"unknown comparison", []Field{{Name: "cutoff", Type: "date", Constraints: &InputConstraints{Before: "missing"}}}, "invalid comparison"},
		{"self comparison", []Field{{Name: "cutoff", Type: "date", Constraints: &InputConstraints{Before: "cutoff"}}}, "invalid comparison"},
		{"mistyped comparison", []Field{{Name: "cutoff", Type: "date", Constraints: &InputConstraints{Before: "rooms"}}, {Name: "rooms", Type: "integer"}}, "invalid comparison"},
		{"cyclic dependencies", []Field{{Name: "a", Type: "date", Constraints: &InputConstraints{Before: "b"}}, {Name: "b", Type: "date", Constraints: &InputConstraints{After: "a"}}}, "cyclic input dependencies"},
		{"range on a datetime", []Field{{Name: "at", Type: "datetime", Range: &DateRange{End: "until"}}, {Name: "until", Type: "date"}}, "invalid date range"},
		{"range onto a non-date", []Field{{Name: "arrive", Type: "date", Range: &DateRange{End: "rooms"}}, {Name: "rooms", Type: "integer"}}, "invalid date range"},
		{"repeated range end", []Field{{Name: "arrive", Type: "date", Range: &DateRange{End: "depart"}}, {Name: "depart", Type: "date"}, {Name: "board", Type: "date", Range: &DateRange{End: "depart"}}}, "repeats a date range end"},
		{"datetime flag on a date pair of strings", []Field{{Name: "hotel", Type: "string", Constraints: &InputConstraints{DateTime: true}}}, "does not take a lodging date"},
	}
	for _, c := range bad {
		err := CheckInputs(c.fields)
		if err == nil || !strings.Contains(err.Error(), c.expect) {
			t.Fatalf("%s: want %q, got %v", c.name, c.expect, err)
		}
	}
}

// The submitted values carry the field that holds the value, the relation that
// failed and why; relation checks wait for both endpoints, and an unannotated
// owner field keeps its own check.
func TestInputIssuesLocateAndRelateValues(t *testing.T) {
	if issues := InputIssues(stayFields(), stayValues()); len(issues) != 0 {
		t.Fatalf("a valid stay was refused: %+v", issues)
	}
	over := stayValues()
	over["rooms"] = 25.0
	issue, ok := issueOn(InputIssues(stayFields(), over), "rooms", "max")
	if !ok || issue.Message != "Value must be at most 20." {
		t.Fatalf("rooms over the declared maximum: %+v", issue)
	}
	part, ok := issueOn(InputIssues(stayFields(), map[string]any{"rooms": 12.5, "arrive": "2026-11-02", "depart": "2026-11-05", "cutoff": "2026-10-30"}), "rooms", "type")
	if !ok || part.Message != "Enter a valid value." {
		t.Fatalf("a fractional room count: %+v", part)
	}
	late := stayValues()
	late["cutoff"] = "2026-11-03"
	issue, ok = issueOn(InputIssues(stayFields(), late), "cutoff", "relation")
	if !ok || issue.Message != "Value must be before arrive." || len(issue.RelatedPaths) != 1 || issue.RelatedPaths[0][0] != "arrive" {
		t.Fatalf("the cutoff after arrival: %+v", issue)
	}
	same := stayValues()
	same["cutoff"] = "2026-11-02"
	if _, ok := issueOn(InputIssues(stayFields(), same), "cutoff", "relation"); !ok {
		t.Fatal("an equal endpoint was accepted without Inclusive")
	}
	inclusive := stayFields()
	inclusive[3].Constraints.Inclusive = true
	if issues := InputIssues(inclusive, same); len(issues) != 0 {
		t.Fatalf("an inclusive endpoint was refused: %+v", issues)
	}
	// The relation waits: one unparsable endpoint reports itself, not the pair.
	broken := stayValues()
	broken["arrive"] = "2026-11-31"
	issues := InputIssues(stayFields(), broken)
	if _, ok := issueOn(issues, "arrive", "type"); !ok {
		t.Fatalf("an impossible date was accepted: %+v", issues)
	}
	if _, ok := issueOn(issues, "cutoff", "relation"); ok {
		t.Fatalf("a relation was checked against an unparsable endpoint: %+v", issues)
	}
	// Empty, 0 and false are different: a required field needs a value, a
	// submitted 0 keeps the declared minimum, and an optional field may be empty.
	required := InputIssues([]Field{{Name: "title", Type: "string", Required: true, Constraints: &InputConstraints{}}, {Name: "note", Type: "string", Constraints: &InputConstraints{}}}, map[string]any{"title": "   "})
	if _, ok := issueOn(required, "title", "required"); !ok {
		t.Fatalf("an empty required field: %+v", required)
	}
	if _, ok := issueOn(InputIssues([]Field{{Name: "note", Type: "string", Required: true, Constraints: &InputConstraints{}}}, map[string]any{"note": "kept"}), "note", "required"); ok {
		t.Fatal("a filled field was reported empty")
	}
	minimum := 1.0
	zero := InputIssues([]Field{{Name: "rooms", Type: "integer", Constraints: &InputConstraints{Min: &minimum}}}, map[string]any{"rooms": 0.0})
	if issue, ok := issueOn(zero, "rooms", "min"); !ok || issue.Message != "Value must be at least 1." {
		t.Fatalf("a submitted zero: %+v", zero)
	}
	if issues := InputIssues([]Field{{Name: "rooms", Type: "integer"}}, map[string]any{}); len(issues) != 0 {
		t.Fatalf("an owner field without declarations got a host issue: %+v", issues)
	}
	// Text lengths are UTF-16 units, as the original build.Input contract is.
	short, long := 3, 4
	lengths := []Field{{Name: "title", Type: "string", Constraints: &InputConstraints{MinLength: &short, MaxLength: &long}}}
	if issues := InputIssues(lengths, map[string]any{"title": "ab"}); len(issues) != 1 || issues[0].Code != "minLength" {
		t.Fatalf("short text: %+v", issues)
	}
	if issues := InputIssues(lengths, map[string]any{"title": "abcde"}); len(issues) != 1 || issues[0].Code != "maxLength" {
		t.Fatalf("long text: %+v", issues)
	}
	three := 3
	units := []Field{{Name: "title", Type: "string", Constraints: &InputConstraints{MaxLength: &three}}}
	if issues := InputIssues(units, map[string]any{"title": "🙂🙂"}); len(issues) != 1 || issues[0].Code != "maxLength" {
		t.Fatalf("four UTF-16 units in two runes: %+v", issues)
	}
	if issues := InputIssues(units, map[string]any{"title": "🙂a"}); len(issues) != 0 {
		t.Fatalf("three UTF-16 units: %+v", issues)
	}
	if issues := InputIssues([]Field{{Name: "kind", Type: "string", Choices: []string{"company", "person"}, Constraints: &InputConstraints{}}}, map[string]any{"kind": "charity"}); len(issues) != 1 || issues[0].Code != "choice" {
		t.Fatalf("an unlisted choice: %+v", issues)
	}
	for _, bad := range []string{"2026-2-01", "2026-11-31", "0000-01-01", "2026-11-02T00:00:00Z"} {
		if _, ok := issueOn(InputIssues(stayFields(), map[string]any{"arrive": bad, "depart": "2026-11-05", "cutoff": "2026-10-30", "rooms": 1.0}), "arrive", "type"); !ok {
			t.Fatalf("%q was accepted as a civil date", bad)
		}
	}
	instants := []Field{{Name: "at", Type: "datetime", Required: true, Constraints: &InputConstraints{}}}
	if issues := InputIssues(instants, map[string]any{"at": "2026-11-02T10:00:00+02:00"}); len(issues) != 0 {
		t.Fatalf("an offset instant was refused: %+v", issues)
	}
	for _, bad := range []string{"2026-11-02T10:00:00", "2026-11-02T10:00:00-00:00", "2026-11-02"} {
		if issues := InputIssues(instants, map[string]any{"at": bad}); len(issues) != 1 || issues[0].Code != "type" {
			t.Fatalf("%q was accepted as an instant: %+v", bad, issues)
		}
	}
	// The declared pair holds numbers too, and its endpoints are compared only
	// when both were submitted.
	minimum, maximum := 1.0, 10.0
	band := []Field{{Name: "low", Type: "number", Constraints: &InputConstraints{Min: &minimum}}, {Name: "high", Type: "number", Constraints: &InputConstraints{Min: &maximum, After: "low"}}}
	if issues := InputIssues(band, map[string]any{"high": 2.0}); len(issues) != 1 || issues[0].Code != "min" {
		t.Fatalf("one endpoint alone: %+v", issues)
	}
}
