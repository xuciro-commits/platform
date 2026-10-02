package platform

import (
	"encoding/json"
	"fmt"
)

// RecordSetPredicate describes complete matching records of the outer query's
// object. It has no window, sort, archive flag or independently chosen object.
type RecordSetPredicate struct {
	Domain json.RawMessage `json:"domain,omitempty"`
	Search string          `json:"search,omitempty"`
	Set    *QuerySet       `json:"set,omitempty"`
}

type QuerySet struct {
	Op     string                `json:"op"`
	Inputs []*RecordSetPredicate `json:"inputs"`
}

const (
	QuerySetMaxNodes = 31
	QuerySetMaxDepth = 4
	QuerySetMaxBytes = 64 * 1024
)

// CheckSet bounds the whole tree before execution, including branches that a
// particular row would short-circuit. Ordinary queries keep their old limits.
func (q Query) CheckSet() error {
	if q.Set == nil {
		return nil
	}
	if q.Offset < 0 || q.Limit < 0 {
		return fmt.Errorf("invalid record set window")
	}
	nodes := 0
	var check func(RecordSetPredicate, int) error
	check = func(p RecordSetPredicate, depth int) error {
		nodes++
		if nodes > QuerySetMaxNodes || depth > QuerySetMaxDepth || len(p.Search) > 4096 {
			return fmt.Errorf("record set budget exceeded")
		}
		if len(p.Domain) > 0 {
			var tokens []json.RawMessage
			if json.Unmarshal(p.Domain, &tokens) != nil || len(tokens) > 32 {
				return fmt.Errorf("record set domain budget exceeded")
			}
		}
		if p.Set != nil {
			if (p.Set.Op != "union" && p.Set.Op != "intersect" && p.Set.Op != "subtract") || len(p.Set.Inputs) != 2 {
				return fmt.Errorf("record set needs a binary supported operation")
			}
			for _, input := range p.Set.Inputs {
				if input == nil {
					return fmt.Errorf("record set input is null")
				}
				if err := check(*input, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	root := RecordSetPredicate{Domain: q.Domain, Search: q.Search, Set: q.Set}
	if err := check(root, 0); err != nil {
		return err
	}
	encoded, err := json.Marshal(root)
	if err != nil || len(encoded) > QuerySetMaxBytes {
		return fmt.Errorf("record set encoding budget exceeded")
	}
	return nil
}
