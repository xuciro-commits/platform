package build

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"platformserver/platform"
)

func (a Action) Reversal(of Action) []Post {
	posts := make([]Post, len(of.Posts))
	for i, p := range of.Posts {
		p.Subtract, p.Floor = !p.Subtract, false
		posts[i] = p
	}
	return posts
}

func (o Object) EffectivePosts(a Action) []Post {
	if a.Reverses == "" {
		return a.Posts
	}
	for _, of := range o.Actions {
		if of.Name == a.Reverses {
			posts := a.Reversal(of)
			return posts
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
