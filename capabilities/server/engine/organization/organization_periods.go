package organization

import (
	"encoding/json"
	"fmt"
	"platformserver/platform"
	"slices"
	"time"
)

func SameProperties(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func periodsOrLegacy(periods []platform.Period, from, until platform.Date) []platform.Period {
	if len(periods) > 0 {
		return periods
	}
	return []platform.Period{{From: from, Until: until}}
}

func periodsActive(periods []platform.Period, from, until, day platform.Date) bool {
	return slices.ContainsFunc(periodsOrLegacy(periods, from, until), func(p platform.Period) bool { return platform.ActiveOn(p.From, p.Until, day) })
}

func (e Element) ActiveOn(day platform.Date) bool {
	return !e.Deleted && periodsActive(e.Periods, e.From, e.Until, day)
}

func (r Relationship) ActiveOn(day platform.Date) bool {
	return periodsActive(r.Periods, r.From, r.Until, day)
}

func (e Element) Covers(from, until platform.Date) bool {
	if e.Deleted {
		return false
	}
	return slices.ContainsFunc(periodsOrLegacy(e.Periods, e.From, e.Until), func(p platform.Period) bool {
		return (p.From == "" || from >= p.From) && (p.Until == "" || until != "" && until <= p.Until)
	})
}

func (r Relationship) PeriodsFromLegacy() []platform.Period {
	return periodsOrLegacy(r.Periods, r.From, r.Until)
}

func (e Element) PeriodsFromLegacy() []platform.Period {
	return periodsOrLegacy(e.Periods, e.From, e.Until)
}

func CheckPeriods(periods []platform.Period) error {
	for i, p := range periods {
		if p.From != "" {
			if _, err := time.Parse(time.DateOnly, p.From); err != nil {
				return fmt.Errorf("Invalid effective period")
			}
		}
		if p.Until != "" {
			if _, err := time.Parse(time.DateOnly, p.Until); err != nil {
				return fmt.Errorf("Invalid effective period")
			}
			if p.Until <= p.From {
				return fmt.Errorf("The effective period must end after it starts")
			}
		}
		if i > 0 && (periods[i-1].Until == "" || p.From < periods[i-1].Until) {
			return fmt.Errorf("Effective periods must not overlap")
		}
	}
	return nil
}

func (e *Element) CloseOn(day platform.Date, reason string) error {
	periods := slices.Clone(e.PeriodsFromLegacy())
	last := &periods[len(periods)-1]
	if day == "" || day <= last.From || last.Until != "" && day > last.Until {
		return fmt.Errorf("The closing date cannot extend the effective period")
	}
	last.Until = day
	if err := CheckPeriods(periods); err != nil {
		return err
	}
	e.Until, e.Closed = day, reason
	if len(e.Periods) > 0 {
		e.Periods = periods
	}
	return nil
}

func (e *Element) ReopenOn(day platform.Date) error {
	periods := slices.Clone(e.PeriodsFromLegacy())
	last := periods[len(periods)-1]
	if e.Deleted || last.Until == "" || day == "" || day < last.Until {
		return fmt.Errorf("Reopening starts a new period after the previous one ends")
	}
	periods = append(periods, platform.Period{From: day})
	if err := CheckPeriods(periods); err != nil {
		return err
	}
	e.Periods = periods
	e.Until, e.Closed = "", ""
	return nil
}

func (e *Element) Reinstate() error {
	if e.Deleted || e.Until == "" {
		return fmt.Errorf("Only a closed element can be reinstated")
	}
	e.Until, e.Closed = "", ""
	if len(e.Periods) > 0 {
		e.Periods[len(e.Periods)-1].Until = ""
	}
	return nil
}

func (r *Relationship) EndOn(day platform.Date) error {
	if _, err := time.Parse(time.DateOnly, day); err != nil {
		return fmt.Errorf("Invalid effective period")
	}
	periods := slices.Clone(r.PeriodsFromLegacy())
	last := &periods[len(periods)-1]
	if day < last.From || last.Until != "" && day > last.Until {
		return fmt.Errorf("Ending a relationship cannot extend its effective period")
	}
	last.Until = day
	r.Until = day
	if len(r.Periods) > 0 {
		r.Periods = periods
	}
	return nil
}

func (r *Relationship) ReopenOn(day platform.Date) {
	periods := slices.Clone(r.PeriodsFromLegacy())
	last := periods[len(periods)-1]
	if last.Until != "" && day >= last.Until {
		r.Periods = append(periods, platform.Period{From: day})
		r.Until = ""
	}
}

type Date = platform.Date

type Period = platform.Period
