package construction

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"platformserver/platform"
)

// Writebacks. A writeback declares that an accepted action on an
// object is sent to an external system through a Connection: an HTTP request
// whose body is mapped from the action's payload. The sending is an
// effect - at least once, in order per connection, with the decision's change
// id as the idempotency key - so an outage queues it and the receiver can
// deduplicate retries. The answer can write fields back onto the record (the document number
// SAP assigned, say) through the object's own edit.
const (
	WritebackType            = "build.writeback"
	SchemaWritebackAnswered  = "build.writeback.answered"
	ValueWritebackAnswerKept = 20
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
	Unknown   int            `json:"unknown,omitempty" field:"readonly"`
	Answers   []WritebackAns `json:"answers,omitempty" field:"readonly" type:"json" title:"Recent answers"`
	// Definition publication is independent of data/runtime versions.
	DefinitionVersion int      `json:"definitionVersion,omitempty" field:"readonly" title:"Version"`
	Published         string   `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed"`
	Versions          []string `json:"definitionVersions,omitempty" field:"readonly" type:"json" title:"Published versions"`
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

// Schema is the full action name a writeback listens for.
func (w Writeback) Schema() string { return w.Object + "." + w.On }

// WritebackRequest is the frozen request an effect carries: everything the
// dispatcher needs without reading records, the secret only by name.
type WritebackRequest struct {
	Connection        string                           `json:"connection,omitempty"`
	Confirmation      string                           `json:"confirmation,omitempty"`
	Dispatch          string                           `json:"dispatch,omitempty"`
	DispatchKey       string                           `json:"dispatchKey,omitempty"`
	SafeRetry         bool                             `json:"safeRetry,omitempty"`
	Party             string                           `json:"party,omitempty"`
	Compensates       string                           `json:"compensates,omitempty"`
	Version           int                              `json:"version,omitempty"`
	Object            string                           `json:"object,omitempty"`
	DefinitionVersion int                              `json:"definitionVersion,omitempty"`
	Result            []SourceField                    `json:"result,omitempty"`
	Writeback         string                           `json:"writeback"`
	Method            string                           `json:"method"`
	Path              string                           `json:"path,omitempty"`
	URL               string                           `json:"url"`
	Secret            string                           `json:"secret,omitempty"`
	AllowPrivate      bool                             `json:"allowPrivate,omitempty"`
	Body              map[string]any                   `json:"body"`
	Protocol          *platform.DriverBinding          `json:"protocol,omitempty"`
	Gateway           string                           `json:"gateway,omitempty"`
	DriverDispatch    *platform.DriverDispatchIdentity `json:"driverDispatch,omitempty"`
	DriverCommand     *platform.DriverCommandValue     `json:"driverCommand,omitempty"`
}

// Request freezes the request for one accepted decision: the payload mapped to
// the body (or sent whole with its id), the path's placeholders filled.
func (w Writeback) Request(conn Connection, id string, payload json.RawMessage) (WritebackRequest, error) {
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
			v, err := ValueConvert(in[m.From], m.Convert)
			if err != nil {
				return WritebackRequest{}, fmt.Errorf("%s: %v", m.From, err)
			}
			body[m.To] = v
		}
	}
	path := w.Path
	for k, v := range in {
		path = strings.ReplaceAll(path, "{"+k+"}", ValueScalar(v))
	}
	method := w.Method
	if method == "" {
		method = "POST"
	}
	request := WritebackRequest{Connection: conn.ID, Version: 1, Object: w.Object, DefinitionVersion: ValueDefinitionVersion, Result: slices.Clone(w.Result), Writeback: w.ID, Method: method, Path: path, URL: conn.Resolve(path), Secret: conn.Secret, AllowPrivate: conn.AllowPrivate, Body: body}
	if conn.Protocol != nil {
		if w.On != "dispatch" || method != "POST" || strings.Trim(path, "/") != "execute" {
			return WritebackRequest{}, fmt.Errorf("protocol commands use a dispatch-only POST execute writeback")
		}
		request.Protocol, request.Gateway = conn.Protocol, conn.Address
	}
	return request, nil
}

// Fields are what the answer writes onto the record, by the Result mapping.
func (w Writeback) Fields(answer []byte) map[string]any {
	var in map[string]any
	if json.Unmarshal(answer, &in) != nil {
		// OData v2 wraps in d; a bare value is nothing to map.
		return nil
	}
	// OData v2 wraps the entity in d. A mapping written against the raw
	// answer (d.MaterialDocument) and one written against the entity
	// (MaterialDocument) both find it (UX-14).
	unwrapped := in
	if d, ok := in["d"].(map[string]any); ok && len(in) == 1 {
		unwrapped = d
	}
	out := map[string]any{}
	for _, m := range w.Result {
		v, ok := ValueLookupPath(in, m.From)
		if !ok {
			if v, ok = ValueLookupPath(unwrapped, m.From); !ok {
				continue
			}
		}
		if c, err := ValueConvert(v, m.Convert); err == nil {
			out[m.To] = c
		}
	}
	return out
}

func ValueLookupPath(in map[string]any, path string) (any, bool) {
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
