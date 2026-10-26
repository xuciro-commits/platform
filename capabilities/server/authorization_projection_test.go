package platformserver

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"platformkernel/kernel"
	"platformserver/internal/construction"
	"platformserver/platform"
)

type projectionOwner struct {
	*notes
	result   any
	original string
}

func (p *projectionOwner) Manifest() platform.Manifest {
	m := p.notes.Manifest()
	m.ReadSources = nil
	if p.original != "" {
		m.ReadSources = map[string]string{p.id + "-notes": p.original}
	}
	return m
}

func (p *projectionOwner) Read(platform.Caller, string) (any, *kernel.Error) {
	return p.result, nil
}

type sourceProjection struct {
	ID         string          `json:"id" source:"record" from:"Source"`
	Name       string          `json:"name" source:"record" from:"Source#name"`
	Secret     string          `json:"secret" source:"generated" from:"Source#secret"`
	Body       json.RawMessage `json:"body" source:"generated" from:"Sources"`
	Missing    string          `json:"missing" source:"generated"`
	Original   string          `json:"original" source:"user-input"`
	Undeclared string          `json:"undeclared"`
	Withheld   bool            `json:"withheld,omitempty"`
	Source     string          `json:"-"`
	Sources    []string        `json:"-"`
}

func TestDerivedDTORechecksSourceRowsFieldsAndRevokedGrants(t *testing.T) {
	owner := &projectionOwner{notes: newNotes("projection", "projection")}
	tn := composeTenant(t, "projection", []Seat{seatOf("ana", "stock:lead", "projection:writer"), seatOf("bo", "stock:clerk", "projection:writer")}, newStock("projection"), owner)
	decide(t, tn, "ana", "stock", "stock.item.create", "stock.item", "I1", map[string]any{"name": "Machine", "secret": "private tolerance"}, txnAt)
	dir := tn.app(PlatformApp).(*Console)
	dir.mu.Lock()
	dir.members["ana"].Roles = map[string]string{"projection": "writer", "stock": "clerk"}
	dir.members["ana"].Grants = nil
	dir.mu.Unlock()
	report := sourceProjection{ID: "I1", Name: "Machine", Secret: "private tolerance", Body: json.RawMessage(`{"name":"Machine","count":9007199254740993}`), Missing: "untracked generation", Original: "member input", Undeclared: "untracked prose", Source: "stock.item/I1", Sources: []string{"stock.item/I1#name"}}
	owner.result = report
	before := platform.Raw(report)
	stale := memberOf(t, tn, "ana")
	read := func(m platform.Member) sourceProjection {
		t.Helper()
		value, err := tn.Read(m, "projection-notes")
		if err != nil {
			t.Fatal(err)
		}
		return value.(sourceProjection)
	}
	visible := read(stale)
	if visible.Name != "Machine" || !bytes.Equal(visible.Body, report.Body) || visible.Original != report.Original || visible.Secret != "" || visible.Missing != "" || visible.Undeclared != "" || !visible.Withheld {
		t.Fatalf("DTO did not preserve authorized content and narrow the rest: %+v", visible)
	}
	refs := tn.refsIn(visible)
	if !strings.Contains(strings.Join(refs, ","), "stock.item/I1#name") || strings.Contains(strings.Join(refs, ","), "#secret") {
		t.Fatal("downstream derivation lost visible DTO field provenance or retained hidden content")
	}
	hidden := read(memberOf(t, tn, "bo"))
	if hidden.ID != "" || hidden.Name != "" || len(hidden.Body) != 0 || !hidden.Withheld {
		t.Fatal("field projection escaped its source row")
	}
	owner.result = []sourceProjection{report}
	value, err := tn.Read(memberOf(t, tn, "bo"), "projection-notes")
	if err != nil || len(value.([]sourceProjection)) != 0 {
		t.Fatal("DTO collection retained an invisible source element")
	}
	owner.result = report
	dir.mu.Lock()
	dir.members["ana"].Roles = map[string]string{"projection": "writer"}
	dir.members["ana"].Grants = nil
	dir.mu.Unlock()
	revoked := read(stale)
	if revoked.Name != "" || len(revoked.Body) != 0 || !revoked.Withheld {
		t.Fatal("retained DTO used stale source authority")
	}
	if !bytes.Equal(before, platform.Raw(owner.result)) {
		t.Fatal("read-time closure changed original DTO bytes")
	}
	row, ok := platform.Get[Item](tn.automation("stock", false), "I1")
	if !ok || row.Name != "Machine" || row.Secret != "private tolerance" {
		t.Fatal("source projection rewrote the accepted record")
	}
}

func TestUndeclaredDTOAndJSONContainersFailClosed(t *testing.T) {
	tn := stockTenant(t)
	type labels []string
	type containers struct {
		Text     string          `json:"text"`
		Pointer  *string         `json:"pointer"`
		Array    [2]string       `json:"array"`
		Labels   labels          `json:"labels"`
		Map      map[string]any  `json:"map"`
		Raw      json.RawMessage `json:"raw"`
		Bytes    []byte          `json:"bytes"`
		Invalid  json.RawMessage `json:"invalid"`
		Original json.RawMessage `json:"original" source:"external-input"`
		Withheld bool            `json:"withheld,omitempty"`
	}
	secret := "private prose"
	input := containers{Text: secret, Pointer: &secret, Array: [2]string{secret, secret}, Labels: labels{secret}, Map: map[string]any{secret: secret}, Raw: json.RawMessage(`{"private prose":"private prose"}`), Bytes: []byte(secret), Invalid: json.RawMessage(`{"duplicate":1,"duplicate":2}`), Original: json.RawMessage(`{"device":"sensor","reading":9007199254740993}`)}
	view := tn.narrowed(memberOf(t, tn, "ana"), input, txnAt, false).(containers)
	if strings.Contains(string(platform.Raw(view)), secret) || view.Pointer == nil || *view.Pointer != "" || view.Array != [2]string{} || len(view.Labels) != 1 || view.Labels[0] != "" || len(view.Map) != 0 || string(view.Raw) != "{}" || len(view.Bytes) != 0 || string(view.Invalid) != "null" || !view.Withheld {
		t.Fatalf("undeclared container leaked or lost its Go shape: %+v", view)
	}
	if !bytes.Equal(view.Original, input.Original) || input.Text != secret || *input.Pointer != secret || len(input.Map) != 1 || string(input.Raw) != `{"private prose":"private prose"}` {
		t.Fatal("projection changed original bytes or numeric precision")
	}
	if tn.narrowed(memberOf(t, tn, "ana"), secret, txnAt, false) != "" {
		t.Fatal("bare string escaped the same declaration rule")
	}
}

type opaqueRead struct{ private string }

func (o opaqueRead) MarshalJSON() ([]byte, error) { return json.Marshal(o.private) }

func TestUndeclaredOpaqueSerializationAndCyclesCannotEscapeProjection(t *testing.T) {
	tn := stockTenant(t)
	m := memberOf(t, tn, "ana")
	if tn.narrowed(m, opaqueRead{private: "serializer prose"}, txnAt, false) != nil {
		t.Fatal("custom serializer bypassed the exported-field projection")
	}
	type node struct {
		Next any    `json:"next"`
		Text string `json:"text" source:"user-input"`
	}
	n := &node{Text: "original input"}
	n.Next = n
	view := tn.narrowed(m, n, txnAt, false).(*node)
	if view.Next.(*node) != nil || n.Next != n || view.Text != n.Text {
		t.Fatal("cyclic DTO did not terminate on a private projection")
	}
}

func TestMissingSourcesWithholdWholeListAndCombinedDerivations(t *testing.T) {
	tn := stockTenant(t)
	type step struct {
		Sources []string `json:"sources"`
		Text    string   `json:"text" source:"generated"`
	}
	type report struct {
		platform.Record
		Sources   []string `json:"sources"`
		Text      string   `json:"text" source:"generated"`
		Original  string   `json:"original" source:"user-input"`
		Steps     []step   `json:"steps"`
		Citations []step   `json:"citations"`
		Summary   string   `json:"summary" source:"generated"`
		Withheld  bool     `json:"withheld"`
	}
	info, err := platform.Describe("stock", platform.Entity{Type: "stock.source-report", Model: report{}, Derived: []platform.Derivation{
		{From: "sources", Fields: []string{"text", "original"}},
		{From: "steps.sources", Fields: []string{"text"}},
		{From: "citations.sources", Element: true},
		{From: "*", Fields: []string{"summary"}},
	}, Withheld: "withheld"}, func(reflect.Type) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	tn.records.byGo[info.Go] = &entityType{info: info}
	input := report{Text: "generation without a source", Original: "original input", Steps: []step{{Text: "untracked step"}}, Citations: []step{{Text: "untracked citation"}}, Summary: "untracked summary"}
	before := platform.Raw(input)
	view := tn.narrowed(memberOf(t, tn, "ana"), input, txnAt, false).(report)
	if view.Text != "" || view.Summary != "" || len(view.Steps) != 1 || view.Steps[0].Text != "" || len(view.Citations) != 0 || !view.Withheld {
		t.Fatalf("empty provenance widened a declared derivation: %+v", view)
	}
	// A derivation's missing source does not erase explicitly original input.
	// It still narrows every field when an actual source is present but denied.
	if view.Original != input.Original || !bytes.Equal(before, platform.Raw(input)) {
		t.Fatal("source closure changed original input or saved bytes")
	}
}

func TestOriginalReadSourceDoesNotWidenRecordsOrExplicitDerivedFields(t *testing.T) {
	owner := &projectionOwner{notes: newNotes("projection", "projection"), original: "record"}
	tn := composeTenant(t, "projection", []Seat{seatOf("ana", "stock:clerk", "projection:writer"), seatOf("bo", "stock:clerk", "projection:writer")}, newStock("projection"), owner)
	for _, id := range []string{"ana", "bo"} {
		decide(t, tn, id, "stock", "stock.item.create", "stock.item", id, map[string]any{"name": id, "note": "undeclared record prose"}, txnAt)
	}
	rows, _, err := platform.Find[Item](tn.automation("stock", false), platform.Query{})
	if err != nil {
		t.Fatal(err)
	}
	type report struct {
		Caption   string `json:"caption"`
		Rows      []Item `json:"rows"`
		Generated string `json:"generated" source:"generated"`
		Withheld  bool   `json:"withheld,omitempty"`
	}
	owner.result = report{Caption: "owner's original configuration", Rows: rows, Generated: "missing provenance"}
	out, err := tn.Read(memberOf(t, tn, "ana"), "projection-notes")
	if err != nil {
		t.Fatal(err)
	}
	view := out.(report)
	if view.Caption == "" || len(view.Rows) != 1 || view.Rows[0].ID != "ana" || view.Rows[0].Note != "" || view.Generated != "" || !view.Withheld {
		t.Fatalf("original declaration widened nested authority: %+v", view)
	}
	for _, source := range []string{"generated", "invented"} {
		owner.original = source
		if construction.CheckManifest(owner.Manifest(), false) == nil {
			t.Fatal("uncontrolled read source was accepted")
		}
	}
}
