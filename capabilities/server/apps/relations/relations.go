// Package relations is the platform's links and timeline (ADR-0011), a
// platform app on the app API and internal/host (ADR-0025 D4).
package relations

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/host"
	"platformserver/platform"
)

// Relations is the platform's links and timeline (ADR-0011): relations between
// any two entities of any apps, and the activity about an entity, so no app —
// and no bridge — has to own them. A member may link or annotate entities of
// apps it holds a role in, and sees only those. Protocol events are told on the
// timeline of the entity they concern and of every entity linked to it.
const (
	ID           = "relations"
	LinkType     = "platform.link"
	NoteType     = "platform.note"
	SchemaLink   = "platform.link"
	SchemaUnlink = "platform.unlink"
	SchemaNote   = "platform.note"
	// Comments and followers on any record (ADR-0028 D6).
	CommentType    = "platform.comment"
	FollowType     = "platform.follow"
	SchemaComment  = "platform.comment.add"
	SchemaFollow   = "platform.follow.add"
	SchemaUnfollow = "platform.follow.remove"
)

// Comment is a member's comment on a record; @member mentions notify them.
type Comment struct {
	platform.Record
	Target   string   `json:"target" field:"readonly" title:"On"`
	Text     string   `json:"text" field:"required,search" type:"longtext"`
	By       string   `json:"by" field:"readonly"`
	Mentions []string `json:"mentions,omitempty" field:"readonly"`
}

// Follow is a member following a record: they hear of its decisions and comments.
type Follow struct {
	platform.Record
	Member string `json:"member" field:"readonly"`
	Target string `json:"target" field:"readonly" title:"Follows"`
}

// FollowID is the one follow of member on target.
func FollowID(member, target string) string {
	return member + "@" + strings.ReplaceAll(target, "/", "~")
}

var mention = regexp.MustCompile(`@([A-Za-z0-9][A-Za-z0-9._-]*)`)

func entities() []platform.Entity {
	return []platform.Entity{
		{Type: CommentType, Title: "Comment", Model: Comment{}, Display: "text", Description: "A member's comment on a record; readable exactly when the record is.",
			Scope: platform.Scope{Through: func(record any) string { return record.(Comment).Target }}},
		{Type: FollowType, Title: "Follow", Model: Follow{}, Description: "A member following a record, told of its changes and comments.",
			Scope: platform.Scope{Through: func(record any) string { return record.(Follow).Target }}},
	}
}

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
	host   host.Host // the tenant running it, once composed
	mu     sync.Mutex
	links  []Link
	notes  []Note
	ledger *platform.Ledger
}

func New(tenant string) *Relations {
	any := []string{platform.AnyMember}
	ref := func(name string) platform.Field {
		return platform.Field{Name: name, Type: "entity", Required: true, Description: "<type>/<id> of an entity of an app you hold a role in"}
	}
	return &Relations{ledger: platform.NewLedger(tenant, ID, platform.NewCatalog(
		platform.Action{Schema: SchemaLink, Target: LinkType, Capability: "links", Title: "Link entities",
			Description: "Relate two entities, of the same or of different apps.", Payload: []platform.Field{ref("from"), ref("to")}, Roles: any},
		platform.Action{Schema: SchemaUnlink, Target: LinkType, Capability: "links", Title: "Unlink entities",
			Description: "Remove the relation between two entities.", Payload: []platform.Field{ref("from"), ref("to")}, Roles: any},
		platform.Action{Schema: SchemaNote, Target: NoteType, Capability: "timeline", Title: "Add note",
			Description: "Add a note to an entity's activity timeline.",
			Payload:     []platform.Field{ref("entity"), {Name: "text", Type: "string", Required: true, Description: "What happened"}}, Roles: any},
		platform.Action{Schema: SchemaComment, Target: CommentType, New: true, Capability: "comments", Title: "Comment",
			Description: "Comment on a record you may read; @member tells them. You then follow the record.",
			Payload:     []platform.Field{ref("target"), {Name: "text", Type: "string", Required: true, Description: "The comment"}}, Roles: any},
		platform.Action{Schema: SchemaFollow, Target: FollowType, Capability: "comments", Title: "Follow",
			Description: "Be told of a record's changes and comments.", Payload: []platform.Field{ref("target")}, Roles: any},
		platform.Action{Schema: SchemaUnfollow, Target: FollowType, Capability: "comments", Title: "Unfollow",
			Description: "Stop being told of a record's changes and comments.", Payload: []platform.Field{}, Roles: any},
	), LinkType, NoteType, CommentType, FollowType)}
}

// Snapshot and Restore: links and notes (ADR-0019 D6).
type relationsState struct {
	Links []Link `json:"links"`
	Notes []Note `json:"notes"`
}

func (r *Relations) Snapshot() (json.RawMessage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ledger.SnapshotWith(relationsState{r.links, r.notes})
}

func (r *Relations) Restore(raw json.RawMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var s relationsState
	if err := r.ledger.RestoreWith(raw, &s); err != nil {
		return err
	}
	r.links, r.notes = s.Links, s.Notes
	return nil
}

func (r *Relations) Manifest() platform.Manifest {
	return platform.Manifest{ID: ID, Title: "Relations", Version: "1", Actions: r.ledger.Catalog, Reads: []string{"links", "timeline"}, Everyone: []string{"links", "timeline"},
		Entities: entities()}
}

func (r *Relations) Declarations() []*pb.AuthorityDeclaration { return r.ledger.Declarations() }

func (r *Relations) Input(platform.Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}

// sees reports whether c holds a role in the app owning entity "<type>/<id>".
func (r *Relations) sees(c platform.Caller, entity string) bool {
	class, _, ok := strings.Cut(entity, "/")
	if !ok || r.host == nil {
		return false
	}
	app, ok := r.host.OwnerOf(class)
	return ok && c.Roles[app] != ""
}

// Attach is the host handing itself to the app (host.Attached).
func (r *Relations) Attach(h host.Host) { r.host = h }

func (r *Relations) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	switch s.GetSchema().GetName() {
	case SchemaComment, SchemaFollow, SchemaUnfollow:
		return r.collaborate(c, s, now)
	}
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

// Observe tells a record's followers of each member's decision on it, and
// protocol events on the timeline of their entity and of the entities linked
// to it (host.Observer). It runs inside the input, so replay
// tells them again.
func (r *Relations) Observe(e platform.Event, events []string) {
	s := e.Record.GetSubmission()
	entity := s.GetTarget().GetType() + "/" + s.GetTarget().GetId()
	if t := s.GetTarget().GetType(); t != CommentType && t != FollowType && t != NoteType && t != LinkType && !strings.HasPrefix(s.GetPrincipalId(), "app:") {
		// A member's decision on a record tells its followers (ADR-0028 D6).
		r.tell(r.host.Automation(ID, false), entity, entity+" changed: "+s.GetSchema().GetName(), "by "+s.GetPrincipalId(),
			"change:"+e.App+"/"+e.Record.GetChangeId(), []string{s.GetPrincipalId()}, e.Record.GetRecordedTime().AsTime())
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, name := range events {
		declared, _ := r.host.ProtocolEvent(name)
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

// Links are the entities linked to entity that c sees (Caller.Links, host.Linker).
func (r *Relations) Links(c platform.Caller, entity string) []string {
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

// Link relates two entities for c (Caller.Link, host.Linker), as a decision of
// this app taken for c.
func (r *Relations) Link(c platform.Caller, from, to *pb.EntityRef, key string, now time.Time) *kernel.Error {
	payload, _ := json.Marshal(map[string]string{"from": from.GetType() + "/" + from.GetId(), "to": to.GetType() + "/" + to.GetId()})
	_, err := r.Submit(r.host.As(c, ID), &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: ID,
		Target: &pb.EntityRef{Type: LinkType, Id: key}, Schema: &pb.SchemaRef{Name: SchemaLink, Version: 1}, IdempotencyKey: key, Payload: payload}, now)
	return err
}

// collaborate decides comments and follows (ADR-0028 D6): on a record the
// member may read, which a replay does not ask again.
func (r *Relations) collaborate(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	var p struct{ Target, Text string }
	json.Unmarshal(s.GetPayload(), &p)
	return r.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		id := s.GetTarget().GetId()
		if s.GetSchema().GetName() == SchemaUnfollow {
			f, ok := platform.Get[Follow](c, id)
			if !ok || f.Archived || f.Member != c.ID && !c.Replaying {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
			}
			f.Archived = true
			return func(rec *pb.ChangeRecord) { c.Put(rec, f) }, nil
		}
		if !strings.Contains(p.Target, "/") || !c.Replaying && !r.host.Readable(c.Member, p.Target, now) {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		follow := Follow{Record: platform.Record{ID: FollowID(c.ID, p.Target)}, Member: c.ID, Target: p.Target}
		existing, following := platform.Get[Follow](c, follow.ID)
		following = following && !existing.Archived
		if s.GetSchema().GetName() == SchemaFollow {
			if s.GetTarget().GetId() != follow.ID {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
			}
			if following {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
			}
			return func(rec *pb.ChangeRecord) { c.Put(rec, follow) }, nil
		}
		text := strings.TrimSpace(p.Text)
		if text == "" {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		}
		if _, known := platform.Get[Comment](c, id); known {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
		comment := Comment{Record: platform.Record{ID: id}, Target: p.Target, Text: text, By: c.ID}
		for _, m := range mention.FindAllStringSubmatch(text, -1) {
			if _, ok := r.host.Member(m[1]); ok && m[1] != c.ID && !slices.Contains(comment.Mentions, m[1]) {
				comment.Mentions = append(comment.Mentions, m[1])
			}
		}
		return func(rec *pb.ChangeRecord) {
			c.Put(rec, comment)
			if !following {
				c.Put(rec, follow)
			}
			var to []platform.Recipient
			for _, m := range comment.Mentions {
				to = append(to, platform.Recipient{Member: m})
			}
			c.Notify(platform.Notification{Title: c.ID + " mentioned you on " + p.Target, Body: text, Ref: p.Target, Key: "mention:" + id}, now, to...)
			r.tell(c, p.Target, "New comment on "+p.Target, text, "comment:"+id, append(comment.Mentions, c.ID), now)
		}, nil
	})
}

// tell notifies a record's followers, except those in skip.
func (r *Relations) tell(c platform.Caller, target, title, body, key string, skip []string, now time.Time) {
	domain, _ := json.Marshal([]any{[]any{"target", "=", target}})
	follows, _, _ := platform.Find[Follow](r.host.Automation(ID, c.Replaying), platform.Query{Domain: domain, Limit: 1000})
	var to []platform.Recipient
	for _, f := range follows {
		if !slices.Contains(skip, f.Member) {
			to = append(to, platform.Recipient{Member: f.Member})
		}
	}
	if len(to) > 0 {
		r.host.Automation(ID, c.Replaying).Notify(platform.Notification{Title: title, Body: body, Ref: target, Key: key}, now, to...)
	}
}
