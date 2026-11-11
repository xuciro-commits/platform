package host

import ()

func (t *Tenant) pageModelRuleRecord(typ, id string, fallback any) any {
	t.Projection.Lock()
	defer t.Projection.Unlock()
	if entity := t.Projection.Type(typ); entity != nil {
		if original := entity.Rows().Get(id); original != nil {
			return original.Value.Interface()
		}
	}
	return fallback
}
