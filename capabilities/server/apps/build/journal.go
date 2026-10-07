package build

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// JournalPost (ADR-0076) is what a decision books: lines whose accounts and
// amounts come from the action's inputs, the record (record.<field>) or
// literals (=1403, =0). Debits and credits must balance when evaluated; the
// host lands the entry through core, which puts it in the open period of its
// date or the next one, and refuses nothing silently.
type JournalPost struct {
	// Date is where the posting date comes from; empty: the decision's time.
	Date  string            `json:"date,omitempty" example:"record.receivedOn"`
	Text  string            `json:"text,omitempty" help:"A literal or a source for the entry's text"`
	Lines []JournalPostLine `json:"lines" title:"Lines"`
}

type JournalPostLine struct {
	Account string `json:"account" help:"=<account code> or a source holding one" example:"=1403"`
	Debit   string `json:"debit,omitempty" example:"amount"`
	Credit  string `json:"credit,omitempty" example:"amount"`
	Text    string `json:"text,omitempty"`
	Object  string `json:"object,omitempty" help:"What the line is about, e.g. record.material"`
	Partner string `json:"partner,omitempty" example:"record.supplier"`
}

// BooksEndpoint is the effect endpoint journal entries go to.
const BooksEndpoint = "core:books"

func (b *Build) checkJournal(o Object) error {
	if o.Numbering != nil {
		if err := o.Numbering.check(o); err != nil {
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

// Reversal is the posts and journal that undo those of the reversed action.
func (a Action) Reversal(of Action) ([]Post, *JournalPost) {
	posts := make([]Post, len(of.Posts))
	for i, p := range of.Posts {
		p.Subtract, p.Floor = !p.Subtract, false
		posts[i] = p
	}
	var journal *JournalPost
	if of.Journal != nil {
		journal = &JournalPost{Text: "Reversal: " + of.Journal.Text}
		for _, l := range of.Journal.Lines {
			journal.Lines = append(journal.Lines, JournalPostLine{Account: l.Account, Debit: l.Credit, Credit: l.Debit, Text: l.Text, Object: l.Object, Partner: l.Partner})
		}
	}
	return posts, journal
}

// Entry evaluates the declaration against the decision: the record after the
// action and the inputs given. The id is the decision's change id, so a
// replay lands the same entry once.
// today is the decider's local date (ADR-0079).
func (j JournalPost) Entry(changeID string, record map[string]any, inputs map[string]any, me string, now time.Time, today string) (map[string]any, error) {
	source := func(from string) any {
		switch {
		case from == "$me":
			return me
		case strings.HasPrefix(from, "="):
			return strings.TrimPrefix(from, "=")
		case strings.HasPrefix(from, "record."):
			return record[strings.TrimPrefix(from, "record.")]
		case from == "":
			return nil
		}
		if v, ok := inputs[from]; ok {
			return v
		}
		return record[from]
	}
	entry := map[string]any{"id": "jnl-" + changeID, "date": today, "source": ID + "/" + changeID}
	if j.Date != "" {
		if d := textOf(source(j.Date)); len(d) >= 10 {
			entry["date"] = d[:10]
		}
	}
	if j.Text != "" {
		entry["text"] = textOf(source(j.Text))
	}
	lines := []map[string]any{}
	for i, l := range j.Lines {
		from, side := l.Debit, "debit"
		if from == "" {
			from, side = l.Credit, "credit"
		}
		amount, err := number(source(from))
		if err != nil {
			return entry, fmt.Errorf("line %d: the amount is not a number", i+1)
		}
		if amount == 0 {
			continue
		}
		lines = append(lines, map[string]any{"account": textOf(source(l.Account)), side: amount, "text": l.Text, "object": textOf(source(l.Object)), "partner": textOf(source(l.Partner))})
	}
	if len(lines) == 0 {
		return entry, fmt.Errorf("nothing to book")
	}
	entry["lines"] = lines
	return entry, nil
}

// EntryBody serializes the frozen action payload; core owns the journal model.
func EntryBody(entry map[string]any) string { raw, _ := json.Marshal(entry); return string(raw) }

// EffectivePosts and EffectiveJournal are what the action really posts and
// books: its own, or the reverse of the action it reverses.
func (o Object) EffectivePosts(a Action) []Post {
	if a.Reverses == "" {
		return a.Posts
	}
	for _, of := range o.Actions {
		if of.Name == a.Reverses {
			posts, _ := a.Reversal(of)
			return posts
		}
	}
	return nil
}

func (o Object) EffectiveJournal(a Action) *JournalPost {
	if a.Reverses == "" {
		return a.Journal
	}
	for _, of := range o.Actions {
		if of.Name == a.Reverses {
			_, journal := a.Reversal(of)
			return journal
		}
	}
	return nil
}

// Numbering is a document number range: SAP's number range object, per object
// type here, optionally restarting each year.
type Numbering struct {
	Field  string `json:"field" help:"The text field that takes the number" example:"number"`
	Prefix string `json:"prefix,omitempty" example:"GR"`
	Yearly bool   `json:"yearly,omitempty" help:"Restart at 1 every year and put the year in the number"`
	Width  int    `json:"width,omitempty" help:"Digits, zero-padded; default 6"`
}

func (n Numbering) check(o Object) error {
	if n.Field == "" {
		return fmt.Errorf("the numbering names the field that takes the number")
	}
	for _, f := range o.Fields {
		if f.Name == n.Field {
			if f.Type != "text" {
				return fmt.Errorf("the numbering field %q is not a text field", n.Field)
			}
			return nil
		}
	}
	return fmt.Errorf("the numbering field %q is not a field of the object", n.Field)
}

// Format is the number for sequence n in year.
func (n Numbering) Format(seq, year int) string {
	width := n.Width
	if width <= 0 {
		width = 6
	}
	if n.Yearly {
		return fmt.Sprintf("%s%d-%0*d", n.Prefix, year, width, seq)
	}
	return fmt.Sprintf("%s%0*d", n.Prefix, width, seq)
}

// numberingOf gives a new record the next number: the count of records so far
// (this year, when yearly) plus one, which is gapless because nothing is deleted.
func numberingOf(o Object, model any) func(c platform.Caller, record any, now time.Time) *kernel.Error {
	if o.Numbering == nil {
		return nil
	}
	n, typ := *o.Numbering, reflect.TypeOf(model)
	return func(c platform.Caller, record any, now time.Time) *kernel.Error {
		v := reflect.ValueOf(record).Elem()
		field := v.FieldByName(goName(n.Field))
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
				if strings.HasPrefix(reflect.ValueOf(r).FieldByName(goName(n.Field)).String(), prefix) {
					seq++
				}
			}
		}
		field.SetString(n.Format(seq, year))
		return nil
	}
}
