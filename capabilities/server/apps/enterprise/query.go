package enterprise

import (
	enterprisemodel "platformserver/internal/enterprise"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// The query and resolve halves of the ref/query contracts:
// typed reads over the one model, projected to what a consumer needs — never
// the whole graph. The host serves them as GET /v1/enterprise-query and
// /v1/enterprise-resolve; the generated SDK (SDK()) wraps both.

// EnterpriseQueryElement is one element as the query contract projects it.
type EnterpriseQueryElement = enterprisemodel.EnterpriseQueryElement

// EnterpriseQueryResult is the answer both reads share.
type EnterpriseQueryResult = enterprisemodel.EnterpriseQueryResult

func project(e enterprisemodel.Element, today platform.Date) enterprisemodel.EnterpriseQueryElement {
	return enterprisemodel.EnterpriseQueryElement{ID: e.ID, Stereotype: e.Stereotype, Name: e.Name, Kind: e.Kind, ShortName: e.ShortName,
		Legal: e.Legal, From: e.From, Until: e.Until, Periods: slices.Clone(e.Periods), Closed: !e.ActiveOn(today)}
}

// Query answers the typed read: stereotypes is an OR-filter
// (empty: every stereotype), kind an exact match, q a case-insensitive
// substring of id/name/shortName/kind, alive a day whose live set to take
// ("all": no window, closed and future included), limit a bounded page (the
// host caps it before calling). Results sort by id: a picker's order must not
// depend on map iteration.
func (e *Enterprise) Query(stereotypes []string, kind, q, alive string, limit int, now time.Time) (enterprisemodel.EnterpriseQueryResult, *kernel.Error) {
	today := platform.Date(now.UTC().Format(time.DateOnly))
	if alive != "" && alive != "all" {
		if len(alive) != len("2006-01-02") {
			return enterprisemodel.EnterpriseQueryResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "alive is \"all\" or a YYYY-MM-DD day")
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
	matches := []enterprisemodel.Element{}
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
	slices.SortFunc(matches, func(a, b enterprisemodel.Element) int { return strings.Compare(a.ID, b.ID) })
	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
	}
	out := enterprisemodel.EnterpriseQueryResult{Elements: make([]enterprisemodel.EnterpriseQueryElement, len(matches))}
	for i, el := range matches {
		out.Elements[i] = project(el, today)
	}
	return out, nil
}

// Resolve answers the ref contract's read side: the projection
// of each named id, closed elements included and marked, unknown ids skipped —
// a stored reference always reads. At most 100 ids per call keep the answer
// bounded; empty resolves to nothing.
func (e *Enterprise) Resolve(ids []string, now time.Time) (enterprisemodel.EnterpriseQueryResult, *kernel.Error) {
	if len(ids) > 100 {
		return enterprisemodel.EnterpriseQueryResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "resolve takes at most 100 ids")
	}
	today := platform.Date(now.UTC().Format(time.DateOnly))
	e.mu.Lock()
	defer e.mu.Unlock()
	out := enterprisemodel.EnterpriseQueryResult{Elements: []enterprisemodel.EnterpriseQueryElement{}}
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
