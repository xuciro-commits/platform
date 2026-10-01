package platformserver

import (
	"fmt"
	"platformserver/platform"
)

func (t *Tenant) checkPageQueries(p platform.Page) error {
	if p.Document == nil {
		return nil
	}
	for id, q := range p.Document.Queries {
		object, ok := t.entity(q.Object.Name)
		if !ok || object.App != q.Object.App {
			return fmt.Errorf("page query %s object is unavailable", id)
		}
		var named *platform.Definition
		if q.Query != nil {
			for i := range t.definitions {
				if t.definitions[i].Ref == q.Query.Ref {
					named = &t.definitions[i]
					break
				}
			}
		}
		if err := p.CheckQuerySchema(q, object, named); err != nil {
			return fmt.Errorf("page query %s: %w", id, err)
		}
	}
	return nil
}
