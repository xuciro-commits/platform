package build

import "platformserver/internal/construction"

import (
	"fmt"

	"reflect"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (b *Build) checkJournal(o construction.Object) error {
	if o.Numbering != nil {
		if err := o.Numbering.Check(o); err != nil {
			return err
		}
	}
	for _, a := range o.Actions {
		if a.Reverses != "" {
			j := -1
			for k, other := range o.Actions {
				if other.Name == a.Reverses {
					j = k
				}
			}
			switch {
			case j < 0 || a.Reverses == a.Name:
				return fmt.Errorf("the action %q reverses %q, which is not another action of this object", a.Name, a.Reverses)
			case len(a.Posts) > 0 || a.Journal != nil:
				return fmt.Errorf("the action %q reverses %q and so posts and books nothing of its own", a.Name, a.Reverses)
			}
		}
		if a.Journal == nil {
			continue
		}
		where := fmt.Sprintf("the action %q books", a.Name)
		if a.Journal.ClosedPeriod != "" && a.Journal.ClosedPeriod != "reject" && a.Journal.ClosedPeriod != "next-open" {
			return fmt.Errorf("%s with an unknown closed period policy", where)
		}
		if len(a.Journal.Lines) < 2 {
			return fmt.Errorf("%s fewer than two lines", where)
		}
		if a.Journal.Date != "" {
			if err := checkSource(o, a, a.Journal.Date); err != nil {
				return fmt.Errorf("%s, date: %w", where, err)
			}
		}
		for i, l := range a.Journal.Lines {
			if l.Account == "" || (l.Debit == "") == (l.Credit == "") {
				return fmt.Errorf("%s line %d without an account, or without exactly one of debit and credit", where, i+1)
			}
			for _, src := range []string{l.Account, l.Debit, l.Credit, l.Object, l.Partner} {
				if src == "" {
					continue
				}
				if err := checkSource(o, a, src); err != nil {
					return fmt.Errorf("%s line %d: %w", where, i+1, err)
				}
			}
		}
	}
	return nil
}

// numberingOf gives a new record the next number: the count of records so far
// (this year, when yearly) plus one, which is gapless because nothing is deleted.
func numberingOf(o construction.Object, model any) func(c platform.Caller, record any, now time.Time) *kernel.Error {
	if o.Numbering == nil {
		return nil
	}
	n, typ := *o.Numbering, reflect.TypeOf(model)
	return func(c platform.Caller, record any, now time.Time) *kernel.Error {
		v := reflect.ValueOf(record).Elem()
		field := v.FieldByName(construction.GoName(n.Field))
		if !field.IsValid() || field.Kind() != reflect.String || field.String() != "" || v.Field(0).Interface().(platform.Record).Revision > 0 {
			return nil
		}
		year := now.UTC().Year()
		_, count, err := c.FindOf(typ, platform.Query{Limit: 1, Archived: true})
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The number range is not readable here")
		}
		seq := count + 1
		if n.Yearly {
			prefix := n.Prefix + strconv.Itoa(year) + "-"
			all, _, _ := c.FindOf(typ, platform.Query{Limit: 100000, Archived: true})
			seq = 1
			for _, r := range all {
				if strings.HasPrefix(reflect.ValueOf(r).FieldByName(construction.GoName(n.Field)).String(), prefix) {
					seq++
				}
			}
		}
		field.SetString(n.Format(seq, year))
		return nil
	}
}
