package platform

import (
	"slices"
	"time"

	"platformkernel/kernel"
)

type Date = string

type Period struct {
	From  Date `json:"from,omitempty"`
	Until Date `json:"until,omitempty"`
}

type Unit struct {
	Periods  []Period `json:"periods,omitempty"`
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Kind     string   `json:"kind"`
	Legal    bool     `json:"legal,omitempty"`
	External bool     `json:"external,omitempty"`
	From     Date     `json:"from,omitempty"`
	Until    Date     `json:"until,omitempty"`
	Closed   string   `json:"closed,omitempty"`
	Calendar string   `json:"calendar,omitempty"`
}

type Calendar struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Workdays []int  `json:"workdays,omitempty"`
	Holidays []Date `json:"holidays,omitempty"`
}

func (c Calendar) Works(day time.Time) bool {
	wd := int(day.Weekday())
	if wd == 0 {
		wd = 7
	}
	workdays := c.Workdays
	if len(workdays) == 0 {
		workdays = []int{1, 2, 3, 4, 5}
	}
	if !slices.Contains(workdays, wd) {
		return false
	}
	return !slices.Contains(c.Holidays, day.Format(time.DateOnly))
}

func (c Calendar) After(from time.Time, n int) time.Time {
	at := from
	for n > 0 {
		at = at.AddDate(0, 0, 1)
		if c.Works(at) {
			n--
		}
	}
	return at
}

type Structure struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Matrix bool   `json:"matrix,omitempty"`
}

type Edge struct {
	Periods   []Period `json:"periods,omitempty"`
	Structure string   `json:"structure"`
	Unit      string   `json:"unit"`
	Parent    string   `json:"parent"`
	Relation  string   `json:"relation,omitempty"`
	Share     float64  `json:"share,omitempty"`
	From      Date     `json:"from,omitempty"`
	Until     Date     `json:"until,omitempty"`
}

type Membership struct {
	Periods []Period `json:"periods,omitempty"`
	Party   string   `json:"party"`
	Unit    string   `json:"unit"`
	Role    string   `json:"role"`
	Primary bool     `json:"primary,omitempty"`
	From    Date     `json:"from,omitempty"`
	Until   Date     `json:"until,omitempty"`
}

type OrgSeed struct {
	Calendars   []Calendar   `json:"calendars,omitempty"`
	Structures  []Structure  `json:"structures"`
	Units       []Unit       `json:"units"`
	Edges       []Edge       `json:"edges"`
	Memberships []Membership `json:"memberships"`
}

func (c Caller) Units(structure string, now time.Time) []string {
	if c.rt == nil {
		return nil
	}
	return c.rt.Units(c, structure, now)
}

type ElementInfo struct {
	ID         string `json:"id"`
	Stereotype string `json:"stereotype"`
	Name       string `json:"name"`
	Kind       string `json:"kind,omitempty"`
	Owner      string `json:"owner,omitempty"`
}

type Enterprise struct{ c Caller }

func (c Caller) Enterprise() Enterprise { return Enterprise{c} }

type EnterpriseUsageRuntime interface {
	CheckEnterpriseUsage(Caller, string, string, bool, time.Time) *kernel.Error
	CheckEnterpriseTenantConnection(Caller, string, time.Time) *kernel.Error
}

func (e Enterprise) CheckUsage(id, until string, deleting bool, now time.Time) *kernel.Error {
	if e.c.Replaying {
		return nil
	}
	if runtime, ok := e.c.rt.(EnterpriseUsageRuntime); ok {
		return runtime.CheckEnterpriseUsage(e.c, id, until, deleting, now)
	}
	return nil
}

func (e Enterprise) CheckTenantConnection(id string, now time.Time) *kernel.Error {
	if e.c.Replaying {
		return nil
	}
	if runtime, ok := e.c.rt.(EnterpriseUsageRuntime); ok {
		return runtime.CheckEnterpriseTenantConnection(e.c, id, now)
	}
	return nil
}

func (e Enterprise) Units(kind string, now time.Time) []string { return e.c.Units(kind, now) }

func (e Enterprise) Element(id string, now time.Time) (ElementInfo, bool) {
	if e.c.rt == nil {
		return ElementInfo{}, false
	}
	return e.c.rt.Element(e.c, id, now)
}

func (e Enterprise) Related(element, stereotype string, outgoing bool, now time.Time) []string {
	if e.c.rt == nil {
		return nil
	}
	return e.c.rt.Related(e.c, element, stereotype, outgoing, now)
}

func (e Enterprise) Capable(element string, now time.Time) []string {
	out := e.Related(element, "Exhibits", true, now)
	for _, cap := range e.Related(element, "IsCapableToPerform", true, now) {
		if !slices.Contains(out, cap) {
			out = append(out, cap)
		}
	}
	return out
}

func (e Enterprise) Located(element string, now time.Time) []string {
	return e.Related(element, "ActualResourceRelationship", true, now)
}

func (e Enterprise) Of(organisation string, now time.Time) []string {
	return e.Related(organisation, "OwnsProcess", true, now)
}

type EnterpriseQueryElement struct {
	ID         string   `json:"id"`
	Stereotype string   `json:"stereotype"`
	Name       string   `json:"name"`
	Kind       string   `json:"kind,omitempty"`
	ShortName  string   `json:"shortName,omitempty"`
	Legal      bool     `json:"legal,omitempty"`
	From       Date     `json:"from,omitempty"`
	Until      Date     `json:"until,omitempty"`
	Periods    []Period `json:"periods,omitempty"`
	Closed     bool     `json:"closed,omitempty"`
}

type EnterpriseQueryResult struct {
	Elements []EnterpriseQueryElement `json:"elements"`
}

type EnterpriseQueries interface {
	Query(stereotypes []string, kind, words, alive string, limit int, now time.Time) (EnterpriseQueryResult, *kernel.Error)
	Resolve(ids []string, now time.Time) (EnterpriseQueryResult, *kernel.Error)
}

type Directory interface {
	Units(party, structure string, day Date) []string
	Below(units []string, structure string, day Date) []string
	Holders(structure, unit, role string, day Date) []string
	Calendar(party string, day Date) Calendar
	Element(id string, day Date) (ElementInfo, bool)
	Related(element, stereotype string, outgoing bool, day Date) []string
}
