package host

import transport "platformserver/kernel/transport"

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	controlowner "platformserver/services/security/control"
	"slices"

	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

func (d *Deployment) Tenants(development platform.TenantSpec) []platform.TenantSpec {
	d.specs = &controlowner.Specs{}
	if err := d.specs.Load(d.TenantsFile, development); err != nil {
		log.Fatalf("tenants: %v", err)
	}
	return d.specs.All()
}

func (d *Deployment) SeatsFor(id string) []platform.Seat {
	if d.specs == nil {
		return nil
	}
	spec, _ := d.specs.Get(id)
	return spec.Seats
}

func (d *Deployment) Templates() []platform.TenantTemplate {
	return controlowner.LoadTemplates(d.TemplatesDir)
}

func firstDecisions(t *Tenant, spec platform.TenantSpec, tpl platform.TenantTemplate, now time.Time) error {
	return controlowner.FirstDecisions(controlowner.FirstDecisionHooks{
		Tenant: t.ID,
		Member: t.member,
		HasApp: func(app string) bool { return t.App(app) != nil },
		DefaultRoles: func() map[string]string {
			roles := map[string]string{}
			for _, a := range t.apps {
				if m := a.Manifest(); m.ID != controlowner.PlatformApp && slices.Contains(m.AllRoles(), controlowner.Admin) {
					roles[m.ID] = controlowner.Admin
				}
			}
			return roles
		},
		Submit: func(admin platform.Member, key int, schema, typ, id string, payload any) error {
			raw, _ := json.Marshal(payload)
			if _, refusal := t.Submit(admin, &pb.Submission{TenantId: t.ID, PrincipalId: admin.ID, Authority: controlowner.PlatformApp, IdempotencyKey: fmt.Sprintf("create:%d", key),
				Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, now); refusal != nil {
				return refusal
			}
			return nil
		},
	}, spec, tpl)
}

func (h *Host) tenantRoutes(mux *http.ServeMux) {
	h.hostRoute(mux, transport.Route{Pattern: "GET /v1/host/templates", Summary: "Tenant templates this host offers (host administrators)", Answer: []platform.TenantTemplate{}},
		func(w http.ResponseWriter, _ *http.Request, _ string, _ *Tenant) {
			out := []platform.TenantTemplate{}
			if h.Templates != nil {
				out = h.Templates()
			}
			WriteJSON(w, http.StatusOK, out)
		})
	h.hostRoute(mux, transport.Route{Pattern: "POST /v1/host/tenants", Summary: "Create a tenant from a template with its first administrator; it serves on the next request and survives restarts (host administrators)", Body: platform.CreateTenantRequest{}, Answer: controlowner.HostTenantView{}},
		func(w http.ResponseWriter, r *http.Request, subject string, _ *Tenant) {
			if h.CreateTenant == nil {
				WriteJSON(w, http.StatusNotImplemented, map[string]any{"error": "this host does not create tenants"})
				return
			}
			var body platform.CreateTenantRequest
			if err := decodeBody(r, &body); err != nil {
				WriteJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
				return
			}
			if slices.ContainsFunc(h.currentTenants(), func(t *Tenant) bool { return t.ID == body.ID }) {
				WriteJSON(w, http.StatusConflict, map[string]any{"error": "tenant " + body.ID + " exists"})
				return
			}
			t, err := h.CreateTenant(body, subject)
			if err != nil {
				WriteJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
				return
			}
			WriteJSON(w, http.StatusCreated, h.tenantView(t))
		})
}

func (d *Deployment) Run(development platform.TenantSpec) error {
	if d.Rebuild == nil && d.RebuildWithCapabilities == nil {
		return fmt.Errorf("run: no tenant factory")
	}
	var tenants []*Tenant
	for _, spec := range d.Tenants(development) {
		t, err := d.rebuildTenant(spec.ID)
		if err != nil {
			return err
		}
		tenants = append(tenants, t)
	}
	return d.Serve(tenants...)
}
