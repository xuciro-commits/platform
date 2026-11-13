package objects

import (
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
)

func CheckNamedQuery(q platform.NamedQuery, info platform.EntityInfo, engine platform.RuleEngine) error {
	if err := q.Check(); err != nil {
		return err
	}
	if q.Object != info.Type {
		return fmt.Errorf("query source object is unavailable")
	}
	return CheckQueryFields(q, info, engine)
}

func CheckQueryFields(q platform.NamedQuery, info platform.EntityInfo, engine platform.RuleEngine) error {
	if _, err := records.CompileDomain(info, q.Domain, engine); err != nil {
		return err
	}
	if q.Filter != nil {
		if _, err := records.CompilePlan(info, *q.Filter, "", engine); err != nil {
			return err
		}
	}
	if q.By != "" {
		field, ok := info.Field(q.By)
		if !ok || field.Type != "reference" && field.SourceType != "record" {
			return fmt.Errorf("query parent input needs a readable reference or record source")
		}
	}
	_, err := records.CompileOrder(info, q.Sort)
	return err
}

func DeclaredQuery(definitions []platform.Definition, app, name, version string) *platform.NamedQuery {
	for _, d := range definitions {
		if d.Ref != (platform.AssetRef{App: app, Kind: platform.AssetQuery, Name: name}) {
			continue
		}
		if version == "" {
			return d.Query
		}
		if bound := d.QueryVersion(version); bound != nil {
			return bound.Query
		}
		return nil
	}
	return nil
}

func DeclaredWindow(q platform.NamedQuery, of string, window platform.Query) (platform.ObjectSet, Window, *kernel.Error) {
	if q.By != "" && of == "" {
		return platform.ObjectSet{}, Window{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{query} needs the record it is run for", q.Title)
	}
	limit := q.Limit
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	if window.Limit > 0 {
		limit = min(window.Limit, limit)
	}
	set := q.ObjectSet(of)
	set.Search = window.Search
	return set, Window{Sort: q.Sort, Offset: window.Offset, Limit: limit}, nil
}
