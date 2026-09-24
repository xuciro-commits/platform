package platformserver

import (
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
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
	mu     sync.Mutex
	links  []Link
	notes  []Note
	ledger *Ledger
}

func NewRelations(tenant string) *Relations {
	any := []string{AnyMember}
	ref := func(name string) Field {
		return Field{Name: name, Type: "entity", Required: true, Description: "<type>/<id> of an entity of an app you hold a role in"}
	}
	return &Relations{ledger: NewLedger(tenant, RelationsApp, NewCatalog(
		Action{Schema: SchemaLink, Target: LinkType, Capability: "links", Title: "Link entities",
			Description: "Relate two entities, of the same or of different apps.", Payload: []Field{ref("from"), ref("to")}, Roles: any},
		Action{Schema: SchemaUnlink, Target: LinkType, Capability: "links", Title: "Unlink entities",
			Description: "Remove the relation between two entities.", Payload: []Field{ref("from"), ref("to")}, Roles: any},
		Action{Schema: SchemaNote, Target: NoteType, Capability: "timeline", Title: "Add note",
			Description: "Add a note to an entity's activity timeline.",
			Payload:     []Field{ref("entity"), {Name: "text", Type: "string", Required: true, Description: "What happened"}}, Roles: any},
	), LinkType, NoteType)}
}

func (r *Relations) Manifest() Manifest {
	return Manifest{ID: RelationsApp, Version: "1", Actions: r.ledger.Catalog, Reads: []string{"links", "timeline"}}
}

func (r *Relations) Declarations() []*pb.AuthorityDeclaration { return r.ledger.Declarations() }

func (r *Relations) Input(Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}

// sees reports whether c holds a role in the app owning entity "<type>/<id>".
func (r *Relations) sees(c Caller, entity string) bool {
	class, _, ok := strings.Cut(entity, "/")
	if !ok || c.tenant == nil {
		return false
	}
	for _, a := range c.tenant.apps {
		if slices.ContainsFunc(a.Declarations(), func(d *pb.AuthorityDeclaration) bool { return d.GetDataClass() == class }) {
			return c.Roles[a.Manifest().ID] != ""
		}
	}
	return false
}

func (r *Relations) Submit(c Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
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
func (r *Relations) Read(c Caller, name string) (any, *kernel.Error) {
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
func (r *Relations) observe(t *Tenant, e Event, events []string) {
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

// Links are the entities linked to entity ("<type>/<id>") that c sees.
func (c Caller) Links(entity string) []string {
	if c.tenant == nil || c.tenant.relations == nil {
		return nil
	}
	r := c.tenant.relations
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

// Link relates two entities for c (an app linking what it just did, for example).
func (c Caller) Link(from, to *pb.EntityRef, key string, now time.Time) *kernel.Error {
	if c.tenant == nil || c.tenant.relations == nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	payload, _ := json.Marshal(map[string]string{"from": from.GetType() + "/" + from.GetId(), "to": to.GetType() + "/" + to.GetId()})
	called := c.tenant.caller(c.Member, c.tenant.relations, c.Replaying)
	called.Automation = c.Automation
	_, err := c.tenant.relations.Submit(called, &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: RelationsApp,
		Target: &pb.EntityRef{Type: LinkType, Id: key}, Schema: &pb.SchemaRef{Name: SchemaLink, Version: 1}, IdempotencyKey: key, Payload: payload}, now)
	return err
}
