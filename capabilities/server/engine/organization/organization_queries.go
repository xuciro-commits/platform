package organization

import (
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/enterprise/uaf"
	"platformserver/platform"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	ID              = "enterprise"
	ModelType       = "enterprise.model"
	SchemaSliceSync = "enterprise.slice.sync"
)

type Metamodel struct {
	Version      string                      `json:"version"`
	URI          string                      `json:"uri"`
	Domains      []string                    `json:"domains"`
	Profile      []ProfileEntry              `json:"profile"`
	Stereotypes  map[string]*uaf.Stereotype  `json:"stereotypes"`
	Enumerations map[string]*uaf.Enumeration `json:"enumerations"`
	Views        []Viewpoint                 `json:"views"`
	// Contracts are the relationship stereotypes as the host checks them
	//what each end accepts, the standard's text it comes from,
	// and what this profile adds. The modeler offers exactly these.
	Contracts []Contract `json:"contracts"`
}

type EnterpriseQueryElement struct {
	ID         string            `json:"id"`
	Stereotype string            `json:"stereotype"`
	Name       string            `json:"name"`
	Kind       string            `json:"kind,omitempty"`
	ShortName  string            `json:"shortName,omitempty"`
	Legal      bool              `json:"legal,omitempty"`
	From       platform.Date     `json:"from,omitempty"`
	Until      platform.Date     `json:"until,omitempty"`
	Periods    []platform.Period `json:"periods,omitempty"`
	// Closed marks an element that is no longer live today (ended, merged,
	// decommissioned): resolve returns it so a stored reference still reads.
	Closed bool `json:"closed,omitempty"`
}

type EnterpriseQueryResult struct {
	Elements []EnterpriseQueryElement `json:"elements"`
}

// The query and resolve halves of the ref/query contracts:
// typed reads over the one model, projected to what a consumer needs — never
// the whole graph. The host serves them as GET /v1/enterprise-query and
// /v1/enterprise-resolve; the generated SDK (SDK()) wraps both.

// EnterpriseQueryElement is one element as the query contract projects it.
// EnterpriseQueryResult is the answer both reads share.
func project(e Element, today platform.Date) EnterpriseQueryElement {
	return EnterpriseQueryElement{ID: e.ID, Stereotype: e.Stereotype, Name: e.Name, Kind: e.Kind, ShortName: e.ShortName,
		Legal: e.Legal, From: e.From, Until: e.Until, Periods: slices.Clone(e.Periods), Closed: !e.ActiveOn(today)}
}

// Query answers the typed read: stereotypes is an OR-filter
// (empty: every stereotype), kind an exact match, q a case-insensitive
// substring of id/name/shortName/kind, alive a day whose live set to take
// ("all": no window, closed and future included), limit a bounded page (the
// host caps it before calling). Results sort by id: a picker's order must not
// depend on map iteration.
func (e *Enterprise) Query(stereotypes []string, kind, q, alive string, limit int, now time.Time) (EnterpriseQueryResult, *kernel.Error) {
	today := platform.Date(now.UTC().Format(time.DateOnly))
	if alive != "" && alive != "all" {
		if len(alive) != len("2006-01-02") {
			return EnterpriseQueryResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "alive is \"all\" or a YYYY-MM-DD day")
		}
		today = platform.Date(alive)
	}
	keep := map[string]bool{}
	for _, s := range stereotypes {
		if s = strings.TrimSpace(s); s != "" {
			keep[s] = true
		}
	}
	needle := strings.ToLower(strings.TrimSpace(q))
	e.mu.Lock()
	defer e.mu.Unlock()
	matches := []Element{}
	for _, el := range e.model.Elements {
		if el.Deleted {
			continue
		}
		if len(keep) > 0 && !keep[el.Stereotype] || kind != "" && el.Kind != kind {
			continue
		}
		if alive != "all" && !el.ActiveOn(today) {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(el.ID+" "+el.Name+" "+el.ShortName+" "+el.Kind), needle) {
			continue
		}
		matches = append(matches, el)
	}
	slices.SortFunc(matches, func(a, b Element) int { return strings.Compare(a.ID, b.ID) })
	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
	}
	out := EnterpriseQueryResult{Elements: make([]EnterpriseQueryElement, len(matches))}
	for i, el := range matches {
		out.Elements[i] = project(el, today)
	}
	return out, nil
}

// Resolve answers the ref contract's read side: the projection
// of each named id, closed elements included and marked, unknown ids skipped —
// a stored reference always reads. At most 100 ids per call keep the answer
// bounded; empty resolves to nothing.
func (e *Enterprise) Resolve(ids []string, now time.Time) (EnterpriseQueryResult, *kernel.Error) {
	if len(ids) > 100 {
		return EnterpriseQueryResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "resolve takes at most 100 ids")
	}
	today := platform.Date(now.UTC().Format(time.DateOnly))
	e.mu.Lock()
	defer e.mu.Unlock()
	out := EnterpriseQueryResult{Elements: []EnterpriseQueryElement{}}
	for _, id := range ids {
		if id == "" {
			continue
		}
		if el := e.model.ElementOf(id); el != nil && !el.Deleted {
			out.Elements = append(out.Elements, project(*el, today))
		}
	}
	return out, nil
}

// The typed SDK over this app's declarations: the same pipeline
// that generates the host's TypeScript (cmd/api-types) emits this file into
// web/packages/kernel/src/gen/enterprise-sdk.ts, and TestAPIContract fails
// while it is stale. Nothing here is written by hand — Profile(), Views() and
// declarations() are the sources, so the types cannot drift from the contract.

// SDK is the generated enterprise-sdk.ts source.
func SDK() string {
	var b strings.Builder
	b.WriteString("// Generated by capabilities/server/cmd/api-types from the enterprise app's own declarations. Do not edit:\n")
	b.WriteString("// change the Go declarations (profile, views, actions), then run `go run ./cmd/api-types` in capabilities/server.\n")
	b.WriteString("import type { EnterpriseQueryResult } from \"./host\";\n\n")

	b.WriteString("// EnterpriseStereotype is every stereotype the Enterprise Core Profile offers the palette.\n")
	b.WriteString("export type EnterpriseStereotype =\n")
	for _, p := range Profile() {
		b.WriteString("\t| " + strconv.Quote(p.Stereotype) + "\n")
	}
	b.WriteString(";\n\n")

	b.WriteString("// EnterpriseViewpoint is the five description views the model projects onto itself.\n")
	b.WriteString("export type EnterpriseViewpoint =\n")
	for _, v := range Views(uaf.Current()) {
		b.WriteString("\t| " + strconv.Quote(v.ID) + "\n")
	}
	b.WriteString(";\n\n")

	actions := declarations()
	b.WriteString("// enterpriseWrites is the write contract: every schema that may change the model.\n")
	b.WriteString("export const enterpriseWrites = [\n")
	for _, a := range actions {
		b.WriteString("\t" + strconv.Quote(a.Schema) + ",\n")
	}
	b.WriteString("] as const;\n\n")
	b.WriteString("export type EnterpriseWrite = (typeof enterpriseWrites)[number];\n\n")

	b.WriteString("// Payloads of the write contract: required fields are required, the rest optional.\n")
	for _, a := range actions {
		name := payloadType(a.Schema)
		b.WriteString("export type " + name + " = {\n")
		for _, f := range a.Payload {
			if f.Description != "" {
				b.WriteString("\t// " + singleLine(f.Description) + "\n")
			}
			key := f.Name
			if !f.Required {
				key += "?"
			}
			b.WriteString("\t" + key + ": " + payloadFieldType(a, f) + ";\n")
		}
		b.WriteString("};\n\n")
	}
	b.WriteString("// EnterpriseWritePayloads maps each write to its payload type.\nexport type EnterpriseWritePayloads = {\n")
	for _, a := range actions {
		b.WriteString("\t" + strconv.Quote(a.Schema) + ": " + payloadType(a.Schema) + ";\n")
	}
	b.WriteString("};\n\n")

	b.WriteString("export type EnterpriseTarget = { type: string; id: string };\n\n")
	b.WriteString("// EnterpriseDecide is any host's decide(), structurally: schema, target, payload.\n")
	b.WriteString("export type EnterpriseDecide = (schema: string, target: EnterpriseTarget, payload: Record<string, unknown>) => Promise<unknown>;\n\n")
	b.WriteString("// enterpriseWrite applies the write contract typed: the schema picks the payload.\n")
	b.WriteString("export function enterpriseWrite<K extends EnterpriseWrite>(\n\tdecide: EnterpriseDecide,\n\tschema: K,\n\ttarget: EnterpriseTarget,\n\tpayload: EnterpriseWritePayloads[K],\n): Promise<unknown> {\n\treturn decide(schema, target, payload);\n}\n\n")

	b.WriteString("export type EnterpriseGet = <T>(path: string) => Promise<T>;\n\n")
	b.WriteString("export type EnterpriseQueryFilters = {\n")
	b.WriteString("\tstereotype?: EnterpriseStereotype | EnterpriseStereotype[];\n")
	b.WriteString("\tkind?: string;\n")
	b.WriteString("\tq?: string;\n")
	b.WriteString("\t// alive is a YYYY-MM-DD day, or \"all\" for closed and future elements too; default today.\n")
	b.WriteString("\talive?: string;\n")
	b.WriteString("\tlimit?: number;\n};\n\n")
	b.WriteString("// queryElements reads the model through the query contract: a typed,\n// filtered projection, never the whole graph.\n")
	b.WriteString("export async function queryElements(get: EnterpriseGet, filters: EnterpriseQueryFilters = {}): Promise<EnterpriseQueryResult> {\n")
	b.WriteString("\tconst params = new URLSearchParams();\n")
	b.WriteString("\tconst stereotypes = filters.stereotype === undefined ? [] : Array.isArray(filters.stereotype) ? filters.stereotype : [filters.stereotype];\n")
	b.WriteString("\tif (stereotypes.length > 0) params.set(\"stereotype\", stereotypes.join(\",\"));\n")
	b.WriteString("\tif (filters.kind) params.set(\"kind\", filters.kind);\n")
	b.WriteString("\tif (filters.q) params.set(\"q\", filters.q);\n")
	b.WriteString("\tif (filters.alive) params.set(\"alive\", filters.alive);\n")
	b.WriteString("\tif (filters.limit) params.set(\"limit\", String(filters.limit));\n")
	b.WriteString("\tconst query = params.toString();\n")
	b.WriteString("\treturn get<EnterpriseQueryResult>(query === \"\" ? \"/v1/enterprise-query\" : \"/v1/enterprise-query?\" + query);\n}\n\n")
	b.WriteString("// resolveElements names stored ids through the ref contract: closed\n// elements resolve too, marked; unknown ids are skipped.\n")
	b.WriteString("export async function resolveElements(get: EnterpriseGet, ids: readonly string[]): Promise<EnterpriseQueryResult> {\n")
	b.WriteString("\tif (ids.length === 0) return { elements: [] };\n")
	b.WriteString("\treturn get<EnterpriseQueryResult>(\"/v1/enterprise-resolve?ids=\" + encodeURIComponent(ids.join(\",\")));\n}\n")
	return b.String()
}

// payloadType is the Go side name of one write's payload type.
func payloadType(schema string) string {
	var b strings.Builder
	b.WriteString("Enterprise")
	for _, part := range strings.FieldsFunc(strings.TrimPrefix(schema, "enterprise."), func(r rune) bool { return r == '.' || r == '-' }) {
		b.WriteString(strings.ToUpper(part[:1]) + part[1:])
	}
	return b.String() + "Payload"
}

// payloadFieldType is one payload field's TypeScript type. The two fields the
// metamodel itself enumerates take their unions; everything else is a scalar
// or an id (references are ids — the SDK does not re-check them, the kernel does).
func payloadFieldType(a platform.Action, f platform.Field) string {
	switch {
	case a.Schema == SchemaElementAdd && f.Name == "stereotype" && f.Type == "string":
		return "EnterpriseStereotype"
	case f.Name == "viewpoint" && f.Type == "string":
		return "EnterpriseViewpoint"
	case f.Type == "integer", f.Type == "number":
		return "number"
	case f.Type == "boolean":
		return "boolean"
	case f.Type == "json":
		return "unknown"
	default:
		return "string"
	}
}

// singleLine keeps a declaration description on one comment line.
func singleLine(s string) string { return strings.ReplaceAll(s, "\n", " ") }

func pinRecord(c platform.Caller, ref string, now time.Time) (map[string]any, *kernel.Error) {
	typ, id, _ := strings.Cut(strings.TrimPrefix(ref, "record:"), "/")
	raw, err := c.ReadRecord(typ, id, now)
	if err != nil {
		return nil, err
	}
	var row map[string]any
	if json.Unmarshal(raw, &row) != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid record")
	}
	return row, nil
}

// Pins are projected through their original record scopes; cached labels never
// grant a reader more access or leave stale names after a business rename.
func readableModel(c platform.Caller, m Model, now time.Time) Model {
	for i := range m.Views {
		pins := []Pin{}
		for _, pin := range m.Views[i].Pins {
			row, err := pinRecord(c, pin.Ref, now)
			if err != nil {
				continue
			}
			pin.Label = RefID(pin.Ref)
			for _, key := range []string{"name", "title", "number", "code"} {
				if label, ok := row[key].(string); ok && label != "" {
					pin.Label = label
					break
				}
			}
			pins = append(pins, pin)
		}
		m.Views[i].Pins = pins
	}
	return m
}
