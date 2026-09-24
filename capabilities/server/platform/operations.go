package platform

import (
	"slices"
	"strconv"
	"time"

	"platformkernel/kernel"
)

// What the host operates for apps (ADR-0013): scheduled jobs, typed settings,
// notifications and connector deliveries (K8).

// Job is work an app runs on a schedule, as "app:<id>" (manifest).
type Job struct {
	Name  string        `json:"name"`
	Title string        `json:"title"`
	Every time.Duration `json:"every"`
}

// Setting is a typed per-tenant value an app declares and administrators set in
// Settings; a value within the app's rules, never a rule (ADR-0008 point 2).
type Setting struct {
	Name        string   `json:"name"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Type        string   `json:"type"` // boolean, integer, text, choice
	Default     string   `json:"default"`
	Choices     []string `json:"choices,omitempty"`
}

// Accepts reports whether v is a value of the setting's type.
func (s Setting) Accepts(v string) bool {
	switch s.Type {
	case "boolean":
		return v == "true" || v == "false"
	case "integer":
		_, err := strconv.Atoi(v)
		return err == nil
	case "choice":
		return slices.Contains(s.Choices, v)
	}
	return s.Type == "text"
}

// Setting is the current value of the calling app's setting (its default until set).
func (c Caller) Setting(name string) string {
	if c.rt == nil {
		return ""
	}
	return c.rt.Setting(c, name)
}

// Recipient is a member; whoever holds a membership (with Role, when given) in
// Unit or in a unit above it in Structure (ADR-0012); or whoever holds AppRole
// in the notifying app.
type Recipient struct {
	Member                string
	Structure, Unit, Role string
	AppRole               string
}

type Notification struct {
	ID     string    `json:"id"`
	Member string    `json:"member"`
	App    string    `json:"app"`
	Title  string    `json:"title"`
	Body   string    `json:"body,omitempty"`
	Ref    string    `json:"ref,omitempty"` // the entity it is about, "<type>/<id>"
	Key    string    `json:"key,omitempty"` // one notification per member, app and key
	At     time.Time `json:"at"`
	Read   bool      `json:"read"`
}

// Notify resolves recipients on now's day and gives each one n, unless it has
// one with n's key from this app already; it returns who received it.
func (c Caller) Notify(n Notification, now time.Time, to ...Recipient) []string {
	if c.rt == nil {
		return nil
	}
	return c.rt.Notify(c, n, now, to)
}

// Deliver accepts one batch from the calling connector about dataClass; a poll
// page moves the cursor from → to (K8).
func (c Caller) Deliver(dataClass, from, to string, now time.Time) *kernel.Error {
	if c.rt == nil {
		return notFound()
	}
	return c.rt.Deliver(c, dataClass, from, to, now)
}
