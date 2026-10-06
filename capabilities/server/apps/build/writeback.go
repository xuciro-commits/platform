package build

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// Writebacks (ADR-0072). A writeback declares that an accepted action on an
// object is sent to an external system through a Connection: an HTTP request
// whose body is mapped from the action's payload. The sending is an ADR-0014
// effect - at least once, in order per connection, with the decision's change
// id as the idempotency key - so an outage queues it and the receiver can
// deduplicate retries. The answer can write fields back onto the record (the document number
// SAP assigned, say) through the object's own edit.
const (
	WritebackType           = "build.writeback"
	SchemaWritebackAnswered = "build.writeback.answered"
	writebackAnswerKept     = 20
	// WritebackEndpoint prefixes the effect endpoint of a connection: "connection:<id>".
	WritebackEndpoint = "connection:"
)

type Writeback struct {
	platform.Record
	Name  string `json:"name" field:"required,search"`
	Title string `json:"title" field:"required,search"`
	// Connection is where it goes (http or odata); Object and On name the accepted action that sends it:
	// On is the action's verb - create, edit, or a declared action's name.
	Connection string `json:"connection" field:"required" ref:"build.connection"`
	Object     string `json:"object" field:"required" title:"Object"`
	On         string `json:"on" field:"required" title:"After action"`
	// Method and Path form the request; Path is relative to the connection's address and may use
	// {id} and {field} placeholders from the payload.
	Method string `json:"method" title:"Method"`
	Path   string `json:"path" title:"Path"`
	// Mapping is payload field → body key; empty sends the payload with its id.
	Mapping []SourceField `json:"mapping,omitempty" title:"Body mapping"`
	// Result is answer key → record field, written onto the record when the system answers.
	Result    []SourceField  `json:"result,omitempty" title:"Answer mapping"`
	State     string         `json:"state" field:"readonly"`
	Publisher string         `json:"publisher,omitempty" field:"readonly"`
	Sent      int            `json:"sent,omitempty" field:"readonly" title:"Delivered"`
	Rejected  int            `json:"rejected,omitempty" field:"readonly"`
	Failed    int            `json:"failed,omitempty" field:"readonly"`
	Answers   []WritebackAns `json:"answers,omitempty" field:"readonly" type:"json" title:"Recent answers"`
}

// WritebackAns is one settled attempt as the builder sees it.
type WritebackAns struct {
	At     time.Time `json:"at"`
	Effect string    `json:"effect"`
	Target string    `json:"target"`
	Result string    `json:"result"` // delivered, rejected, failed
	Detail string    `json:"detail,omitempty"`
	Answer string    `json:"answer,omitempty"` // first bytes of the body
}

func (b *Build) writebackEntity() platform.Entity {
	return platform.Entity{Type: WritebackType, Title: "Writeback", Plural: "Writebacks", Model: Writeback{}, Display: "title",
		Description: "An accepted action on an object sent to an external system through a connection: queued, in order, retried with the same key for receiver deduplication.",
		Validate: func(c platform.Caller, record any) *kernel.Error {
			w := record.(*Writeback)
			if w.State == "published" {
				return b.checkWriteback(c, *w)
			}
			return nil
		},
		Scope:    platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant, Integrator: platform.ScopeTenant}},
		Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder, Integrator}, Capability: "integrations"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}},
			Transitions: []platform.Transition{
				{Name: "publish", Title: "Publish", Description: "Check the writeback and send every matching decision from now on.", From: []string{"draft", "published"}, To: []string{"published"}, Roles: []string{Builder, Integrator}, Capability: "integrations", Payload: []platform.Field{}, Do: b.publishWriteback},
				{Name: "pause", Title: "Pause", Description: "Stop sending; what is queued still goes.", From: []string{"published"}, To: []string{"draft"}, Roles: []string{Builder, Integrator}, Capability: "integrations", Payload: []platform.Field{}}}}}
}

func writebackActions() []platform.Action {
	return []platform.Action{{Schema: SchemaWritebackAnswered, Target: WritebackType, Capability: "integrations", Title: "Keep answer", Description: "Retain what the external system answered to one writeback.", Automation: true,
		Payload: []platform.Field{{Name: "answer", Type: "json", Required: true, Description: "The attempt's result and the answer's first bytes"}}}}
}

func (b *Build) publishWriteback(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	w, ok := record.(*Writeback)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if err := b.checkWriteback(c, *w); err != nil {
		return err
	}
	w.Publisher = c.ID
	return nil
}

func (b *Build) checkWriteback(c platform.Caller, w Writeback) *kernel.Error {
	refuse := func(message string, args ...any) *kernel.Error {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, message, args...)
	}
	if err := b.checkName(w.Name, w.ID); err != nil {
		return refuse(err.Error())
	}
	conn, ok := platform.Get[Connection](c, w.Connection)
	if !ok || conn.State != "ready" {
		return refuse("The connection is not ready; check it first")
	}
	if conn.Kind != "http" && conn.Kind != "odata" {
		return refuse("A writeback goes through an http or OData connection")
	}
	info, ok := b.lookupEntity(w.Object)
	if !ok {
		return refuse("The object {object} is not installed", w.Object)
	}
	if w.On == "" || strings.ContainsAny(w.On, "./ ") {
		return refuse("After action is the verb of the object's action: create, edit or a declared action's name")
	}
	if !slices.Contains([]string{"", "POST", "PUT", "PATCH"}, w.Method) {
		return refuse("The method is POST, PUT or PATCH")
	}
	if strings.Contains(w.Path, "://") {
		return refuse("The path is relative to the connection's address")
	}
	for _, m := range append(slices.Clone(w.Mapping), w.Result...) {
		if m.From == "" || m.To == "" {
			return refuse("Every mapping names both sides")
		}
		if !slices.Contains([]string{"", "string", "number", "boolean", "date"}, m.Convert) {
			return refuse("A conversion is one of string, number, boolean, date")
		}
	}
	for _, m := range w.Result {
		if f, ok := info.Field(m.To); !ok || f.ReadOnly {
			return refuse("The answer field {field} is not a writable field of the object", m.To)
		}
	}
	return nil
}

// Schema is the full action name a writeback listens for.
func (w Writeback) Schema() string { return w.Object + "." + w.On }

// WritebackRequest is the frozen request an effect carries: everything the
// dispatcher needs without reading records, the secret only by name.
type WritebackRequest struct {
	Writeback    string         `json:"writeback"`
	Method       string         `json:"method"`
	URL          string         `json:"url"`
	Secret       string         `json:"secret,omitempty"`
	AllowPrivate bool           `json:"allowPrivate,omitempty"`
	Body         map[string]any `json:"body"`
}

// Request freezes the request for one accepted decision: the payload mapped to
// the body (or sent whole with its id), the path's placeholders filled.
func (w Writeback) Request(conn Connection, id string, payload json.RawMessage) (WritebackRequest, error) {
	var in map[string]any
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &in); err != nil {
			return WritebackRequest{}, fmt.Errorf("the payload is not an object")
		}
	}
	if in == nil {
		in = map[string]any{}
	}
	in["id"] = id
	body := in
	if len(w.Mapping) > 0 {
		body = map[string]any{}
		for _, m := range w.Mapping {
			v, err := convert(in[m.From], m.Convert)
			if err != nil {
				return WritebackRequest{}, fmt.Errorf("%s: %v", m.From, err)
			}
			body[m.To] = v
		}
	}
	path := w.Path
	for k, v := range in {
		path = strings.ReplaceAll(path, "{"+k+"}", scalar(v))
	}
	method := w.Method
	if method == "" {
		method = "POST"
	}
	return WritebackRequest{Writeback: w.ID, Method: method, URL: conn.Resolve(path), Secret: conn.Secret, AllowPrivate: conn.AllowPrivate, Body: body}, nil
}

// Fields are what the answer writes onto the record, by the Result mapping.
func (w Writeback) Fields(answer []byte) map[string]any {
	var in map[string]any
	if json.Unmarshal(answer, &in) != nil {
		// OData v2 wraps in d; a bare value is nothing to map.
		return nil
	}
	if d, ok := in["d"].(map[string]any); ok && len(in) == 1 {
		in = d
	}
	out := map[string]any{}
	for _, m := range w.Result {
		v, ok := lookupPath(in, m.From)
		if !ok {
			continue
		}
		if c, err := convert(v, m.Convert); err == nil {
			out[m.To] = c
		}
	}
	return out
}

func lookupPath(in map[string]any, path string) (any, bool) {
	var cur any = in
	for _, part := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = m[part]; !ok {
			return nil, false
		}
	}
	return cur, true
}

func (b *Build) submitWritebackAnswered(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return b.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		var payload struct {
			Answer WritebackAns `json:"answer"`
		}
		w, ok := platform.Get[Writeback](c, s.GetTarget().GetId())
		if !ok {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The writeback does not exist")
		}
		if json.Unmarshal(s.GetPayload(), &payload) != nil || payload.Answer.Result == "" {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "An answer carries its result")
		}
		return func(r *pb.ChangeRecord) {
			switch payload.Answer.Result {
			case "delivered":
				w.Sent++
			case "rejected":
				w.Rejected++
			default:
				w.Failed++
			}
			payload.Answer.At = now
			w.Answers = append([]WritebackAns{payload.Answer}, w.Answers...)
			if len(w.Answers) > writebackAnswerKept {
				w.Answers = w.Answers[:writebackAnswerKept]
			}
			c.Put(r, w)
		}, nil
	})
}

// Answered takes a settled writeback effect (platform.Answerer): it keeps the
// answer on the writeback and, when delivered with a Result mapping, writes
// the answer's fields onto the record through the object's own edit.
func (b *Build) answered(c platform.Caller, effect platform.Effect, out platform.Outcome, now time.Time) *kernel.Error {
	var req WritebackRequest
	if json.Unmarshal([]byte(effect.Body), &req) != nil || req.Writeback == "" {
		return nil
	}
	w, ok := platform.Get[Writeback](c, req.Writeback)
	if !ok {
		return nil
	}
	ans := WritebackAns{Effect: effect.ID, Target: effect.Target, Result: out.Result, Detail: out.Detail, Answer: clipAnswer(out.Answer)}
	if out.Result != "delivered" && out.Result != "rejected" {
		ans.Result = "failed"
	}
	payload, _ := json.Marshal(map[string]any{"answer": ans})
	if _, err := platform.Decide(c, b, &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: ID, IdempotencyKey: "answered:" + effect.ID,
		Target: &pb.EntityRef{Type: WritebackType, Id: w.ID}, Schema: &pb.SchemaRef{Name: SchemaWritebackAnswered, Version: 1}, Payload: payload}, now); err != nil {
		return err
	}
	typ, id, ok := strings.Cut(effect.Target, "/")
	if out.Result != "delivered" || !ok || typ != w.Object || len(w.Result) == 0 {
		return nil
	}
	fields := w.Fields(out.Answer)
	if len(fields) == 0 {
		return nil
	}
	edit, _ := json.Marshal(fields)
	_, err := platform.Decide(c, b, &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: ID, IdempotencyKey: "answer-fields:" + effect.ID,
		Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: typ + ".edit", Version: 1}, Payload: edit}, now)
	return err
}

func clipAnswer(raw []byte) string {
	s := string(raw)
	if len(s) > 400 {
		return s[:400] + "…"
	}
	return s
}
