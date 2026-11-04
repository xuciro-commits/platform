package host

import (
	"platformserver/services/data"
	"platformserver/services/resources/authoring"
)

func (t *Tenant) dataOwner() *data.Owner {
	owner, _ := t.app(build.ID).(interface{ DataOwner() *data.Owner })
	if owner == nil {
		return nil
	}
	return owner.DataOwner()
}
