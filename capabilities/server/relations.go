package platformserver

import (
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// Relations is the platform's links and timeline (ADR-0011): relations between
// any two entities of any apps, and the activity about an entity, so no app —
// and no bridge — has to own them. A member may link or annotate entities of
// apps it holds a role in, and sees only those. Protocol events are told on the
// timeline of the entity they concern and of every entity linked to it.
const (
	RelationsApp = "relations"
	LinkType     = "platform.link"
	NoteType     = "platform.note"
	SchemaLink   = "platform.link"
	SchemaUnlink = "platform.unlink"
	SchemaNote   = "platform.note"
)

type Link struct {
	From string    `json:"from"` // "<type>/<id>"
	To   string    `json:"to"`
	By   string    `json:"by"`
	At   time.Time `json:"at"`
}

type Note struct {
	Entity string    `json:"entity"`
	By     string    `json:"by"`
	At     time.Time `json:"at"`
	Text   string    `json:"text"`
}

type Relations struct {
	t      *Tenant // the tenant running it, once composed (NewTenant)
	mu     sync.Mutex
	links  []Link
	notes  []Note
	ledger *platform.Ledger
}

func NewRelations(tenant string) *Relations {
	any := []string{platform.AnyMember}
	ref := func(name string) platform.Field {
		return platform.Field{Name: name, Type: "entity", Required: true, Description: "<type>/<id> of an entity of an app you hold a role in"}
	}
	return &Relations{ledger: platform.NewLedger(tenant, RelationsApp, platform.NewCatalog(
		platform.Action{Schema: SchemaLink, Target: LinkType, Capability: "links", Title: "Link entities",
			Description: "Relate two entities, of the same or of different apps.", Payload: []platform.Field{ref("from"), ref("to")}, Roles: any},
		platform.Action{Schema: SchemaUnlink, Target: LinkType, Capability: "links", Title: "Unlink entities",
			Description: "Remove the relation between two entities.", Payload: []platform.Field{ref("from"), ref("to")}, Roles: any},
		platform.Action{Schema: SchemaNote, Target: NoteType, Capability: "timeline", Title: "Add note",
			Description: "Add a note to an entity's activity timeline.",
			Payload:     []platform.Field{ref("entity"), {Name: "text", Type: "string", Required: true, Description: "What happened"}}, Roles: any},
	), LinkType, NoteType)}
}

func (r *Relations) Manifest() platform.Manifest {
	return platform.Manifest{ID: RelationsApp, Title: "Relations", Version: "1", Actions: r.ledger.Catalog, Reads: []string{"links", "timeline"}, Everyone: []string{"links", "timeline"}}
}

func (r *Relations) Declarations() []*pb.AuthorityDeclaration { return r.ledger.Declarations() }

func (r *Relations) Input(platform.Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}

// sees reports whether c holds a role in the app owning entity "<type>/<id>".
func (r *Relations) sees(c platform.Caller, entity string) bool {
	class, _, ok := strings.Cut(entity, "/")
	if !ok || r.t == nil {
		return false
	}
	for _, a := range r.t.apps {
		if slices.ContainsFunc(a.Declarations(), func(d *pb.AuthorityDeclaration) bool { return d.GetDataClass() == class }) {
			return c.Roles[a.Manifest().ID] != ""
		}
	}
	return false
}

func (r *Relations) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var p struct{ From, To, Entity, Text string }
	json.Unmarshal(s.GetPayload(), &p)
	entities := []string{p.Entity}
	if s.GetSchema().GetName() != SchemaNote {
		entities = []string{p.From, p.To}
	}
	allowed := func() bool {
		return !slices.ContainsFunc(entities, func(e string) bool { return !r.sees(c, e) })
	}
	return r.ledger.Receive(c, s, now, allowed, func() (func(*pb.ChangeRecord), *kernel.Error) {
		invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		if slices.ContainsFunc(entities, func(e string) bool { return !strings.Contains(e, "/") }) {
			return nil, invalid
		}
		linked := slices.IndexFunc(r.links, func(l Link) bool { return l.From == p.From && l.To == p.To })
		switch s.GetSchema().GetName() {
		case SchemaLink:
			if linked >= 0 {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
			}
			return func(rec *pb.ChangeRecord) {
				r.links = append(r.links, Link{From: p.From, To: p.To, By: c.ID, At: rec.GetRecordedTime().AsTime()})
			}, nil
		case SchemaUnlink:
			if linked < 0 {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
			}
			return func(*pb.ChangeRecord) { r.links = slices.Delete(r.links, linked, linked+1) }, nil
		}
		if strings.TrimSpace(p.Text) == "" {
			return nil, invalid
		}
		return func(rec *pb.ChangeRecord) {
			r.notes = append(r.notes, Note{Entity: p.Entity, By: c.ID, At: rec.GetRecordedTime().AsTime(), Text: p.Text})
		}, nil
	})
}

// Read "links" or "timeline" (newest last), limited to entities the caller sees.
func (r *Relations) Read(c platform.Caller, name string) (any, *kernel.Error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if name == "links" {
		out := []Link{}
		for _, l := range r.links {
			if r.sees(c, l.From) && r.sees(c, l.To) {
				out = append(out, l)
			}
		}
		return out, nil
	}
	out := []Note{}
	for _, n := range r.notes {
		if r.sees(c, n.Entity) {
			out = append(out, n)
		}
	}
	return out, nil
}

// observe tells protocol events on the timeline of their entity and of the
// entities linked to it. It runs during delivery, so replay tells them again.
func (r *Relations) observe(t *Tenant, e platform.Event, events []string) {
	s := e.Record.GetSubmission()
	entity := s.GetTarget().GetType() + "/" + s.GetTarget().GetId()
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, name := range events {
		declared, _ := t.protocolEvent(name)
		text := declared.Title + " (" + entity + ", by " + s.GetPrincipalId() + ")"
		about := []string{entity}
		for _, l := range r.links {
			if l.From == entity && !slices.Contains(about, l.To) {
				about = append(about, l.To)
			}
			if l.To == entity && !slices.Contains(about, l.From) {
				about = append(about, l.From)
			}
		}
		for _, x := range about {
			r.notes = append(r.notes, Note{Entity: x, By: "app:" + e.App, At: e.Record.GetRecordedTime().AsTime(), Text: text})
		}
	}
}

// linksOf are the entities linked to entity that c sees (Caller.Links).
func (t *Tenant) linksOf(c platform.Caller, entity string) []string {
	r := t.relations
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, l := range r.links {
		switch {
		case l.From == entity && r.sees(c, l.To):
			out = append(out, l.To)
		case l.To == entity && r.sees(c, l.From):
			out = append(out, l.From)
		}
	}
	return out
}

// link relates two entities for c (Caller.Link).
func (t *Tenant) link(c platform.Caller, from, to *pb.EntityRef, key string, now time.Time) *kernel.Error {
	if t.relations == nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	payload, _ := json.Marshal(map[string]string{"from": from.GetType() + "/" + from.GetId(), "to": to.GetType() + "/" + to.GetId()})
	called := platform.NewCaller(runtime{t}, c.Member, RelationsApp, c.Replaying, c.Automation)
	_, err := t.relations.Submit(called, &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: RelationsApp,
		Target: &pb.EntityRef{Type: LinkType, Id: key}, Schema: &pb.SchemaRef{Name: SchemaLink, Version: 1}, IdempotencyKey: key, Payload: payload}, now)
	return err
}
