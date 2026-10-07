package platformserver

import (
	"encoding/json"
	"maps"
	"slices"
)

// committed is the submit pipeline's memory of what it already answered, by
// idempotency key: refusals (effect-free answers), accepted answers to
// top-level inputs and release commands, connector inputs' answers, and the
// composite commands applied. Guarded by the tenant's lock; rebuilt by replay
// and carried by snapshots. Retries read here before any decision runs.
type committed struct {
	refusals   map[string]refusedResult   // "<app>/<key>"
	answers    map[string]json.RawMessage // "<app>/<key>", "release:<key>"
	inputs     map[string]json.RawMessage // "<app>/<key>"
	composites map[string]string          // key → applied digest
}

func (c *committed) saveAnswer(key string, raw json.RawMessage) {
	if c.answers == nil {
		c.answers = map[string]json.RawMessage{}
	}
	c.answers[key] = slices.Clone(raw)
}

func (c *committed) saveInput(key string, raw json.RawMessage) {
	if c.inputs == nil {
		c.inputs = map[string]json.RawMessage{}
	}
	c.inputs[key] = slices.Clone(raw)
}

func (c *committed) saveComposite(key, digest string) {
	if c.composites == nil {
		c.composites = map[string]string{}
	}
	c.composites[key] = digest
}

func (c *committed) snapshot(s *tenantState) {
	s.Refusals, s.AcceptedAnswers, s.AcceptedInputs, s.Composites = maps.Clone(c.refusals), maps.Clone(c.answers), maps.Clone(c.inputs), maps.Clone(c.composites)
}

func (c *committed) restore(s *tenantState) {
	c.refusals, c.answers, c.inputs = maps.Clone(s.Refusals), maps.Clone(s.AcceptedAnswers), maps.Clone(s.AcceptedInputs)
	if c.refusals == nil {
		c.refusals = map[string]refusedResult{}
	}
	if s.Composites != nil {
		c.composites = s.Composites
	}
}
