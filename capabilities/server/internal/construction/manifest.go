package construction

import (
	"fmt"
	"slices"
	"strings"

	"platformserver/platform"
)

// CheckManifest validates one application declaration. The host supplies only
// whether the composed owner implements the original job role, never the owner
// or a tenant callback.
func CheckManifest(m platform.Manifest, runsJobs bool) error {
	if m.ID == "" || m.Actions == nil {
		return fmt.Errorf("manifest without ID or action catalog")
	}
	for _, action := range m.Actions.All() {
		if action.Title == "" || action.Description == "" || action.Target == "" || action.Payload == nil || len(action.Roles) == 0 && !action.Automation {
			return fmt.Errorf("action %s lacks a title, description, target, payload fields or roles", action.Schema)
		}
	}
	if len(m.Jobs) > 0 && !runsJobs {
		return fmt.Errorf("declares jobs but has no Run")
	}
	for _, j := range m.Jobs {
		if j.Name == "" || j.Every <= 0 {
			return fmt.Errorf("job %q needs a name and a positive interval", j.Name)
		}
	}
	for i, s := range m.Settings {
		if s.Name == "" || !s.Accepts(s.Default) || slices.ContainsFunc(m.Settings[:i], func(x platform.Setting) bool { return x.Name == s.Name }) {
			return fmt.Errorf("setting %q: unnamed, repeated, or its default is not a %s", s.Name, s.Type)
		}
	}
	for i, s := range m.Sequences {
		if err := s.Check(); err != nil || slices.ContainsFunc(m.Sequences[:i], func(x platform.Sequence) bool { return x.Name == s.Name }) {
			return fmt.Errorf("sequence %q: badly declared or repeated", s.Name)
		}
	}
	for i, e := range m.Emits {
		if e.Name == "" || strings.Contains(e.Name, "/") || slices.ContainsFunc(m.Emits[:i], func(x platform.EffectKind) bool { return x.Name == e.Name }) {
			return fmt.Errorf("effect kind %q: unnamed, repeated or containing '/'", e.Name)
		}
	}
	for _, action := range m.Actions.All() {
		for _, used := range action.Uses {
			protocol, _, ok := strings.Cut(used, "#")
			if !ok || !slices.ContainsFunc(m.Consumes, func(c platform.Consumption) bool { return c.Protocol == protocol }) {
				return fmt.Errorf("action %s uses %s, not an action of a protocol it consumes", action.Schema, used)
			}
		}
	}
	for _, r := range m.Everyone {
		if !slices.Contains(m.Reads, r) {
			return fmt.Errorf("read %q is opened to everyone but not declared", r)
		}
	}
	for name, source := range m.ReadSources {
		if !slices.Contains(m.Reads, name) || !slices.Contains([]string{"record", "user-input", "external-input"}, source) {
			return fmt.Errorf("read %q has an undeclared name or invalid original source type %q", name, source)
		}
	}
	return nil
}

// CheckSubscriptions checks the original subscription declarations without
// invoking subscriber code or resolving mutable tenant state.
func CheckSubscriptions(m platform.Manifest, subscriber bool) error {
	for _, action := range m.Subscribes {
		if protocol, _, ok := strings.Cut(action, "#"); ok {
			if !slices.ContainsFunc(m.Consumes, func(c platform.Consumption) bool { return c.Protocol == protocol }) {
				return fmt.Errorf("%s subscribes to %s of a protocol it does not consume", m.ID, action)
			}
			continue
		}
		if _, own := m.Actions.Action(action); !own {
			return fmt.Errorf("%s subscribes to %s, neither its own action nor a protocol event", m.ID, action)
		}
		if !subscriber {
			return fmt.Errorf("%s subscribes but has no Handle", m.ID)
		}
	}
	return nil
}

// Callback-free transitions retain the smaller version-4 result format.
// Callback and approval transactions use the validated multi-decision batch.
func CallbackFreeTransition(m platform.Manifest, schema string) bool {
	for _, entity := range m.Entities {
		verb, match := strings.CutPrefix(schema, entity.Type+".")
		if !match || entity.Lifecycle == nil {
			continue
		}
		for _, transition := range entity.Lifecycle.Transitions {
			if transition.Name == verb && transition.Do == nil && transition.After == nil && transition.Approval == nil {
				return true
			}
		}
	}
	return false
}
