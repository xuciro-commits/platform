package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"platformserver/journal"
	acceptedformat "platformserver/kernel/acceptedformat"
	"platformserver/platform"
	"slices"
	"strings"
	"sync"
)

type AcceptedValueReader interface {
	StateValue(context.Context, string, string, string, int64) (json.RawMessage, error)
	StateValues(context.Context, string, string, int64) (map[string]json.RawMessage, error)
}

type CommittedState struct {
	DecisionDefinitions map[string]*platform.DecisionDefinition
	SubmissionSchemas   map[string]platform.SchemaVersion
	CompositeAnswers    map[string]json.RawMessage
	Refusals            map[string]acceptedformat.Refusal
	AcceptedAnswers     map[string]json.RawMessage
	AcceptedInputs      map[string]json.RawMessage
	Composites          map[string]string
}

type Committed struct {
	position            func() int64
	appliedPosition     int64
	pending             map[string]bool
	storage             AcceptedValueReader
	tenant              string
	decisionDefinitions map[string]*platform.DecisionDefinition
	schemaVersions      map[string]platform.SchemaVersion
	refusals            map[string]acceptedformat.Refusal
	answers             map[string]json.RawMessage
	inputs              map[string]json.RawMessage
	compositeAnswers    map[string]json.RawMessage
	composites          map[string]string
}

func (c *Committed) SaveAnswer(key string, raw json.RawMessage) {
	c.advance()
	if c.answers == nil {
		c.answers = map[string]json.RawMessage{}
	}
	c.answers[key] = slices.Clone(raw)
}

func (c *Committed) SaveInput(key string, raw json.RawMessage) {
	c.advance()
	if c.inputs == nil {
		c.inputs = map[string]json.RawMessage{}
	}
	c.inputs[key] = slices.Clone(raw)
}

func (c *Committed) SaveCompositeAnswer(key string, raw json.RawMessage) {
	c.advance()
	if c.compositeAnswers == nil {
		c.compositeAnswers = map[string]json.RawMessage{}
	}
	c.compositeAnswers[key] = slices.Clone(raw)
}

func (c *Committed) SaveComposite(key, digest string) {
	c.advance()
	if c.composites == nil {
		c.composites = map[string]string{}
	}
	c.composites[key] = digest
}

func (c *Committed) SnapshotState() (s CommittedState) {
	s.DecisionDefinitions = c.AllDecisionDefinitions()
	s.SubmissionSchemas = c.AllSchemaVersions()
	s.CompositeAnswers = c.AllCompositeAnswers()
	s.Refusals, s.AcceptedAnswers, s.AcceptedInputs, s.Composites = c.AllRefusals(), c.AllAnswers(), c.AllInputs(), c.AllComposites()
	for key, raw := range s.AcceptedAnswers {
		if c.storage == nil {
			break
		}
		if strings.HasPrefix(key, "release:") {
			continue
		}
		result, receipt, err := acceptedformat.DecodeBatch(raw)
		if err != nil {
			continue
		}
		sub, err := acceptedformat.BatchSubmission(result, receipt)
		if err == nil && sub != nil && result.Tenant == c.tenant && key == result.App+"/"+sub.GetIdempotencyKey() && !result.HasSavedAnswer(sub) {
			delete(s.AcceptedAnswers, key)
		}
	}
	return s
}

func (c *Committed) Restore(s CommittedState) {
	c.decisionDefinitions = maps.Clone(s.DecisionDefinitions)
	c.schemaVersions = maps.Clone(s.SubmissionSchemas)
	c.compositeAnswers = maps.Clone(s.CompositeAnswers)
	c.refusals, c.answers, c.inputs = maps.Clone(s.Refusals), maps.Clone(s.AcceptedAnswers), maps.Clone(s.AcceptedInputs)
	if c.refusals == nil {
		c.refusals = map[string]acceptedformat.Refusal{}
	}
	if s.Composites != nil {
		c.composites = s.Composites
	}
}

func (c *Committed) SaveDecisionDefinition(app, change string, definition *platform.DecisionDefinition) {
	if definition == nil {
		return
	}
	c.advance()
	if c.decisionDefinitions == nil {
		c.decisionDefinitions = map[string]*platform.DecisionDefinition{}
	}
	c.decisionDefinitions[app+"/"+change] = definition
}

func (c *Committed) SaveSchemaVersion(app, key string, binding *platform.SchemaVersion) {
	if binding == nil {
		return
	}
	c.advance()
	if c.schemaVersions == nil {
		c.schemaVersions = map[string]platform.SchemaVersion{}
	}
	c.schemaVersions[app+"/"+key] = *binding
}

func (c *Committed) UseStorage(s AcceptedValueReader, tenant string, position func() int64) {
	c.storage, c.tenant = s, tenant
	c.position = position
	c.advance()
	c.pending = nil
	c.answers, c.inputs, c.compositeAnswers = nil, nil, nil
	c.refusals, c.composites, c.schemaVersions, c.decisionDefinitions = nil, nil, nil, nil
}
func (c *Committed) advance() {
	if c.position != nil {
		c.appliedPosition = c.position()
	}
}
func (c *Committed) stored(owner, key string) json.RawMessage {
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
func storedValues[T any](c *Committed, owner string, memory map[string]T, field ...string) map[string]T {
	if c.storage == nil && len(field) == 0 {
		if memory == nil {
			return map[string]T{}
		}
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
func storedValue[T any](c *Committed, owner, key string, memory map[string]T) (v T, ok bool) {
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
func (c *Committed) Answer(key string) json.RawMessage {
	v, _ := storedValue(c, "answer", key, c.answers)
	return v
}
func (c *Committed) Input(key string) json.RawMessage {
	v, _ := storedValue(c, "input", key, c.inputs)
	return v
}
func (c *Committed) Refusal(key string) (acceptedformat.Refusal, bool) {
	return storedValue(c, "refusal", key, c.refusals)
}
func (c *Committed) CompositeAnswer(key string) json.RawMessage {
	v, _ := storedValue(c, "composite-answer", key, c.compositeAnswers)
	return v
}
func (c *Committed) Composite(key string) (string, bool) {
	return storedValue(c, "composite", key, c.composites)
}
func (c *Committed) SchemaVersion(key string) (platform.SchemaVersion, bool) {
	return storedValue(c, "schema-version", key, c.schemaVersions)
}
func (c *Committed) DecisionDefinition(key string) *platform.DecisionDefinition {
	v, _ := storedValue(c, "decision-definition", key, c.decisionDefinitions)
	return v
}
func (c *Committed) AllAnswers(field ...string) map[string]json.RawMessage {
	answers := storedValues(c, "answer", c.answers, field...)
	if len(answers) == 0 {
		return nil
	}
	return answers
}
func (c *Committed) AllInputs() map[string]json.RawMessage { return storedValues(c, "input", c.inputs) }
func (c *Committed) AllRefusals(field ...string) map[string]acceptedformat.Refusal {
	return storedValues(c, "refusal", c.refusals, field...)
}
func (c *Committed) AllCompositeAnswers() map[string]json.RawMessage {
	return storedValues(c, "composite-answer", c.compositeAnswers)
}
func (c *Committed) AllComposites() map[string]string {
	return storedValues(c, "composite", c.composites)
}
func (c *Committed) AllSchemaVersions() map[string]platform.SchemaVersion {
	return storedValues(c, "schema-version", c.schemaVersions)
}
func (c *Committed) AllDecisionDefinitions() map[string]*platform.DecisionDefinition {
	return storedValues(c, "decision-definition", c.decisionDefinitions)
}
func (c *Committed) SaveRefusal(key string, r acceptedformat.Refusal) {
	c.advance()
	if c.refusals == nil {
		c.refusals = map[string]acceptedformat.Refusal{}
	}
	c.refusals[key] = r
}
func (c *Committed) StorageValues(d *journal.StateDelta) {
	add := func(owner, key string, v any) {
		d.Values = append(d.Values, journal.StateValue{Owner: owner, Key: key, Image: platform.Raw(v)})
	}
	for k, v := range c.AllAnswers() {
		add("answer", k, v)
	}
	for k, v := range c.AllInputs() {
		add("input", k, v)
	}
	for k, v := range c.AllRefusals() {
		add("refusal", k, v)
	}
	for k, v := range c.AllCompositeAnswers() {
		add("composite-answer", k, v)
	}
	for k, v := range c.AllComposites() {
		add("composite", k, v)
	}
	for k, v := range c.AllSchemaVersions() {
		add("schema-version", k, v)
	}
	for k, v := range c.AllDecisionDefinitions() {
		add("decision-definition", k, v)
	}
}

func (c *Committed) ReleasePending() {
	if c.storage == nil {
		return
	}
	c.pending = nil
	c.answers, c.inputs, c.compositeAnswers = nil, nil, nil
	c.refusals, c.composites, c.schemaVersions, c.decisionDefinitions = nil, nil, nil, nil
}
func (c *Committed) HoldPending(d journal.StateDelta) {
	if c.storage == nil {
		return
	}
	c.pending = map[string]bool{}
	for _, v := range d.Values {
		c.pending[v.Owner+"/"+v.Key] = true
	}
}

func (c *Committed) CacheSizes() (answers, schemas int) {
	return len(c.answers), len(c.schemaVersions)
}

type Sequences struct {
	mu   sync.Mutex
	last map[string]int
}

func (s *Sequences) Take(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.last == nil {
		s.last = map[string]int{}
	}
	s.last[key]++
	return s.last[key]
}

func (s *Sequences) Clone() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := maps.Clone(s.last)
	if out == nil {
		out = map[string]int{}
	}
	return out
}

func (s *Sequences) Check(bases, next map[string]int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, n := range next {
		if bases[key] != s.last[key] || n <= s.last[key] {
			return fmt.Errorf("record batch sequence %s does not advance", key)
		}
	}
	return nil
}

func (s *Sequences) Set(next map[string]int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.last == nil {
		s.last = map[string]int{}
	}
	maps.Copy(s.last, next)
}

func (s *Sequences) Restore(last map[string]int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.last = last
	if s.last == nil {
		s.last = map[string]int{}
	}
}
