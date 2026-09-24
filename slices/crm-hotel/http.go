package crmhotel

import (
	"net/http"
	"time"

	"crm"
	"hotel"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver"
)

// Tenant is one organisation running the composed software: two packages and
// their bridge, assembled at build time (ADR-0008).
type Tenant struct {
	Hotel  *hotel.Hotel
	CRM    *crm.CRM
	Bridge *Bridge
}

func NewTenant(id string, rooms map[string]hotel.RoomType) *Tenant {
	h, c := hotel.NewHotel(id, rooms), crm.New(id)
	return &Tenant{Hotel: h, CRM: c, Bridge: New(id, c, h)}
}

// NewServer serves the composition: submissions go to the package whose catalog
// declares their action (F-23: routing by owner is composition code today),
// and a member's catalog is the union of what each package grants its roles.
func NewServer(tenants map[string]*Tenant, authenticate platformserver.Authenticate[Member]) http.Handler {
	s := platformserver.New(tenants, authenticate)
	hotels, crms, bridges := hotel.Actions(), crm.Actions(), Actions()
	s.Kernel(func(who Member, t *Tenant, sub *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
		schema := sub.GetSchema().GetName()
		if _, ok := hotels.Action(schema); ok {
			return t.Hotel.Submit(who.Hotel(), sub, now)
		}
		if _, ok := crms.Action(schema); ok {
			return t.CRM.Submit(who.CRM(), sub, now)
		}
		if _, ok := bridges.Action(schema); ok {
			return t.Bridge.Submit(who, sub, now)
		}
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
	}, func(t *Tenant) []*pb.AuthorityDeclaration {
		return append(append(t.Hotel.Declarations(), t.CRM.Declarations()...), t.Bridge.Declarations()...)
	})
	s.Read("/v1/actions", func(who Member, t *Tenant) any {
		return append(append(t.CRM.Catalog(who.CRM()), t.Bridge.Catalog(who)...), t.Hotel.Catalog(who.Hotel())...)
	})
	s.Read("/v1/customers", func(_ Member, t *Tenant) any { return t.Bridge.Customers() })
	s.Read("/v1/reservations", func(_ Member, t *Tenant) any { return t.Hotel.Reservations() })
	return s.Handler()
}
