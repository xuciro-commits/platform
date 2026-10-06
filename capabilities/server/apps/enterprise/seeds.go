package enterprise

import (
	"fmt"
	"strings"

	"platformserver/platform"
)

// Patterns (ADR-0067 D6, ADR-0068 §5): reusable pieces of an enterprise on a
// four-level backbone that every organisation, whatever its industry, can be
// read against:
//
//	1 Enterprise  a legal whole: group, company, subsidiary
//	2 Site        a place that runs a business: plant, hotel, warehouse, office, region
//	3 Function    a department or workshop inside a site or enterprise
//	4 Team        the working unit: line, shift, team, station
//
// A pattern builds a subtree whose root sits at one level, and can be grafted
// under any organisation — the levels describe the pattern, not a grafting
// restriction. The scale templates (S/M/L/XL) are compositions of the
// same patterns, so an empty tenant and a grown one get the same shapes.

type Pattern struct {
	ID          string           `json:"id"`
	Title       string           `json:"title"`
	Description string           `json:"description"`
	Level       int              `json:"level"` // 1–4, the level of the subtree's root
	Industry    string           `json:"industry,omitempty"`
	Params      []platform.Field `json:"params"` // integer knobs with defaults in Description, besides the name
	build       func(b *builder, under string, name string, p Params) string
}

// Params are a pattern's integer knobs by name; missing ones take the default.
type Params map[string]int

func (p Params) get(name string, def int) int {
	if v, ok := p[name]; ok && v > 0 {
		return v
	}
	return def
}

// LevelNames are the backbone's levels, for palettes and previews.
var LevelNames = []string{"", "Enterprise", "Site", "Function", "Team"}

func knob(name, description string) platform.Field {
	return platform.Field{Name: name, Type: "integer", Description: description}
}

// Patterns is the catalogue, in level order.
func Patterns() []Pattern {
	out := []Pattern{
		{ID: "group", Title: "Group", Level: 1, Description: "A holding with a board and committees, group headquarters functions, and subsidiaries each shaped as a company.",
			Params: []platform.Field{knob("subsidiaries", "Subsidiaries (default 3)")}, build: buildGroup},
		{ID: "company", Title: "Company", Level: 1, Description: "A legal company with a general manager and the usual functions: operations, sales, finance, human resources.",
			Params: []platform.Field{knob("functions", "Functions (default 4)")}, build: buildCompany},
		{ID: "plant", Title: "Plant", Level: 2, Industry: "manufacturing", Description: "A factory: production, quality, maintenance, warehouse and planning; workshops with lines, stations and machines; raw material and finished goods warehouses.",
			Params: []platform.Field{knob("workshops", "Workshops (default 2)"), knob("lines", "Lines per workshop (default 2)"), knob("stations", "Stations per line (default 3)")}, build: buildPlant},
		{ID: "hotel", Title: "Hotel", Level: 2, Industry: "hospitality", Description: "A property: front office, housekeeping, food and beverage, engineering, sales; floors with rooms as locations.",
			Params: []platform.Field{knob("floors", "Floors (default 4)"), knob("rooms", "Rooms per floor (default 12)")}, build: buildHotel},
		{ID: "warehouse", Title: "Distribution centre", Level: 2, Industry: "logistics", Description: "A warehouse: inbound, outbound and inventory teams; zones, aisles and bins as locations; docks.",
			Params: []platform.Field{knob("zones", "Zones (default 3)"), knob("aisles", "Aisles per zone (default 4)"), knob("bins", "Bins per aisle (default 10)")}, build: buildWarehouse},
		{ID: "office", Title: "Office or branch", Level: 2, Description: "A sales or service branch: a manager, teams of staff, one office location.",
			Params: []platform.Field{knob("teams", "Teams (default 2)")}, build: buildOffice},
		{ID: "department", Title: "Department", Level: 3, Description: "A function with a manager post and staff posts; optionally teams below it.",
			Params: []platform.Field{knob("teams", "Teams (default 0)")}, build: buildDepartment},
		{ID: "shared-services", Title: "Shared services", Level: 3, Description: "A shared service centre: finance, human resources and IT serving the whole enterprise.",
			Params: []platform.Field{}, build: buildSharedServices},
		{ID: "line", Title: "Production line", Level: 4, Industry: "manufacturing", Description: "A line with a lead and operators, stations as locations and a machine at each.",
			Params: []platform.Field{knob("stations", "Stations (default 3)")}, build: buildLine},
		{ID: "team", Title: "Team", Level: 4, Description: "A team with a lead and staff posts.", Params: []platform.Field{}, build: buildTeam},
	}
	return out
}

// PatternByID finds a pattern.
func PatternByID(id string) (Pattern, bool) {
	for _, p := range Patterns() {
		if p.ID == id {
			return p, true
		}
	}
	return Pattern{}, false
}

// Preview is what a pattern would add with the given parameters, for people
// to see before they apply it.
type Preview struct {
	Elements      int      `json:"elements"`
	Relationships int      `json:"relationships"`
	Organisations int      `json:"organisations"`
	Posts         int      `json:"posts"`
	Locations     int      `json:"locations"`
	Resources     int      `json:"resources"`
	Outline       []string `json:"outline"` // the root and its first two levels, indented
}

// PatternPreview builds the pattern in a scratch model and summarises it.
func PatternPreview(id, name string, params Params) (Preview, bool) {
	pat, ok := PatternByID(id)
	if !ok {
		return Preview{}, false
	}
	b := newBuilder("", "2000-01-01", nil)
	b.kind("management", "Management", "management", false)
	if name == "" {
		name = pat.Title
	}
	root := pat.build(b, "", name, params)
	pv := Preview{Elements: len(b.m.Elements), Relationships: len(b.m.Relationships), Outline: []string{}}
	for _, e := range b.m.Elements {
		switch e.Stereotype {
		case Organization:
			pv.Organisations++
		case Post:
			pv.Posts++
		case Location:
			pv.Locations++
		case Resource:
			pv.Resources++
		}
	}
	var walk func(id string, depth int)
	walk = func(id string, depth int) {
		if depth > 2 || len(pv.Outline) > 40 {
			return
		}
		el := b.m.element(id)
		pv.Outline = append(pv.Outline, strings.Repeat("  ", depth)+el.Name)
		for _, c := range b.m.children(id, "management") {
			walk(c, depth+1)
		}
	}
	walk(root, 0)
	return pv, true
}

// children are the organisations placed directly under id in kind, in insertion order.
func (m Model) children(id, kind string) []string {
	var out []string
	for _, r := range m.Relationships {
		if r.Stereotype == Placement && r.Target == id && r.Kind == kind {
			out = append(out, r.Source)
		}
	}
	return out
}

// Apply grafts a pattern into the model under an organisation ("" for the top),
// on day. Ids never collide with what the model already holds.
func (m *Model) Apply(id, under, name string, params Params, day Date) (string, error) {
	pat, ok := PatternByID(id)
	if !ok {
		return "", fmt.Errorf("there is no pattern %q", id)
	}
	if name == "" {
		return "", fmt.Errorf("the %s needs a name", strings.ToLower(pat.Title))
	}
	if under != "" {
		parent := m.element(under)
		if parent == nil || parent.Stereotype != Organization {
			return "", fmt.Errorf("%q is not an organisation to place the %s under", under, strings.ToLower(pat.Title))
		}
	}
	b := newBuilder(m.Scale, day, m)
	if m.kind("management") == nil {
		b.kind("management", "Management", "management", false)
	}
	if m.kind("site") == nil {
		b.kind("site", "Sites", "site", false)
	}
	if pat.Level == 1 && m.kind("legal") == nil {
		b.kind("legal", "Legal", "legal", false)
	}
	root := pat.build(b, under, name, params)
	if under != "" {
		b.under("management", root, under)
		if m.element(under).Legal && b.m.element(root).Legal {
			b.under("legal", root, under)
		}
	}
	*m = b.m
	return root, nil
}

// --- Scale templates: compositions of the patterns.

type SeedParams struct {
	Scale         string // S, M, L, XL; empty: from Headcount
	Name          string
	Headcount     int
	Sites         int
	LegalEntities int
	Industry      string
	Day           Date
}

// ScaleOf picks the scale a headcount suggests.
func ScaleOf(headcount int) string {
	switch {
	case headcount <= 100:
		return "S"
	case headcount <= 1000:
		return "M"
	case headcount <= 10000:
		return "L"
	default:
		return "XL"
	}
}

// site is the level-2 pattern an industry runs.
func sitePattern(industry string) string {
	switch industry {
	case "hospitality":
		return "hotel"
	case "logistics", "trade":
		return "warehouse"
	case "services", "other":
		return "office"
	}
	return "plant"
}

// Template is the model a scale template produces for the parameters.
func Template(p SeedParams) (Model, error) {
	if p.Scale == "" {
		p.Scale = ScaleOf(p.Headcount)
	}
	if p.Sites < 1 {
		p.Sites = map[string]int{"S": 1, "M": 1, "L": 3, "XL": 6}[p.Scale]
	}
	if p.LegalEntities < 1 {
		p.LegalEntities = map[string]int{"S": 1, "M": 1, "L": 1, "XL": 4}[p.Scale]
	}
	b := newBuilder(p.Scale, p.Day, nil)
	b.kind("management", "Management", "management", false)
	b.kind("site", "Sites", "site", false)
	b.kind("legal", "Legal", "legal", false)
	site := sitePattern(p.Industry)
	build := func(id, under, name string, params Params) string {
		pat, _ := PatternByID(id)
		root := pat.build(b, under, name, params)
		if under != "" {
			b.under("management", root, under)
		}
		return root
	}
	switch p.Scale {
	case "S":
		co := build("company", "", p.Name, Params{"functions": 3})
		b.location(p.Name+" office", "office", "", co)
		for _, g := range []string{"Grow revenue", "Keep customers", "Stay profitable"} {
			b.goal(co, g)
		}
	case "M":
		co := build("company", "", p.Name, nil)
		build(site, co, p.Name+" "+siteWord(site), nil)
		for _, s := range []string{"ERP", "MES", "WMS"} {
			b.relate(Owns, "", co, b.element("System", s, s))
		}
		for _, g := range []string{"On-time delivery above 95%", "First-pass yield above 98%", "Zero lost-time accidents"} {
			b.goal(co, g)
		}
	case "L":
		b.kind("finance", "Cost centres", "finance", false)
		b.kind("project", "Projects", "project", true)
		co := build("company", "", p.Name, nil)
		hq := build("shared-services", co, "Shared services", nil)
		b.under("finance", hq, co)
		for i := 1; i <= p.Sites; i++ {
			bu := b.element(Organization, fmt.Sprintf("Business unit %c", 'A'+i-1), "business unit")
			b.under("management", bu, co)
			b.under("finance", bu, co)
			b.post(bu, "Business unit director", "executive")
			build(site, bu, fmt.Sprintf("%s %c %s", p.Name, 'A'+i-1, siteWord(site)), nil)
		}
		for _, s := range []string{"ERP", "MES", "WMS", "PLM", "CRM"} {
			b.relate(Owns, "", hq, b.element("System", s, s))
		}
		for _, pr := range []string{"Digital factory programme", "New product line launch"} {
			b.relate(Owns, "", co, b.element(Project, pr, "improvement"))
		}
		for _, g := range []string{"Double revenue in five years", "Operating margin above 12%", "Employee engagement above 80%"} {
			b.goal(co, g)
		}
	case "XL":
		b.kind("finance", "Cost centres", "finance", false)
		b.kind("governance", "Governance", "governance", false)
		b.kind("project", "Projects", "project", true)
		grp := build("group", "", p.Name, Params{"subsidiaries": p.LegalEntities})
		regions := []string{"North", "South", "East", "West", "Overseas"}
		subs := b.m.children(grp, "legal")
		for i, sub := range subs {
			region := b.location(regions[i%len(regions)]+" region", "region", "", sub)
			for j := 1; j <= max(1, p.Sites/len(subs)); j++ {
				before := len(b.m.Elements)
				build(site, sub, fmt.Sprintf("%s %d %s", b.m.element(sub).Name, j, siteWord(site)), nil)
				for _, e := range b.m.Elements[before:] {
					if e.Stereotype == Location && e.Kind == siteWord(site) {
						b.under("site", e.ID, region)
					}
				}
			}
		}
		for _, g := range []string{"Top three in every market served", "Return on capital above 10%", "Net zero by 2040"} {
			b.goal(grp, g)
		}
	default:
		return Model{}, fmt.Errorf("the scale %q is not S, M, L or XL", p.Scale)
	}
	for _, c := range capabilitiesFor(p.Scale, p.Industry) {
		b.capability(b.m.Elements[0].ID, c, "business")
	}
	b.view("Organisation", "Pr-Sr", b.org("")...)
	b.view("Capabilities", "St-Tx", b.ofStereotype(Capability)...)
	b.view("Sites and resources", "Rs-Sr", append(b.ofStereotype(Location), b.ofStereotype(Resource)...)...)
	if projects := b.ofStereotype(Project); len(projects) > 0 {
		b.view("Projects", "Pj-Rm", projects...)
	}
	return b.m, nil
}

func siteWord(pattern string) string {
	return map[string]string{"plant": "plant", "hotel": "hotel", "warehouse": "warehouse", "office": "office"}[pattern]
}

func capabilitiesFor(scale, industry string) []string {
	base := []string{"Sell and serve", "Manage talent", "Govern finance"}
	switch industry {
	case "hospitality":
		base = append([]string{"Host guests", "Run food and beverage", "Maintain the property"}, base...)
	case "logistics", "trade":
		base = append([]string{"Receive and store", "Pick and ship", "Plan inventory"}, base...)
	case "services", "other":
		base = append([]string{"Deliver services", "Support customers"}, base...)
	default:
		base = append([]string{"Manufacture", "Assure quality", "Maintain equipment", "Plan production", "Buy materials"}, base...)
	}
	if scale == "L" || scale == "XL" {
		base = append([]string{"Set strategy", "Allocate capital"}, base...)
	}
	return base
}

// --- The builder: elements and relationships with ids that never collide.

type builder struct {
	m   Model
	day Date
	ids map[string]int
}

func newBuilder(scale string, day Date, existing *Model) *builder {
	b := &builder{day: day, ids: map[string]int{}}
	if existing != nil {
		b.m = *existing
		for _, e := range b.m.Elements {
			b.ids[e.ID] = 1
		}
		for _, r := range b.m.Relationships {
			b.ids[r.ID] = 1
		}
	} else {
		b.m = Model{UAF: "1.3", Scale: scale, Kinds: []Kind{}, Elements: []Element{}, Relationships: []Relationship{}, Views: []View{}}
	}
	return b
}

func slug(s string) string {
	var out []rune
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			out = append(out, r)
		case r == ' ' || r == '-' || r == '_' || r == '/':
			if len(out) > 0 && out[len(out)-1] != '-' {
				out = append(out, '-')
			}
		}
	}
	if len(out) == 0 {
		return "x"
	}
	return strings.TrimRight(string(out), "-")
}

func (b *builder) id(prefix, name string) string {
	base := prefix + "-" + slug(name)
	for n := b.ids[base]; ; n++ {
		id := base
		if n > 0 {
			id = fmt.Sprintf("%s-%d", base, n+1)
		}
		if b.ids[id] == 0 {
			b.ids[base], b.ids[id] = n+1, 1
			return id
		}
	}
}

func (b *builder) kind(id, name, kind string, matrix bool) {
	b.m.Kinds = append(b.m.Kinds, Kind{ID: id, Name: name, Kind: kind, Matrix: matrix})
}

func (b *builder) element(stereotype, name, kind string) string {
	prefix := map[string]string{Organization: "org", Post: "post", Person: "person", Location: "loc", Resource: "res", Capability: "cap", Goal: "goal", Project: "prj", "System": "sys", "Responsibility": "resp"}[stereotype]
	if prefix == "" {
		prefix = slug(stereotype)
	}
	id := b.id(prefix, name)
	b.m.Elements = append(b.m.Elements, Element{ID: id, Stereotype: stereotype, Name: name, Kind: kind, From: b.day})
	return id
}

func (b *builder) legal(name, kind string) string {
	id := b.element(Organization, name, kind)
	b.m.Elements[len(b.m.Elements)-1].Legal = true
	return id
}

func (b *builder) relate(stereotype, kind, source, target string) string {
	id := b.id("rel", stereotype+"-"+source+"-"+target)
	b.m.Relationships = append(b.m.Relationships, Relationship{ID: id, Stereotype: stereotype, Kind: kind, Source: source, Target: target, From: b.day})
	return id
}

func (b *builder) under(kind, child, parent string) { b.relate(Placement, kind, child, parent) }

// unit adds an organisation under a parent in management.
func (b *builder) unit(parent, name, kind string) string {
	id := b.element(Organization, name, kind)
	if parent != "" {
		b.under("management", id, parent)
	}
	return id
}

func (b *builder) post(org, name, kind string) string {
	p := b.element(Post, name, kind)
	b.relate(Owns, "", org, p)
	return p
}

func (b *builder) capability(org, name, kind string) string {
	c := b.element(Capability, name, kind)
	b.relate(Performs, "", org, c)
	return c
}

func (b *builder) goal(org, name string) {
	b.relate(Owns, "", org, b.element(Goal, name, ""))
}

func (b *builder) location(name, kind, parent, owner string) string {
	l := b.element(Location, name, kind)
	if parent != "" {
		b.under("site", l, parent)
	}
	if owner != "" {
		b.relate(Owns, "", owner, l)
	}
	return l
}

func (b *builder) view(name, grid string, elements ...string) {
	b.m.Views = append(b.m.Views, View{ID: b.id("view", name), Name: name, Grid: grid, Elements: elements})
}

func (b *builder) org(kind string) []string {
	var out []string
	for _, e := range b.m.Elements {
		if e.Stereotype == Organization && (kind == "" || e.Kind == kind) {
			out = append(out, e.ID)
		}
	}
	return out
}

func (b *builder) ofStereotype(st string) []string {
	var out []string
	for _, e := range b.m.Elements {
		if e.Stereotype == st {
			out = append(out, e.ID)
		}
	}
	return out
}

// --- Level 1: enterprises.

func buildGroup(b *builder, _ string, name string, p Params) string {
	grp := b.legal(name, "group")
	b.post(grp, "Group chief executive", "executive")
	board := b.element(Organization, "Board of directors", "committee")
	if b.m.kind("governance") == nil {
		b.kind("governance", "Governance", "governance", false)
	}
	b.under("governance", grp, board)
	for _, c := range []string{"Audit committee", "Remuneration committee"} {
		b.under("governance", b.element(Organization, c, "committee"), board)
	}
	hq := b.unit(grp, "Group headquarters", "division")
	for _, f := range []string{"Group finance", "Group HR", "Group IT", "Strategy & M&A", "Legal & compliance", "Internal audit"} {
		d := b.unit(hq, f, "department")
		b.post(d, "Head of "+strings.ToLower(f), "manager")
	}
	kinds := []string{"Industrial", "Technology", "Trading", "Services", "Energy", "Logistics"}
	for i := 1; i <= p.get("subsidiaries", 3); i++ {
		sub := buildCompany(b, grp, fmt.Sprintf("%s %s", name, kinds[(i-1)%len(kinds)]), Params{"functions": 4})
		b.m.element(sub).Kind = "subsidiary"
		b.under("management", sub, grp)
		b.under("legal", sub, grp)
		b.m.Relationships[len(b.m.Relationships)-1].Share = 1
	}
	return grp
}

func buildCompany(b *builder, _ string, name string, p Params) string {
	co := b.legal(name, "company")
	b.post(co, "General manager", "executive")
	functions := []string{"Operations", "Sales & service", "Finance & admin", "Human resources", "Research & development", "Procurement"}
	n := p.get("functions", 4)
	if n > len(functions) {
		n = len(functions)
	}
	for _, f := range functions[:n] {
		d := b.unit(co, f, "department")
		b.post(d, f+" manager", "manager")
		b.post(d, f+" staff", "staff")
	}
	return co
}

// --- Level 2: sites.

func buildPlant(b *builder, _ string, name string, p Params) string {
	plant := b.element(Organization, name, "factory")
	b.post(plant, "Plant manager", "executive")
	loc := b.location(name, "plant", "", plant)
	var production string
	for _, d := range []string{"Production", "Quality", "Maintenance", "Warehouse & logistics", "Planning & purchasing"} {
		dept := b.unit(plant, d, "department")
		b.post(dept, d+" manager", "manager")
		if d == "Production" {
			production = dept
		}
	}
	for w := 1; w <= p.get("workshops", 2); w++ {
		shop := b.unit(production, fmt.Sprintf("Workshop %d", w), "workshop")
		b.post(shop, "Workshop supervisor", "supervisor")
		shopLoc := b.location(fmt.Sprintf("Workshop %d", w), "workshop", loc, shop)
		for l := 1; l <= p.get("lines", 2); l++ {
			line := buildLineAt(b, shop, shopLoc, fmt.Sprintf("Line %d-%d", w, l), p.get("stations", 3))
			_ = line
		}
	}
	b.location("Raw material warehouse", "warehouse", loc, plant)
	b.location("Finished goods warehouse", "warehouse", loc, plant)
	return plant
}

func buildHotel(b *builder, _ string, name string, p Params) string {
	hotel := b.element(Organization, name, "hotel")
	b.post(hotel, "General manager", "executive")
	loc := b.location(name, "property", "", hotel)
	for _, d := range []string{"Front office", "Housekeeping", "Food & beverage", "Engineering", "Sales & marketing"} {
		dept := b.unit(hotel, d, "department")
		b.post(dept, d+" manager", "manager")
		b.post(dept, d+" staff", "staff")
	}
	for f := 1; f <= p.get("floors", 4); f++ {
		floor := b.location(fmt.Sprintf("Floor %d", f), "floor", loc, hotel)
		for r := 1; r <= p.get("rooms", 12); r++ {
			b.location(fmt.Sprintf("Room %d%02d", f, r), "room", floor, "")
		}
	}
	for _, a := range []string{"Lobby", "Restaurant", "Kitchen", "Conference hall"} {
		b.location(a, "area", loc, hotel)
	}
	return hotel
}

func buildWarehouse(b *builder, _ string, name string, p Params) string {
	wh := b.element(Organization, name, "warehouse")
	b.post(wh, "Warehouse manager", "executive")
	loc := b.location(name, "warehouse", "", wh)
	for _, d := range []string{"Inbound", "Outbound", "Inventory control"} {
		team := b.unit(wh, d, "team")
		b.post(team, d+" supervisor", "supervisor")
		b.post(team, d+" operator", "operator")
	}
	for z := 1; z <= p.get("zones", 3); z++ {
		zone := b.location(fmt.Sprintf("Zone %c", 'A'+z-1), "zone", loc, wh)
		for a := 1; a <= p.get("aisles", 4); a++ {
			aisle := b.location(fmt.Sprintf("Aisle %c%02d", 'A'+z-1, a), "aisle", zone, "")
			for n := 1; n <= p.get("bins", 10); n++ {
				b.location(fmt.Sprintf("Bin %c%02d-%02d", 'A'+z-1, a, n), "bin", aisle, "")
			}
		}
	}
	for d := 1; d <= 2; d++ {
		b.location(fmt.Sprintf("Dock %d", d), "dock", loc, wh)
	}
	return wh
}

func buildOffice(b *builder, _ string, name string, p Params) string {
	office := b.element(Organization, name, "branch")
	b.post(office, "Branch manager", "manager")
	b.location(name, "office", "", office)
	for i := 1; i <= p.get("teams", 2); i++ {
		buildTeam(b, office, fmt.Sprintf("Team %d", i), nil)
	}
	return office
}

// --- Level 3: functions.

func buildDepartment(b *builder, _ string, name string, p Params) string {
	d := b.element(Organization, name, "department")
	b.post(d, name+" manager", "manager")
	b.post(d, name+" staff", "staff")
	for i := 1; i <= p.get("teams", 0); i++ {
		buildTeam(b, d, fmt.Sprintf("%s team %d", name, i), nil)
	}
	return d
}

func buildSharedServices(b *builder, _ string, name string, _ Params) string {
	ssc := b.element(Organization, name, "division")
	b.post(ssc, "Head of shared services", "executive")
	for _, f := range []string{"Finance", "Human resources", "IT"} {
		d := b.unit(ssc, f, "department")
		b.post(d, "Head of "+strings.ToLower(f), "manager")
	}
	return ssc
}

// --- Level 4: teams.

func buildLine(b *builder, under string, name string, p Params) string {
	// the line's stations sit in the parent's location when it has one
	var at string
	if under != "" {
		if locs := b.m.Of(under, Owns, true, b.day); len(locs) > 0 {
			for _, l := range locs {
				if e := b.m.element(l); e != nil && e.Stereotype == Location {
					at = l
					break
				}
			}
		}
	}
	return buildLineAt(b, "", at, name, p.get("stations", 3))
}

func buildLineAt(b *builder, shop, shopLoc, name string, stations int) string {
	line := b.element(Organization, name, "line")
	if shop != "" {
		b.under("management", line, shop)
	}
	b.post(line, "Line lead", "supervisor")
	b.post(line, "Operator", "operator")
	lineLoc := b.location(name, "line", shopLoc, line)
	for s := 1; s <= stations; s++ {
		st := b.location(fmt.Sprintf("%s station %d", name, s), "station", lineLoc, line)
		mach := b.element(Resource, fmt.Sprintf("%s machine %d", name, s), "machine")
		b.relate(Owns, "", line, mach)
		b.relate(Placement, "site", mach, st)
	}
	return line
}

func buildTeam(b *builder, under string, name string, _ Params) string {
	team := b.element(Organization, name, "team")
	if under != "" {
		b.under("management", team, under)
	}
	b.post(team, name+" lead", "supervisor")
	b.post(team, name+" member", "staff")
	return team
}
