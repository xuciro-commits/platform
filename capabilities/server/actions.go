package platformserver

import "slices"

// Action declares one business action a domain offers (ADR-0008): the submission
// schema that carries it, what it acts on, the capability it belongs to, and a
// description that screens and automation callers (AI agents included) read.
// Roles say who may call it; attribute conditions stay in the domain's policy.
type Action struct {
	Schema      string   `json:"schema"`
	Target      string   `json:"target"`
	Capability  string   `json:"capability"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Payload     []Field  `json:"payload"`
	Roles       []string `json:"-"`
	// Uses names other apps' actions this one invokes; a caller is offered it
	// only when it may call those too (ADR-0009).
	Uses []string `json:"uses,omitempty"`
}

// Field describes one payload field of an action.
type Field struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required,omitempty"`
	Description string `json:"description"`
}

// Catalog holds a domain's actions and which capabilities are deactivated for a
// deployment (start-up configuration, not runtime installation).
type Catalog struct {
	actions  []Action
	disabled map[string]bool
}

func NewCatalog(actions ...Action) *Catalog {
	return &Catalog{actions: actions, disabled: map[string]bool{}}
}

// Disable deactivates a capability; false if no action belongs to it.
func (c *Catalog) Disable(capability string) bool {
	if !slices.ContainsFunc(c.actions, func(a Action) bool { return a.Capability == capability }) {
		return false
	}
	c.disabled[capability] = true
	return true
}

// Action is the declaration of schema, active or not.
func (c *Catalog) Action(schema string) (Action, bool) {
	i := slices.IndexFunc(c.actions, func(a Action) bool { return a.Schema == schema })
	if i < 0 {
		return Action{}, false
	}
	return c.actions[i], true
}

// Enabled reports whether schema is a declared action of an active capability.
func (c *Catalog) Enabled(schema string) bool {
	i := slices.IndexFunc(c.actions, func(a Action) bool { return a.Schema == schema })
	return i >= 0 && !c.disabled[c.actions[i].Capability]
}

// AnyMember in an action's Roles offers it to every member; the app's attribute
// conditions then decide (the platform's links and timeline check the entities' apps).
const AnyMember = "*"

// Permits reports whether role may call the enabled action schema.
func (c *Catalog) Permits(role, schema string) bool {
	return slices.ContainsFunc(c.For(role), func(a Action) bool { return a.Schema == schema })
}

// For is the catalog a caller with role receives: enabled actions it may call.
func (c *Catalog) For(role string) []Action {
	out := []Action{}
	for _, a := range c.actions {
		if !c.disabled[a.Capability] && (slices.Contains(a.Roles, role) || slices.Contains(a.Roles, AnyMember)) {
			out = append(out, a)
		}
	}
	return out
}

// Roles are the roles an app defines: every role some action grants.
func (c *Catalog) Roles() []string {
	var out []string
	for _, a := range c.actions {
		for _, r := range a.Roles {
			if !slices.Contains(out, r) {
				out = append(out, r)
			}
		}
	}
	slices.Sort(out)
	return out
}

// CapabilityInfo is one capability of an app: its actions and whether it is active.
type CapabilityInfo struct {
	Name    string   `json:"name"`
	Enabled bool     `json:"enabled"`
	Actions []string `json:"actions"`
}

func (c *Catalog) Capabilities() []CapabilityInfo {
	var out []CapabilityInfo
	for _, a := range c.actions {
		i := slices.IndexFunc(out, func(x CapabilityInfo) bool { return x.Name == a.Capability })
		if i < 0 {
			out = append(out, CapabilityInfo{Name: a.Capability, Enabled: !c.disabled[a.Capability]})
			i = len(out) - 1
		}
		out[i].Actions = append(out[i].Actions, a.Schema)
	}
	return out
}
