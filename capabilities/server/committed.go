package platformserver

import (
	"context"
	"encoding/json"
	"maps"
	resultformat "platformserver/internal/acceptance/result"
	"platformserver/journal"
	"platformserver/platform"
	"slices"
	"strings"
)

// committed is the submit pipeline's memory of what it already answered, by
// idempotency key: refusals (effect-free answers), accepted answers to
// top-level inputs and release commands, connector inputs' answers, and the
// composite commands applied. Guarded by the tenant's lock; rebuilt by replay
// and carried by snapshots. Retries read here before any decision runs.
type acceptedValueReader interface {
	StateValue(context.Context, string, string, string, int64) (json.RawMessage, error)
	StateValues(context.Context, string, string, int64) (map[string]json.RawMessage, error)
}

type committed struct {
	position            func() int64
	appliedPosition     int64
	pending             map[string]bool
	storage             acceptedValueReader
	tenant              string
	decisionDefinitions map[string]*resultformat.DecisionDefinition
	schemaVersions      map[string]platform.SchemaVersion
	refusals            map[string]resultformat.Refusal // "<app>/<key>"
	answers             map[string]json.RawMessage      // "<app>/<key>", "release:<key>"
	inputs              map[string]json.RawMessage      // "<app>/<key>"
	compositeAnswers    map[string]json.RawMessage
	composites          map[string]string // key → applied digest
}

func (c *committed) saveAnswer(key string, raw json.RawMessage) {
	c.advance()
	if c.answers == nil {
		c.answers = map[string]json.RawMessage{}
	}
	c.answers[key] = slices.Clone(raw)
}

func (c *committed) saveInput(key string, raw json.RawMessage) {
	c.advance()
	if c.inputs == nil {
		c.inputs = map[string]json.RawMessage{}
	}
	c.inputs[key] = slices.Clone(raw)
}

func (c *committed) saveCompositeAnswer(key string, raw json.RawMessage) {
	c.advance()
	if c.compositeAnswers == nil {
		c.compositeAnswers = map[string]json.RawMessage{}
	}
	c.compositeAnswers[key] = slices.Clone(raw)
}

func (c *committed) saveComposite(key, digest string) {
	c.advance()
	if c.composites == nil {
		c.composites = map[string]string{}
	}
	c.composites[key] = digest
}

func (c *committed) snapshot(s *tenantState) {
	s.DecisionDefinitions = c.allDecisionDefinitions()
	s.SubmissionSchemas = c.allSchemaVersions()
	s.CompositeAnswers = c.allCompositeAnswers()
	s.Refusals, s.AcceptedAnswers, s.AcceptedInputs, s.Composites = c.allRefusals(), c.allAnswers(), c.allInputs(), c.allComposites()
	// Earlier SQL adapters put ordinary batches into the answer bucket even
	// though their original receipts already answer retries. Keep those stored
	// bytes readable, but project only real saved answers into host snapshots.
	for key, raw := range s.AcceptedAnswers {
		if c.storage == nil {
			break
		}
		if strings.HasPrefix(key, "release:") {
			continue
		}
		result, receipt, err := resultformat.DecodeBatch(raw)
		if err != nil {
			continue
		} // Restore retains its original rejection.
		sub, err := resultformat.BatchSubmission(result, receipt)
		if err == nil && sub != nil && result.Tenant == c.tenant && key == result.App+"/"+sub.GetIdempotencyKey() && !result.HasSavedAnswer(sub) {
			delete(s.AcceptedAnswers, key)
		}
	}
}

func (c *committed) restore(s *tenantState) {
	c.decisionDefinitions = maps.Clone(s.DecisionDefinitions)
	c.schemaVersions = maps.Clone(s.SubmissionSchemas)
	c.compositeAnswers = maps.Clone(s.CompositeAnswers)
	c.refusals, c.answers, c.inputs = maps.Clone(s.Refusals), maps.Clone(s.AcceptedAnswers), maps.Clone(s.AcceptedInputs)
	if c.refusals == nil {
		c.refusals = map[string]resultformat.Refusal{}
	}
	if s.Composites != nil {
		c.composites = s.Composites
	}
}

func (c *committed) saveDecisionDefinition(app, change string, definition *resultformat.DecisionDefinition) {
	if definition == nil {
		return
	}
	c.advance()
	if c.decisionDefinitions == nil {
		c.decisionDefinitions = map[string]*resultformat.DecisionDefinition{}
	}
	c.decisionDefinitions[app+"/"+change] = definition
}

func (c *committed) saveSchemaVersion(app, key string, binding *platform.SchemaVersion) {
	if binding == nil {
		return
	}
	c.advance()
	if c.schemaVersions == nil {
		c.schemaVersions = map[string]platform.SchemaVersion{}
	}
	c.schemaVersions[app+"/"+key] = *binding
}

// Historical answers are read only when requested. New committed images have
// already been written in the accepted transaction before these save methods.
func (c *committed) useStorage(s acceptedValueReader, tenant string, position func() int64) {
	c.storage, c.tenant = s, tenant
	c.position = position
	c.advance()
	c.pending = nil
	c.answers, c.inputs, c.compositeAnswers = nil, nil, nil
	c.refusals, c.composites, c.schemaVersions, c.decisionDefinitions = nil, nil, nil, nil
}
func (c *committed) advance() {
	if c.position != nil {
		c.appliedPosition = c.position()
	}
}
func (c *committed) stored(owner, key string) json.RawMessage {
	if c.pending[owner+"/"+key] {
		return nil
	}
	if c.storage == nil {
		return nil
	}
	raw, err := c.storage.StateValue(context.Background(), c.tenant, owner, key, c.appliedPosition)
	if err != nil {
		panic(err)
	}
	return raw
}
func storedValues[T any](c *committed, owner string, memory map[string]T, field ...string) map[string]T {
	if c.storage == nil && len(field) == 0 {
		return maps.Clone(memory)
	}
	var values map[string]json.RawMessage
	var err error
	if selection, ok := c.storage.(journal.StateValueSelection); ok && len(field) > 0 {
		values, err = selection.StateValuesWithField(context.Background(), c.tenant, owner, field[0], c.appliedPosition)
	} else if c.storage != nil {
		values, err = c.storage.StateValues(context.Background(), c.tenant, owner, c.appliedPosition)
	}
	if err != nil {
		panic(err)
	}
	selected := func(value any) bool {
		if len(field) == 0 {
			return true
		}
		raw, marshalErr := json.Marshal(value)
		if marshalErr != nil {
			panic(marshalErr)
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			panic(err)
		}
		_, present := object[field[0]]
		return present
	}
	out := make(map[string]T, len(values))
	for key, raw := range values {
		if c.pending[owner+"/"+key] || !selected(raw) {
			continue
		}
		var v T
		if err = json.Unmarshal(raw, &v); err != nil {
			panic(err)
		}
		out[key] = v
	}
	for key, v := range memory {
		if selected(v) {
			out[key] = v
		} else {
			delete(out, key)
		}
	}
	return out
}
func storedValue[T any](c *committed, owner, key string, memory map[string]T) (v T, ok bool) {
	if v, ok = memory[key]; ok {
		return
	}
	raw := c.stored(owner, key)
	if len(raw) == 0 {
		return v, false
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		panic(err)
	}
	return v, true
}
func (c *committed) answer(key string) json.RawMessage {
	v, _ := storedValue(c, "answer", key, c.answers)
	return v
}
func (c *committed) input(key string) json.RawMessage {
	v, _ := storedValue(c, "input", key, c.inputs)
	return v
}
func (c *committed) refusal(key string) (resultformat.Refusal, bool) {
	return storedValue(c, "refusal", key, c.refusals)
}
func (c *committed) compositeAnswer(key string) json.RawMessage {
	v, _ := storedValue(c, "composite-answer", key, c.compositeAnswers)
	return v
}
func (c *committed) composite(key string) (string, bool) {
	return storedValue(c, "composite", key, c.composites)
}
func (c *committed) schemaVersion(key string) (platform.SchemaVersion, bool) {
	return storedValue(c, "schema-version", key, c.schemaVersions)
}
func (c *committed) decisionDefinition(key string) *resultformat.DecisionDefinition {
	v, _ := storedValue(c, "decision-definition", key, c.decisionDefinitions)
	return v
}
func (c *committed) allAnswers() map[string]json.RawMessage {
	answers := storedValues(c, "answer", c.answers)
	if len(answers) == 0 {
		return nil
	}
	return answers
}
func (c *committed) allInputs() map[string]json.RawMessage { return storedValues(c, "input", c.inputs) }
func (c *committed) allRefusals() map[string]resultformat.Refusal {
	return storedValues(c, "refusal", c.refusals)
}
func (c *committed) allCompositeAnswers() map[string]json.RawMessage {
	return storedValues(c, "composite-answer", c.compositeAnswers)
}
func (c *committed) allComposites() map[string]string {
	return storedValues(c, "composite", c.composites)
}
func (c *committed) allSchemaVersions() map[string]platform.SchemaVersion {
	return storedValues(c, "schema-version", c.schemaVersions)
}
func (c *committed) allDecisionDefinitions() map[string]*resultformat.DecisionDefinition {
	return storedValues(c, "decision-definition", c.decisionDefinitions)
}
func (c *committed) saveRefusal(key string, r resultformat.Refusal) {
	c.advance()
	if c.refusals == nil {
		c.refusals = map[string]resultformat.Refusal{}
	}
	c.refusals[key] = r
}
func (c *committed) storageValues(d *journal.StateDelta) {
	add := func(owner, key string, v any) {
		d.Values = append(d.Values, journal.StateValue{Owner: owner, Key: key, Image: jsonOf(v)})
	}
	for k, v := range c.allAnswers() {
		add("answer", k, v)
	}
	for k, v := range c.allInputs() {
		add("input", k, v)
	}
	for k, v := range c.allRefusals() {
		add("refusal", k, v)
	}
	for k, v := range c.allCompositeAnswers() {
		add("composite-answer", k, v)
	}
	for k, v := range c.allComposites() {
		add("composite", k, v)
	}
	for k, v := range c.allSchemaVersions() {
		add("schema-version", k, v)
	}
	for k, v := range c.allDecisionDefinitions() {
		add("decision-definition", k, v)
	}
}

// One result may be committed before it is applied to the working set. Hide
// its historical metadata until the original application path saves it; keep
// at most that result's metadata resident, then read it from SQL thereafter.
func (c *committed) releasePending() {
	if c.storage == nil {
		return
	}
	c.pending = nil
	c.answers, c.inputs, c.compositeAnswers = nil, nil, nil
	c.refusals, c.composites, c.schemaVersions, c.decisionDefinitions = nil, nil, nil, nil
}
func (c *committed) holdPending(d journal.StateDelta) {
	if c.storage == nil {
		return
	}
	c.pending = map[string]bool{}
	for _, v := range d.Values {
		c.pending[v.Owner+"/"+v.Key] = true
	}
}
