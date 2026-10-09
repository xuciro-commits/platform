package enterprise

import (
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

// queryFixture is a small model across the window rules the query contract
// tests: live, closed, future, two stereotypes, a kind and a name to find.
func queryFixture() *Enterprise {
	return &Enterprise{model: Model{Elements: []Element{
		{ID: "loc-workshop", Stereotype: Location, Name: "Assembly workshop", Kind: "workshop", From: "2026-01-01"},
		{ID: "loc-line", Stereotype: Location, Name: "Line 1", Kind: "line", From: "2026-01-01"},
		{ID: "loc-old", Stereotype: Location, Name: "Old press hall", Kind: "workshop", From: "2026-01-01", Until: "2026-06-01", Closed: "merged"},
		{ID: "loc-future", Stereotype: Location, Name: "Annex", Kind: "workshop", From: "2027-01-01"},
		{ID: "org-acme", Stereotype: Organization, Name: "Acme Manufacturing", Kind: "plant", From: "2026-01-01"},
	}}}
}

func idsOf(r EnterpriseQueryResult) []string {
	out := make([]string, len(r.Elements))
	for i, e := range r.Elements {
		out[i] = e.ID
	}
	return out
}

func TestQuery(t *testing.T) {
	e := queryFixture()
	now := time.Date(2026, 10, 9, 17, 0, 0, 0, time.UTC)
	joined := func(r EnterpriseQueryResult, err *kernel.Error) string {
		if err != nil {
			t.Fatal(err)
		}
		return strings.Join(idsOf(r), ",")
	}
	// Default: today's live set — no closed, no future, sorted by id.
	if got := joined(e.Query(nil, "", "", "", 200, now)); got != "loc-line,loc-workshop,org-acme" {
		t.Errorf("live set = %q", got)
	}
	// Stereotype OR-filter.
	if got := joined(e.Query([]string{"ActualLocation"}, "", "", "", 200, now)); got != "loc-line,loc-workshop" {
		t.Errorf("locations = %q", got)
	}
	if got := joined(e.Query([]string{" ActualLocation ", "ActualOrganization"}, "", "", "", 200, now)); got != "loc-line,loc-workshop,org-acme" {
		t.Errorf("trimmed OR-filter = %q", got)
	}
	// Exact kind, case-insensitive words.
	if got := joined(e.Query(nil, "workshop", "", "", 200, now)); got != "loc-workshop" {
		t.Errorf("kind = %q", got)
	}
	if got := joined(e.Query(nil, "", "ACME", "", 200, now)); got != "org-acme" {
		t.Errorf("q = %q", got)
	}
	// alive=all opens the window; an explicit day reads that day.
	if got := joined(e.Query(nil, "", "", "all", 200, now)); got != "loc-future,loc-line,loc-old,loc-workshop,org-acme" {
		t.Errorf("all = %q", got)
	}
	if got := joined(e.Query(nil, "", "", "2026-03-15", 200, now)); got != "loc-line,loc-old,loc-workshop,org-acme" {
		t.Errorf("day = %q", got)
	}
	// The bounded page keeps the id order.
	if got := joined(e.Query(nil, "", "", "", 1, now)); got != "loc-line" {
		t.Errorf("limit = %q", got)
	}
	if _, err := e.Query(nil, "", "", "yesterday", 200, now); err == nil || err.Code != pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT {
		t.Errorf("alive=yesterday → %v, want INVALID_ARGUMENT", err)
	}
}

func TestResolve(t *testing.T) {
	e := queryFixture()
	now := time.Date(2026, 10, 9, 17, 0, 0, 0, time.UTC)
	r, err := e.Resolve([]string{"loc-old", "missing", "", "org-acme"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Elements) != 2 || r.Elements[0].ID != "loc-old" || r.Elements[1].ID != "org-acme" {
		t.Fatalf("resolve = %+v", r.Elements)
	}
	if !r.Elements[0].Closed || !strings.Contains(r.Elements[0].Until, "2026-06-01") {
		t.Errorf("closed element must resolve with its window: %+v", r.Elements[0])
	}
	if r.Elements[1].Closed {
		t.Errorf("live element marked closed: %+v", r.Elements[1])
	}
	many := make([]string, 101)
	if _, err := e.Resolve(many, now); err == nil || err.Code != pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT {
		t.Errorf("101 ids → %v, want INVALID_ARGUMENT", err)
	}
}

// TestSDK pins the generated file's contract shape: the unions, the write
// list, one payload type per write, the metamodel-specialized fields, and the
// read helpers. The staleness of the file itself is TestAPIContract's job.
func TestSDK(t *testing.T) {
	got := SDK()
	if got != SDK() {
		t.Fatal("SDK() is not deterministic")
	}
	for _, want := range []string{
		`export type EnterpriseStereotype =`,
		`	| "ActualLocation"`,
		`	| "InformationElement"`,
		`	| "organization"`,
		`	| "control"`,
		`export const enterpriseWrites = [`,
		`	"enterprise.element.add",`,
		`	"enterprise.relationship.change",`,
		`export type EnterpriseWrite = (typeof enterpriseWrites)[number];`,
		`stereotype: EnterpriseStereotype;`,
		`viewpoint: EnterpriseViewpoint;`,
		`until: string;`,
		`export type EnterpriseWritePayloads = {`,
		`export function enterpriseWrite<K extends EnterpriseWrite>(`,
		`export async function queryElements(get: EnterpriseGet, filters: EnterpriseQueryFilters = {}): Promise<EnterpriseQueryResult> {`,
		`export async function resolveElements(get: EnterpriseGet, ids: readonly string[]): Promise<EnterpriseQueryResult> {`,
		`return get<EnterpriseQueryResult>(query === "" ? "/v1/enterprise-query" : "/v1/enterprise-query?" + query);`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("SDK() is missing %q", want)
		}
	}
	// One payload type per declared write, and required fields typed required:
	// element.close's "until" is required, relationship.end's is optional.
	for _, a := range declarations() {
		if !strings.Contains(got, "export type "+payloadType(a.Schema)+" = {\n") {
			t.Errorf("SDK() has no payload for %s", a.Schema)
		}
	}
	if !strings.Contains(got, "export type EnterpriseElementClosePayload = {\n\t// Last day + 1\n\tuntil: string;") {
		t.Error("element.close's required until is not typed required")
	}
	if !strings.Contains(got, "export type EnterpriseRelationshipEndPayload = {\n\t// Valid until, exclusive (empty: open)\n\tuntil?: string;") {
		t.Error("relationship.end's optional until is not typed optional")
	}
}
