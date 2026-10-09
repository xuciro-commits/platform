package enterprise

import (
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// The query and resolve halves of the ref/query contracts (ADR-0094 D1, D2):
// typed reads over the one model, projected to what a consumer needs — never
// the whole graph. The host serves them as GET /v1/enterprise-query and
// /v1/enterprise-resolve; the generated SDK (SDK()) wraps both.

// EnterpriseQueryElement is one element as the query contract projects it.
type EnterpriseQueryElement struct {
	ID         string `json:"id"`
	Stereotype string `json:"stereotype"`
	Name       string `json:"name"`
	Kind       string `json:"kind,omitempty"`
	ShortName  string `json:"shortName,omitempty"`
	Legal      bool   `json:"legal,omitempty"`
	From       Date   `json:"from,omitempty"`
	Until      Date   `json:"until,omitempty"`
	// Closed marks an element that is no longer live today (ended, merged,
	// decommissioned): resolve returns it so a stored reference still reads.
	Closed bool `json:"closed,omitempty"`
}

// EnterpriseQueryResult is the answer both reads share.
type EnterpriseQueryResult struct {
	Elements []EnterpriseQueryElement `json:"elements"`
}

func project(e Element, today Date) EnterpriseQueryElement {
	return EnterpriseQueryElement{ID: e.ID, Stereotype: e.Stereotype, Name: e.Name, Kind: e.Kind, ShortName: e.ShortName,
		Legal: e.Legal, From: e.From, Until: e.Until, Closed: !activeOn(e.From, e.Until, today)}
}

// Query answers the typed read (ADR-0094 D1): stereotypes is an OR-filter
// (empty: every stereotype), kind an exact match, q a case-insensitive
// substring of id/name/shortName/kind, alive a day whose live set to take
// ("all": no window, closed and future included), limit a bounded page (the
// host caps it before calling). Results sort by id: a picker's order must not
// depend on map iteration.
func (e *Enterprise) Query(stereotypes []string, kind, q, alive string, limit int, now time.Time) (EnterpriseQueryResult, *kernel.Error) {
	today := Date(now.UTC().Format(time.DateOnly))
	if alive != "" && alive != "all" {
		if len(alive) != len("2006-01-02") {
			return EnterpriseQueryResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "alive is \"all\" or a YYYY-MM-DD day")
		}
		today = Date(alive)
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
		if len(keep) > 0 && !keep[el.Stereotype] || kind != "" && el.Kind != kind {
			continue
		}
		if alive != "all" && !activeOn(el.From, el.Until, today) {
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

// Resolve answers the ref contract's read side (ADR-0094 D2): the projection
// of each named id, closed elements included and marked, unknown ids skipped —
// a stored reference always reads. At most 100 ids per call keep the answer
// bounded; empty resolves to nothing.
func (e *Enterprise) Resolve(ids []string, now time.Time) (EnterpriseQueryResult, *kernel.Error) {
	if len(ids) > 100 {
		return EnterpriseQueryResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "resolve takes at most 100 ids")
	}
	today := Date(now.UTC().Format(time.DateOnly))
	e.mu.Lock()
	defer e.mu.Unlock()
	out := EnterpriseQueryResult{Elements: []EnterpriseQueryElement{}}
	for _, id := range ids {
		if id == "" {
			continue
		}
		if el := e.model.element(id); el != nil {
			out.Elements = append(out.Elements, project(*el, today))
		}
	}
	return out, nil
}
