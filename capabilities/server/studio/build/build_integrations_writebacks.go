package build

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

var identifierRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func identifier(s string) bool { return identifierRE.MatchString(s) }

func tableName(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) == 0 || len(parts) > 2 {
		return false
	}
	for _, p := range parts {
		if !identifier(p) {
			return false
		}
	}
	return true
}

var simpleWhereRE = regexp.MustCompile(`^\s*[A-Za-z_][A-Za-z0-9_]*\s*(=|<>|!=|<|>|<=|>=)\s*('[^';]*'|-?[0-9]+(\.[0-9]+)?|true|false|null)(\s+and\s+[A-Za-z_][A-Za-z0-9_]*\s*(=|<>|!=|<|>|<=|>=)\s*('[^';]*'|-?[0-9]+(\.[0-9]+)?|true|false|null))*\s*$`)

func simpleWhere(s string) bool { return s == "" || simpleWhereRE.MatchString(strings.ToLower(s)) }

func (b *Build) writebackEntity() platform.Entity {
	return platform.Entity{Type: WritebackType, Title: "Writeback", Plural: "Writebacks", Model: Writeback{}, Display: "title",
		Description: "An accepted action on an object sent to an external system through a connection: queued, in order, retried with the same key for receiver deduplication.",
		Validate: func(c platform.Caller, record any) *kernel.Error {
			w := record.(*Writeback)
			if w.State == "published" {
				fixed, err := b.ExecutionWriteback(*w)
				if err != nil {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
				}
				if err := b.checkWritebackConnection(c, *w); err != nil {
					return err
				}
				return b.checkWriteback(c, fixed)
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
	if !c.Replaying {
		fixed, err := b.freezeDirectIntegration(platform.AssetWriteback, *w)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		*w = fixed.(Writeback)
	}
	return nil
}

func (b *Build) checkWriteback(c platform.Caller, w Writeback) *kernel.Error {
	refuse := func(message string, args ...any) *kernel.Error {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, message, args...)
	}
	if err := b.checkName(w.Name, w.ID); err != nil {
		return refuse(err.Error())
	}
	if err := CheckWritebackTransport(w.Transport, w.Method); err != nil {
		return refuse(err.Error())
	}
	if err := b.checkWritebackConnection(c, w); err != nil {
		return err
	}
	info, ok := b.lookupEntity(w.Object)
	if !ok {
		return refuse("The object {object} is not installed", w.Object)
	}
	if info.App != ID {
		return refuse("A writeback currently listens to objects owned by Build; native application objects need an owner callback")
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

func (b *Build) checkWritebackConnection(c platform.Caller, w Writeback) *kernel.Error {
	refuse := func(message string, args ...any) *kernel.Error {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, message, args...)
	}
	conn, ok := platform.Get[Connection](c, w.Connection)
	if ok {
		fixed, err := b.ConnectionConfiguration(conn)
		if err != nil {
			return refuse(err.Error())
		}
		conn = fixed
	}
	if !ok {
		return refuse("Connection {connection} does not exist", w.Connection)
	}
	if conn.Kind != "http" && conn.Kind != "odata" {
		return refuse("A writeback goes through an http or OData connection")
	}
	if w.Transport == "a2a" && conn.Kind != "http" {
		return refuse("A2A uses an HTTP connection")
	}
	return nil
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
			case "unknown":
				w.Unknown++
			default:
				w.Failed++
			}
			payload.Answer.At = now
			w.Answers = append([]WritebackAns{payload.Answer}, w.Answers...)
			if len(w.Answers) > ValueWritebackAnswerKept {
				w.Answers = w.Answers[:ValueWritebackAnswerKept]
			}
			c.Put(r, w)
		}, nil
	})
}

func (b *Build) answered(c platform.Caller, effect platform.Effect, out platform.Outcome, now time.Time) *kernel.Error {
	var req WritebackRequest
	if json.Unmarshal([]byte(effect.Body), &req) != nil || req.Writeback == "" {
		return nil
	}
	if req.Dispatch != "" {
		outcome := map[string]string{"delivered": "accepted", "rejected": "failed", "unknown": "unknown", "discarded": "unknown"}[out.Result]
		if effect.State == "failed" {
			outcome = "failed"
		}
		if outcome == "" {
			return nil
		}
		key := fmt.Sprintf("%s:%d:%s", effect.ID, effect.Attempts, out.Result)
		_, err := platform.Decide(c, b, dispatchSubmission(c, req.Dispatch, platform.DispatchAnswerSchema, key, map[string]any{"intent": effect.ID, "outcome": outcome, "reason": out.Detail, "answer": out.Answer}, now), now)
		return err
	}
	if req.Confirmation != "" {
		outcome := map[string]string{"delivered": "accepted", "rejected": "refused", "unknown": "unknown", "discarded": "refused"}[out.Result]
		if effect.State == "failed" {
			outcome = "failed"
		}
		tag := req.Party
		if req.Compensates != "" {
			tag += ":compensate"
		}
		key := fmt.Sprintf("%s:%d:%s", effect.ID, effect.Attempts, out.Result)
		_, err := platform.Decide(c, b, confirmationSubmission(c, req.Confirmation, confirmationAnswer, key, map[string]any{"tag": tag, "outcome": outcome, "reason": out.Detail, "answer": out.Answer}, now), now)
		return err
	}
	w, ok := platform.Get[Writeback](c, req.Writeback)
	if !ok {
		return nil
	}
	if req.Version == 1 {
		w.Object, w.Result = req.Object, slices.Clone(req.Result)
		if w.Object == "" {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Writeback request has no frozen target")
		}
	} else if req.Version != 0 || w.DefinitionVersion != 0 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Historical writeback request requires its original unversioned definition")
	}
	ans := WritebackAns{Effect: effect.ID, Target: effect.Target, Result: out.Result, Detail: out.Detail, Answer: clipAnswer(out.Answer)}
	if out.Result != "delivered" && out.Result != "rejected" && out.Result != "unknown" {
		ans.Result = "failed"
	}
	payload, _ := json.Marshal(map[string]any{"answer": ans})
	if _, err := platform.Decide(c, b, &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: ID, IdempotencyKey: "answered:" + effect.ID + map[bool]string{true: ":unknown"}[out.Result == "unknown"],
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

const (
	WritebackType            = "build.writeback"
	SchemaWritebackAnswered  = "build.writeback.answered"
	ValueWritebackAnswerKept = 20
	WritebackEndpoint        = "connection:"
)

type Writeback struct {
	platform.Record
	Name              string         `json:"name" field:"required,search"`
	Title             string         `json:"title" field:"required,search"`
	Connection        string         `json:"connection" field:"required" ref:"build.connection"`
	Object            string         `json:"object" field:"required" title:"Object"`
	On                string         `json:"on" field:"required" title:"After action"`
	Method            string         `json:"method" title:"Method"`
	Transport         string         `json:"transport,omitempty" choices:"http,a2a" title:"Transport"`
	Path              string         `json:"path" title:"Path"`
	Mapping           []SourceField  `json:"mapping,omitempty" title:"Body mapping"`
	Result            []SourceField  `json:"result,omitempty" title:"Answer mapping"`
	State             string         `json:"state" field:"readonly"`
	Publisher         string         `json:"publisher,omitempty" field:"readonly"`
	Sent              int            `json:"sent,omitempty" field:"readonly" title:"Delivered"`
	Rejected          int            `json:"rejected,omitempty" field:"readonly"`
	Failed            int            `json:"failed,omitempty" field:"readonly"`
	Unknown           int            `json:"unknown,omitempty" field:"readonly"`
	Answers           []WritebackAns `json:"answers,omitempty" field:"readonly" type:"json" title:"Recent answers"`
	DefinitionVersion int            `json:"definitionVersion,omitempty" field:"readonly" title:"Version"`
	Published         string         `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed"`
	Versions          []string       `json:"definitionVersions,omitempty" field:"readonly" type:"json" title:"Published versions"`
}

type WritebackAns struct {
	At     time.Time `json:"at"`
	Effect string    `json:"effect"`
	Target string    `json:"target"`
	Result string    `json:"result"`
	Detail string    `json:"detail,omitempty"`
	Answer string    `json:"answer,omitempty"`
}

func (w Writeback) Schema() string { return w.Object + "." + w.On }

type WritebackRequest struct {
	Connection        string         `json:"connection,omitempty"`
	Confirmation      string         `json:"confirmation,omitempty"`
	Dispatch          string         `json:"dispatch,omitempty"`
	DispatchKey       string         `json:"dispatchKey,omitempty"`
	SafeRetry         bool           `json:"safeRetry,omitempty"`
	Party             string         `json:"party,omitempty"`
	Compensates       string         `json:"compensates,omitempty"`
	Version           int            `json:"version,omitempty"`
	Object            string         `json:"object,omitempty"`
	DefinitionVersion int            `json:"definitionVersion,omitempty"`
	Result            []SourceField  `json:"result,omitempty"`
	Writeback         string         `json:"writeback"`
	Method            string         `json:"method"`
	Transport         string         `json:"transport,omitempty"`
	Path              string         `json:"path,omitempty"`
	URL               string         `json:"url"`
	Secret            string         `json:"secret,omitempty"`
	AllowPrivate      bool           `json:"allowPrivate,omitempty"`
	Body              map[string]any `json:"body"`
}

func (w Writeback) Request(conn Connection, id string, payload json.RawMessage) (WritebackRequest, error) {
	if err := CheckWritebackTransport(w.Transport, w.Method); err != nil {
		return WritebackRequest{}, err
	}
	if w.Transport == "a2a" && conn.Kind != "http" {
		return WritebackRequest{}, fmt.Errorf("A2A uses an HTTP connection")
	}
	ValueDefinitionVersion := 0
	if w.Published != "" {
		var asset platform.ReleaseAsset
		if err := ValueDecodeIntegration([]byte(w.Published), &asset); err != nil {
			return WritebackRequest{}, err
		}
		if asset.Ref != (platform.AssetRef{App: ID, Kind: platform.AssetWriteback, Name: w.Name}) {
			return WritebackRequest{}, fmt.Errorf("writeback publication identity differs")
		}
		if err := ValueValidateIntegrationDescriptor(asset); err != nil {
			return WritebackRequest{}, err
		}
		version, err := ValueIntegrationOrdinal(asset)
		if err != nil {
			return WritebackRequest{}, err
		}
		ValueDefinitionVersion = version
	} else if w.DefinitionVersion != 0 {
		return WritebackRequest{}, fmt.Errorf("versioned writeback has no accepted definition")
	}
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
			v, err := platform.IntegrationConvert(in[m.From], m.Convert)
			if err != nil {
				return WritebackRequest{}, fmt.Errorf("%s: %v", m.From, err)
			}
			body[m.To] = v
		}
	}
	path := w.Path
	for k, v := range in {
		path = strings.ReplaceAll(path, "{"+k+"}", platform.IntegrationScalar(v))
	}
	method := w.Method
	if method == "" {
		method = "POST"
	}
	request := WritebackRequest{Connection: conn.ID, Version: 1, Object: w.Object, DefinitionVersion: ValueDefinitionVersion, Result: slices.Clone(w.Result), Writeback: w.ID, Method: method, Transport: w.Transport, Path: path, URL: conn.Resolve(path), Secret: conn.Secret, AllowPrivate: conn.AllowPrivate, Body: body}
	return request, nil
}

func CheckWritebackTransport(transport, method string) error {
	if transport != "" && transport != "http" && transport != "a2a" {
		return fmt.Errorf("A writeback transport is HTTP or A2A")
	}
	if transport == "a2a" && method != "" && method != "POST" {
		return fmt.Errorf("A2A uses POST SendMessage")
	}
	return nil
}

func (w Writeback) Fields(answer []byte) map[string]any {
	var in map[string]any
	if json.Unmarshal(answer, &in) != nil {
		return nil
	}
	unwrapped := in
	if d, ok := in["d"].(map[string]any); ok && len(in) == 1 {
		unwrapped = d
	}
	out := map[string]any{}
	for _, m := range w.Result {
		v, ok := platform.IntegrationLookupPath(in, m.From)
		if !ok {
			if v, ok = platform.IntegrationLookupPath(unwrapped, m.From); !ok {
				continue
			}
		}
		if c, err := platform.IntegrationConvert(v, m.Convert); err == nil {
			out[m.To] = c
		}
	}
	return out
}
