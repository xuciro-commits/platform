package platformserver

import transport "platformserver/kernel/transport"

import (
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"platformserver/internal/systemscope"
	controlowner "platformserver/kernel/control"
	"regexp"
	"slices"
	"strings"

	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

func (d *Deployment) Tenants(development platform.TenantSpec) []platform.TenantSpec {
	d.specs = &controlowner.Specs{}
	if err := d.specs.Load(d.TenantsFile, development); err != nil {
		log.Fatalf("tenants: %v", err)
	}
	d.system = d.systemSpec(development)
	list := d.specs.All()
	if _, ok := d.specs.Get(systemscope.Tenant); ok {
		return list
	}
	return append(list, d.system)
}

func (d *Deployment) systemSpec(development platform.TenantSpec) platform.TenantSpec {
	roles := map[string]string{
		"platform": controlowner.Admin, "system": "publisher", "build": "builder",
		platform.AIApp: platform.AIAdmin, platform.AgentApp: platform.AgentAdmin,
	}
	seats := []platform.Seat{}
	if d.HostAdmins != "" {
		for _, subject := range strings.Split(d.HostAdmins, ",") {
			subject = strings.TrimSpace(subject)
			if subject == "" {
				continue
			}
			seats = append(seats, platform.Seat{Subjects: []string{subject}, Member: platform.Member{ID: "operator", Roles: roles}})
		}
	}
	if len(seats) == 0 {
		for _, seat := range development.Seats {
			merged := platform.Member{ID: seat.Member.ID, Roles: maps.Clone(seat.Member.Roles)}
			if merged.Roles == nil {
				merged.Roles = map[string]string{}
			}
			for app, role := range roles {
				if _, held := merged.Roles[app]; !held {
					merged.Roles[app] = role
				}
			}
			seats = append(seats, platform.Seat{Subjects: seat.Subjects, Member: merged})
		}
	}
	return platform.TenantSpec{ID: systemscope.Tenant, Name: "Platform", Seats: seats}
}

func (d *Deployment) SeatsFor(id string) []platform.Seat {
	if d.specs == nil {
		return nil
	}
	spec, found := d.specs.Get(id)
	if !found && id == systemscope.Tenant {
		return d.system.Seats
	}
	return spec.Seats
}

func (d *Deployment) Templates() []platform.TenantTemplate {
	out := []platform.TenantTemplate{}
	if d.TemplatesDir == "" {
		return out
	}
	entries, _ := os.ReadDir(d.TemplatesDir)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(d.TemplatesDir, e.Name()))
		if err != nil {
			continue
		}
		var t platform.TenantTemplate
		if json.Unmarshal(raw, &t) != nil {
			log.Printf("template %s: not a tenant template", e.Name())
			continue
		}
		t.Name = strings.TrimSuffix(e.Name(), ".json")
		if t.Title == "" {
			t.Title = t.Name
		}
		out = append(out, t)
	}
	slices.SortFunc(out, func(a, b platform.TenantTemplate) int { return strings.Compare(a.Name, b.Name) })
	return out
}

func (d *Deployment) spec(req platform.CreateTenantRequest, by string, now time.Time) (platform.TenantSpec, platform.TenantTemplate, error) {
	var tpl platform.TenantTemplate
	if !controlowner.ValidTenantID(req.ID) {
		return platform.TenantSpec{}, tpl, fmt.Errorf("tenant id: 2-32 lower-case letters, digits and dashes, starting with a letter")
	}
	if !strings.HasPrefix(req.Admin, "user:") && !strings.HasPrefix(req.Admin, "client:") {
		return platform.TenantSpec{}, tpl, fmt.Errorf("admin: user:<email> or client:<id>")
	}
	if req.Template != "" {
		i := slices.IndexFunc(d.Templates(), func(t platform.TenantTemplate) bool { return t.Name == req.Template })
		if i < 0 {
			return platform.TenantSpec{}, tpl, fmt.Errorf("template %q: not offered", req.Template)
		}
		tpl = d.Templates()[i]
	}
	settings := map[string]string{}
	for k, v := range tpl.Settings {
		settings[k] = v
	}
	for k, v := range req.Settings {
		settings[k] = v
	}
	if req.Name != "" {
		settings[controlowner.SettingName] = req.Name
	}
	_, after, _ := strings.Cut(req.Admin, ":")
	id, _, _ := strings.Cut(after, "@")
	id = strings.ToLower(regexp.MustCompile(`[^a-z0-9-]+`).ReplaceAllString(strings.ToLower(id), "-"))
	if id == "" {
		id = "admin"
	}
	seat := platform.Seat{Subjects: []string{req.Admin}, Member: platform.Member{ID: id, Roles: map[string]string{controlowner.PlatformApp: controlowner.Admin}}}
	return platform.TenantSpec{ID: req.ID, Name: req.Name, Template: req.Template, Seats: []platform.Seat{seat}, Settings: settings, Created: now.UTC(), By: by}, tpl, nil
}

func firstDecisions(t *Tenant, spec platform.TenantSpec, tpl platform.TenantTemplate, now time.Time) error {
	if len(spec.Seats) == 0 {
		return nil
	}
	admin, ok := t.member(spec.Seats[0].ID)
	if !ok {
		return fmt.Errorf("tenant %s: first seat %s is not a member", t.ID, spec.Seats[0].ID)
	}
	key := 0
	decide := func(schema, typ, id string, payload any) error {
		key++
		raw, _ := json.Marshal(payload)
		_, err := t.Submit(admin, &pb.Submission{TenantId: t.ID, PrincipalId: admin.ID, Authority: controlowner.PlatformApp, IdempotencyKey: fmt.Sprintf("create:%d", key),
			Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, now)
		if err != nil {
			return fmt.Errorf("%s %s: %s", schema, id, err.Error())
		}
		return nil
	}
	roles := tpl.AdminRoles
	if len(roles) == 0 {
		roles = map[string]string{}
		for _, a := range t.apps {
			if m := a.Manifest(); m.ID != controlowner.PlatformApp && slices.Contains(m.AllRoles(), controlowner.Admin) {
				roles[m.ID] = controlowner.Admin
			}
		}
	}
	apps := make([]string, 0, len(roles))
	for app := range roles {
		apps = append(apps, app)
	}
	slices.Sort(apps)
	for _, app := range apps {
		if t.app(app) == nil {
			continue
		}
		if err := decide(controlowner.SchemaGrant, controlowner.MemberType, admin.ID, map[string]string{"app": app, "role": roles[app]}); err != nil {
			return err
		}
		admin, _ = t.member(admin.ID)
	}
	names := make([]string, 0, len(spec.Settings))
	for name := range spec.Settings {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if err := decide(controlowner.SchemaSettingSet, controlowner.SettingType, controlowner.PlatformApp+"/"+name, map[string]string{"value": spec.Settings[name]}); err != nil {
			return err
		}
	}
	return nil
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
	h.hostRoute(mux, transport.Route{Pattern: "POST /v1/host/tenants", Summary: "Create a tenant from a template with its first administrator; it serves on the next request and survives restarts (host administrators)", Body: platform.CreateTenantRequest{}, Answer: HostTenantView{}},
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
