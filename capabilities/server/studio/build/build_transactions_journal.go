package build

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"platformserver/platform"
)

type JournalPost struct {
	ClosedPeriod string            `json:"closedPeriod,omitempty" choices:"reject,next-open" title:"Closed period policy"`
	Date         string            `json:"date,omitempty" example:"record.receivedOn"`
	Text         string            `json:"text,omitempty" help:"A literal or a source for the entry's text"`
	Lines        []JournalPostLine `json:"lines" title:"Lines"`
}

type JournalPostLine struct {
	Account string `json:"account" help:"=<account code> or a source holding one" example:"=1403"`
	Debit   string `json:"debit,omitempty" example:"amount"`
	Credit  string `json:"credit,omitempty" example:"amount"`
	Text    string `json:"text,omitempty"`
	Object  string `json:"object,omitempty" help:"What the line is about, e.g. record.material"`
	Partner string `json:"partner,omitempty" example:"record.supplier"`
}

const BooksEndpoint = "core:books"

func (a Action) Reversal(of Action) ([]Post, *JournalPost) {
	posts := make([]Post, len(of.Posts))
	for i, p := range of.Posts {
		p.Subtract, p.Floor = !p.Subtract, false
		posts[i] = p
	}
	var journal *JournalPost
	if of.Journal != nil {
		journal = &JournalPost{Text: "Reversal: " + of.Journal.Text, ClosedPeriod: of.Journal.ClosedPeriod}
		for _, l := range of.Journal.Lines {
			journal.Lines = append(journal.Lines, JournalPostLine{Account: l.Account, Debit: l.Credit, Credit: l.Debit, Text: l.Text, Object: l.Object, Partner: l.Partner})
		}
	}
	return posts, journal
}

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
	if j.ClosedPeriod != "" {
		entry["closedPeriod"] = j.ClosedPeriod
	}
	if j.Date != "" {
		if d := platform.TextOf(source(j.Date)); len(d) >= 10 {
			entry["date"] = d[:10]
		}
	}
	if j.Text != "" {
		entry["text"] = platform.TextOf(source(j.Text))
	}
	lines := []map[string]any{}
	for i, l := range j.Lines {
		from, side := l.Debit, "debit"
		if from == "" {
			from, side = l.Credit, "credit"
		}
		amount, err := ValueNumber(source(from))
		if err != nil {
			return entry, fmt.Errorf("line %d: the amount is not a number", i+1)
		}
		if amount == 0 {
			continue
		}
		lines = append(lines, map[string]any{"account": platform.TextOf(source(l.Account)), side: amount, "text": l.Text, "object": platform.TextOf(source(l.Object)), "partner": platform.TextOf(source(l.Partner))})
	}
	if len(lines) == 0 {
		return entry, fmt.Errorf("nothing to book")
	}
	entry["lines"] = lines
	return entry, nil
}

func EntryBody(entry map[string]any) string { raw, _ := json.Marshal(entry); return string(raw) }

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

type Numbering struct {
	Field  string `json:"field" help:"The text field that takes the number" example:"number"`
	Prefix string `json:"prefix,omitempty" example:"GR"`
	Yearly bool   `json:"yearly,omitempty" help:"Restart at 1 every year and put the year in the number"`
	Width  int    `json:"width,omitempty" help:"Digits, zero-padded; default 6"`
}

func (n Numbering) Check(o Object) error {
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

type Post struct {
	Object   string `json:"object" field:"required" help:"A defined object of this builder that keeps the balance" example:"build.stockbalance"`
	Match    []Set  `json:"match" title:"Balance identified by"`
	Field    string `json:"field" field:"required" help:"The balance's integer, decimal or money field" example:"onhand"`
	Amount   string `json:"amount" field:"required" example:"quantity"`
	Subtract bool   `json:"subtract,omitempty" title:"Subtract"`
	Floor    bool   `json:"floor,omitempty" title:"Never below zero"`
}

func ValueNumber(x any) (float64, error) {
	switch n := x.(type) {
	case float64:
		return n, nil
	case int:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case json.Number:
		return n.Float64()
	case string:
		return strconv.ParseFloat(strings.TrimSpace(n), 64)
	case map[string]any:
		if amount, ok := n["amount"]; ok {
			return ValueNumber(amount)
		}
	case platform.Money:
		return float64(n.Amount), nil
	}
	return 0, fmt.Errorf("not a number")
}
