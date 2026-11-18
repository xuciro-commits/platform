package authoring

import (
	"platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/resources/publication"
)

type Owner struct {
	env publication.ApplicationEnvironment
}

func NewOwner(env publication.ApplicationEnvironment) publication.ApplicationAuthoring {
	return &Owner{env: env}
}

func (o *Owner) Pages() ([]protocol.Page, error) {
	return publication.Inventory[protocol.Page](o.env.Host().Automation(platform.Caller{}, o.env.AppID()))
}

func (o *Owner) Applications() ([]protocol.Application, error) {
	return publication.Inventory[protocol.Application](o.env.Host().Automation(platform.Caller{}, o.env.AppID()))
}
