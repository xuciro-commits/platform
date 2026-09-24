// Package sales is the sales software as a solution (ADR-0011): the platform's
// directory and relations, a lodging provider and the CRM, composed without a
// bridge. The CRM consumes the lodging protocol; any provider serves it.
package sales

import (
	"crm"
	"hotel"
	"platformserver"
)

// NewTenant composes the software for one tenant with the hotel as lodging provider.
func NewTenant(id string, rooms map[string]hotel.RoomType, seats ...platformserver.Seat) (*platformserver.Tenant, error) {
	return Compose(id, hotel.NewHotel(id, rooms), seats...)
}

// Compose puts any lodging provider under the CRM.
func Compose(id string, lodging platformserver.App, seats ...platformserver.Seat) (*platformserver.Tenant, error) {
	return platformserver.NewTenant(id, platformserver.NewDirectory(id, seats...), platformserver.NewRelations(id), lodging, crm.New(id))
}
