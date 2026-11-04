package objects

import (
	"fmt"
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
