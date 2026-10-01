package platform

import (
	"encoding/json"
	"fmt"
)

// Check bounds a reusable declaration. Field and predicate semantics remain
// with the original record reader; this does not implement another evaluator.
func (q NamedQuery) Check() error {
	if q.Name == "" || q.Title == "" || q.Object == "" || len(q.Name) > 80 || len(q.Title) > 1024 || len(q.Description) > 4096 || q.Limit < 0 || q.Limit > 200 || len(q.Sort) > 4 || len(q.Domain) > 16384 {
		return fmt.Errorf("query has an invalid identity or budget")
	}
	var terms []json.RawMessage
	if len(q.Domain) > 0 && (json.Unmarshal(q.Domain, &terms) != nil || len(terms) > 32) {
		return fmt.Errorf("query domain needs a bounded list of conditions")
	}
	return nil
}

// QueryVersion resolves an explicit binding without falling back to latest.
func (d Definition) QueryVersion(version string) *Definition {
	if version == d.Version && d.Query != nil {
		return &d
	}
	q, ok := d.QueryVersions[version]
	if !ok {
		return nil
	}
	d.Query, d.Version, d.QueryVersions = &q, version, nil
	return &d
}
