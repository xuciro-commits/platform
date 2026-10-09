package platform

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Check bounds a reusable declaration. Field and predicate semantics remain
// with the original record reader; this does not implement another evaluator.
func (q NamedQuery) Check() error {
	if q.Name == "" || q.Title == "" || (q.Object == "") == (q.Interface == "") || len(q.Name) > 80 || len(q.Title) > 1024 || len(q.Description) > 4096 || q.Limit < 0 || q.Limit > 200 || len(q.Sort) > 4 || len(q.Domain) > 16384 {
		return fmt.Errorf("query has an invalid identity or budget")
	}
	if q.Interface != "" {
		owner, _, _ := strings.Cut(q.Interface, ".")
		if q.By != "" || q.InterfaceShape == nil || q.InterfaceShape.Name != q.Interface || q.InterfaceShape.Check(owner) != nil || len(q.Implementations) > 256 {
			return fmt.Errorf("interface query needs its frozen shape and implementation list, without an untyped parent input")
		}
		for i, typ := range q.Implementations {
			if typ == "" || slices.Contains(q.Implementations[:i], typ) {
				return fmt.Errorf("interface query implementations must be distinct object types")
			}
		}
	} else if q.InterfaceShape != nil || len(q.Implementations) != 0 {
		return fmt.Errorf("object query cannot carry an interface binding")
	}
	var terms []json.RawMessage
	if len(q.Domain) > 0 && (json.Unmarshal(q.Domain, &terms) != nil || len(terms) > 32) {
		return fmt.Errorf("query domain needs a bounded list of conditions")
	}
	return nil
}

// Dependencies names the actual object definitions a query was published
// against. An interface is a contract in the query bytes, not an object type.
func (q NamedQuery) Dependencies() []AssetRef {
	types := q.Implementations
	if q.Object != "" {
		types = []string{q.Object}
	}
	refs := make([]AssetRef, 0, len(types))
	for _, typ := range types {
		owner, _, _ := strings.Cut(typ, ".")
		refs = append(refs, AssetRef{App: owner, Kind: AssetObject, Name: typ})
	}
	slices.SortFunc(refs, func(a, b AssetRef) int { return strings.Compare(a.String(), b.String()) })
	return refs
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
	d.Query, d.Version, d.QueryVersions, d.Requires = &q, version, nil, q.Dependencies()
	return &d
}
