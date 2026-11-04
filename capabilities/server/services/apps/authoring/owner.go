package authoring

import (
	"platformserver/platform"
	"platformserver/services/resources/publication"
)

type Environment interface {
	Host() platform.BuildHost
	AppID() string
	Version() string
	Entity(string) (platform.EntityInfo, bool)
	DraftObjectLookup(platform.Caller) (func(string) (platform.EntityInfo, bool), error)
	CheckName(string, string) error
}

type Owner struct {
	env Environment
}

func NewOwner(env Environment) *Owner {
	return &Owner{env: env}
}

func (o *Owner) Fork(env Environment) *Owner {
	return &Owner{env: env}
}

func (o *Owner) Pages() ([]Page, error) {
	return publication.Inventory[Page](o.env.Host().Automation(platform.Caller{}, o.env.AppID()))
}

func (o *Owner) Applications() ([]Application, error) {
	return publication.Inventory[Application](o.env.Host().Automation(platform.Caller{}, o.env.AppID()))
}
