package platform

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"strings"
	"time"
)

const (
	RelationsApp     = "relations"
	RelationLinkType = "platform.link"
	CommentType      = "platform.comment"
	FollowType       = "platform.follow"
)

// Comment is a member's comment on a record; @member mentions notify them.
type RecordComment struct {
	Record
	Target   string   `json:"target" field:"readonly" title:"On"`
	Text     string   `json:"text" field:"required,search" type:"longtext"`
	By       string   `json:"by" field:"readonly"`
	Mentions []string `json:"mentions,omitempty" field:"readonly"`
}

// Follow is a member following a record: they hear of its decisions and comments.
type RecordFollow struct {
	Record
	Member string `json:"member" field:"readonly"`
	Target string `json:"target" field:"readonly" title:"Follows"`
}

// FollowID is the one follow of member on target.
func FollowID(member, target string) string {
	return member + "@" + strings.ReplaceAll(target, "/", "~")
}

type RelationLink struct {
	From string    `json:"from"` // "<type>/<id>"
	To   string    `json:"to"`
	By   string    `json:"by"`
	At   time.Time `json:"at"`
}

// Note is one protocol event on an entity's timeline, told by the app that decided it.
type RelationNote struct {
	Entity string    `json:"entity"`
	By     string    `json:"by"`
	At     time.Time `json:"at"`
	Title  string    `json:"title,omitempty"` // a protocol event's title, said in the reader's language (F-37)
	Text   string    `json:"text"`
}

// Links are the entities linked to entity ("<type>/<id>") that c sees (ADR-0011).
func (c Caller) Links(entity string) []string {
	if c.rt == nil {
		return nil
	}
	return c.rt.Links(c, entity)
}

// Link relates two entities for c (an app linking what it just did, for example).
func (c Caller) Link(from, to *pb.EntityRef, key string, now time.Time) *kernel.Error {
	if c.rt == nil {
		return notFound()
	}
	return c.rt.Link(c, from, to, key, now)
}
