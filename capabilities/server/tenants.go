package platformserver

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

// A host's tenants are configured, not compiled in (ADR-0078 §2.2): the
// -tenants file lists every tenant with its seats and the settings it starts
// with, and the host console adds one at run time from a template. The host
// binary still decides which apps a tenant runs (its Rebuild); the file and
// the console decide who the tenants are.

// TenantSpec is one tenant as configured.
type TenantSpec struct {
	ID       string            `json:"id"`
	Name     string            `json:"name,omitempty"`
	Template string            `json:"template,omitempty"`
	Seats    []Seat            `json:"seats"`
	Settings map[string]string `json:"settings,omitempty"` // platform settings decided when the tenant first starts
	Created  time.Time         `json:"created,omitempty"`
	By       string            `json:"by,omitempty"` // the host administrator who created it
}

// TenantTemplate is a starting point for a tenant (deploy/templates/<name>.json):
// default settings and the roles its first administrator holds in each app.
type TenantTemplate struct {
	Name        string            `json:"name"`
	Title       string            `json:"title"`
	Description string            `json:"description,omitempty"`
	Settings    map[string]string `json:"settings,omitempty"`
	AdminRoles  map[string]string `json:"adminRoles,omitempty"` // app → role; empty: "admin" in every app that defines it
}

// CreateTenantRequest is the host console's request for a new tenant.
type CreateTenantRequest struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Template string            `json:"template,omitempty"`
	Admin    string            `json:"admin"` // user:<email> or client:<id> of the first administrator
	Settings map[string]string `json:"settings,omitempty"`
}

var tenantID = regexp.MustCompile(`^[a-z][a-z0-9-]{1,31}$`)

// specs are the host's tenant specs, from the -tenants file or the binary's development default.
type specs struct {
	mu   sync.Mutex
	file string
	list []TenantSpec
}

func (s *specs) load(file string, development TenantSpec) error {
	s.file = file
	if file == "" {
		s.list = []TenantSpec{development}
		return nil
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	var list []TenantSpec
	if err := json.Unmarshal(raw, &list); err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}
	seen := map[string]bool{}
	for _, t := range list {
		if !tenantID.MatchString(t.ID) || seen[t.ID] {
			return fmt.Errorf("%s: tenant id %q is invalid or repeated", file, t.ID)
		}
		seen[t.ID] = true
	}
	s.list = list
	return nil
}

func (s *specs) get(id string) (TenantSpec, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.list {
		if t.ID == id {
			return t, true
		}
	}
	return TenantSpec{}, false
}

func (s *specs) all() []TenantSpec {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.list)
}

// add appends a spec and writes the file back, so the tenant exists on the next start too.
func (s *specs) add(spec TenantSpec) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if slices.ContainsFunc(s.list, func(t TenantSpec) bool { return t.ID == spec.ID }) {
		return fmt.Errorf("tenant %s exists", spec.ID)
	}
	next := append(slices.Clone(s.list), spec)
	if s.file != "" {
		raw, _ := json.MarshalIndent(next, "", "  ")
		if err := os.WriteFile(s.file, append(raw, '\n'), 0o644); err != nil {
			return err
		}
	}
	s.list = next
	return nil
}

// Tenants are the tenant specs a host starts with: the -tenants file, or the
// binary's development tenant. Call it before Rebuild; SeatsFor answers from it.
func (d *Deployment) Tenants(development TenantSpec) []TenantSpec {
	d.specs = &specs{}
	if err := d.specs.load(d.TenantsFile, development); err != nil {
		log.Fatalf("tenants: %v", err)
	}
	return d.specs.all()
}

// SeatsFor are the configured seats of a tenant, for the binary's Rebuild.
func (d *Deployment) SeatsFor(id string) []Seat {
	if d.specs == nil {
		return nil
	}
	spec, _ := d.specs.get(id)
	return spec.Seats
}

// Templates are the tenant templates of -templates, sorted by name.
func (d *Deployment) Templates() []TenantTemplate {
	out := []TenantTemplate{}
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
		var t TenantTemplate
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
	slices.SortFunc(out, func(a, b TenantTemplate) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// spec turns a console request into a tenant spec: the template's settings
// under the request's, and the first administrator's seat.
func (d *Deployment) spec(req CreateTenantRequest, by string, now time.Time) (TenantSpec, TenantTemplate, error) {
	var tpl TenantTemplate
	if !tenantID.MatchString(req.ID) {
		return TenantSpec{}, tpl, fmt.Errorf("tenant id: 2-32 lower-case letters, digits and dashes, starting with a letter")
	}
	if !strings.HasPrefix(req.Admin, "user:") && !strings.HasPrefix(req.Admin, "client:") {
		return TenantSpec{}, tpl, fmt.Errorf("admin: user:<email> or client:<id>")
	}
	if req.Template != "" {
		i := slices.IndexFunc(d.Templates(), func(t TenantTemplate) bool { return t.Name == req.Template })
		if i < 0 {
			return TenantSpec{}, tpl, fmt.Errorf("template %q: not offered", req.Template)
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
		settings[SettingName] = req.Name
	}
	_, after, _ := strings.Cut(req.Admin, ":")
	id, _, _ := strings.Cut(after, "@")
	id = strings.ToLower(regexp.MustCompile(`[^a-z0-9-]+`).ReplaceAllString(strings.ToLower(id), "-"))
	if id == "" {
		id = "admin"
	}
	seat := Seat{Subjects: []string{req.Admin}, Member: platform.Member{ID: id, Roles: map[string]string{PlatformApp: Admin}}}
	return TenantSpec{ID: req.ID, Name: req.Name, Template: req.Template, Seats: []Seat{seat}, Settings: settings, Created: now.UTC(), By: by}, tpl, nil
}

// firstDecisions gives a new tenant its configured settings and its first
// administrator's roles through ordinary decisions, so they are journaled and
// audited like any later change.
func firstDecisions(t *Tenant, spec TenantSpec, tpl TenantTemplate, now time.Time) error {
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
		_, err := t.Submit(admin, &pb.Submission{TenantId: t.ID, PrincipalId: admin.ID, Authority: PlatformApp, IdempotencyKey: fmt.Sprintf("create:%d", key),
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
			if m := a.Manifest(); m.ID != PlatformApp && slices.Contains(m.AllRoles(), Admin) {
				roles[m.ID] = Admin
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
		if err := decide(SchemaGrant, MemberType, admin.ID, map[string]string{"app": app, "role": roles[app]}); err != nil {
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
		if err := decide(SchemaSettingSet, SettingType, PlatformApp+"/"+name, map[string]string{"value": spec.Settings[name]}); err != nil {
			return err
		}
	}
	return nil
}

// tenantRoutes are the host console's tenant creation: templates and POST /v1/host/tenants.
func (h *Host) tenantRoutes(mux *http.ServeMux) {
	h.hostRoute(mux, Route{Pattern: "GET /v1/host/templates", Summary: "Tenant templates this host offers (host administrators)", Answer: []TenantTemplate{}},
		func(w http.ResponseWriter, _ *http.Request, _ string, _ *Tenant) {
			out := []TenantTemplate{}
			if h.Templates != nil {
				out = h.Templates()
			}
			WriteJSON(w, http.StatusOK, out)
		})
	h.hostRoute(mux, Route{Pattern: "POST /v1/host/tenants", Summary: "Create a tenant from a template with its first administrator; it serves on the next request and survives restarts (host administrators)", Body: CreateTenantRequest{}, Answer: HostTenantView{}},
		func(w http.ResponseWriter, r *http.Request, subject string, _ *Tenant) {
			if h.CreateTenant == nil {
				WriteJSON(w, http.StatusNotImplemented, map[string]any{"error": "this host does not create tenants"})
				return
			}
			var body CreateTenantRequest
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

// Run is a host binary's whole main after its flags and Rebuild: compose
// every configured tenant (the -tenants file, or development) and serve.
func (d *Deployment) Run(development TenantSpec) error {
	if d.Rebuild == nil {
		return fmt.Errorf("run: no Rebuild")
	}
	var tenants []*Tenant
	for _, spec := range d.Tenants(development) {
		t, err := d.Rebuild(spec.ID)
		if err != nil {
			return err
		}
		tenants = append(tenants, t)
	}
	return d.Serve(tenants...)
}
