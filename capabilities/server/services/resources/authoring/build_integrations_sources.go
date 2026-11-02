package build

import (
	"encoding/json"
	"fmt"
	"math/big"
	"net/url"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (b *Build) sourceEntity() platform.Entity {
	return platform.Entity{Type: SourceType, Title: "Data source", Plural: "Data sources", Model: Source{}, Display: "title", Description: "An external JSON endpoint whose rows become records of one object, pulled on a period or on request.",
		Validate: func(c platform.Caller, record any) *kernel.Error {
			s := record.(*Source)
			if s.State == "published" {
				fixed, err := b.ExecutionSource(*s)
				if err != nil {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
				}
				if err := b.checkSourceConnection(c, *s); err != nil {
					return err
				}
				return b.checkSource(c, fixed)
			}
			return nil
		},
		Scope:    platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant, Integrator: platform.ScopeTenant}},
		Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder, Integrator}, Capability: "integrations"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}},
			Transitions: []platform.Transition{
				{Name: "publish", Title: "Publish", Description: "Publish this source definition. Use Pull now to start it.", From: []string{"draft", "published"}, To: []string{"published"}, Roles: []string{Builder, Integrator}, Capability: "integrations", Payload: []platform.Field{}, Do: b.publishSource},
				{Name: "pull", Title: "Pull now", Description: "Ask for one pull at the next tick.", From: []string{"published"}, To: []string{"published"}, Roles: []string{Builder, Integrator}, Capability: "integrations", Payload: []platform.Field{}, Do: b.requestPull},
				{Name: "reset", Title: "Reset cursor", Description: "Forget the incremental cursor and pull everything at the next tick.", From: []string{"published"}, To: []string{"published"}, Roles: []string{Builder, Integrator}, Capability: "integrations", Payload: []platform.Field{}, Do: b.resetCursor},
				{Name: "pause", Title: "Pause", Description: "Stop pulling; the mapping stays.", From: []string{"published"}, To: []string{"draft"}, Roles: []string{Builder, Integrator}, Capability: "integrations", Payload: []platform.Field{}}}}}
}

func sourceActions() []platform.Action {
	return []platform.Action{{Schema: SchemaSourcePulled, Target: SourceType, Capability: "integrations", Title: "Keep pull result", Description: "Retain what one pull of a data source decided.", Roles: []string{Builder, Integrator},
		Payload: []platform.Field{{Name: "pull", Type: "json", Required: true, Description: "The pull's counts and first failures"}}}}
}

func (b *Build) requestPull(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	s, ok := record.(*Source)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if s.Stream && s.Dataset == "" {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A continuous source is consumed through its Flow instance")
	}
	if problem := b.prepareSourcePull(c, s); problem != nil {
		return problem
	}
	if !c.Replaying {
		s.Puller, s.PullerScopes = c.ID, string(platform.Raw(c.Member.Scopes))
	}
	s.Requested = true
	return nil
}

func (b *Build) resetCursor(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	s, ok := record.(*Source)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if s.Stream && s.Dataset == "" {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A continuous source cursor belongs to its Flow instance")
	}
	if problem := b.prepareSourcePull(c, s); problem != nil {
		return problem
	}
	if s.Last != nil && s.Last.Execution != nil && s.Last.Execution.Pending {
		last, execution := *s.Last, *s.Last.Execution
		execution.Pending = false
		last.Execution = &execution
		last.Error = "The previous pull was stopped by resetting its cursor"
		s.Last = &last
	}
	if !c.Replaying {
		s.Puller, s.PullerScopes = c.ID, string(platform.Raw(c.Member.Scopes))
	}
	s.Cursor, s.Requested = "", true
	return nil
}

func (b *Build) prepareSourcePull(c platform.Caller, source *Source) *kernel.Error {
	if c.Replaying {
		return nil
	}
	fixed, err := b.ExecutionSource(*source)
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{why}", err.Error())
	}
	if problem := b.checkSourceConnection(c, fixed); problem != nil {
		return problem
	}
	if fixed.Connection != "" {
		connection, ok := platform.Get[Connection](c, fixed.Connection)
		if !ok {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Connection {connection} does not exist", fixed.Connection)
		}
		if _, err := b.ExecutionConnection(connection); err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{why}", err.Error())
		}
	}
	return nil
}

func (b *Build) publishSource(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	s, ok := record.(*Source)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if err := b.checkSource(c, *s); err != nil {
		return err
	}
	if c.Replaying {
		s.Puller, s.Requested = c.ID, !s.Stream
	}
	if !c.Replaying {
		s.Publisher, s.PublisherScopes = c.ID, string(platform.Raw(c.Member.Scopes))
		fixed, err := b.freezeDirectIntegration(platform.AssetSource, *s)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		*s = fixed.(Source)
	}
	if s.Stream && s.Dataset != "" && !c.Replaying {
		s.Puller, s.PullerScopes = c.ID, string(platform.Raw(c.Member.Scopes))
	}
	return nil
}

func (b *Build) checkSource(c platform.Caller, s Source) *kernel.Error {
	refuse := func(message string, args ...any) *kernel.Error {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, message, args...)
	}
	if err := b.checkName(s.Name, s.ID); err != nil {
		return refuse(err.Error())
	}
	if !slices.Contains([]string{"", "json", "csv", "odata", "table"}, s.Profile) {
		return refuse("A source profile is one of json, csv, odata, table")
	}
	if err := b.checkSourceConnection(c, s); err != nil {
		return err
	}
	if s.Since != "" && !identifier(s.Since) {
		return refuse("The incremental column is a plain identifier")
	}
	if s.Stream {
		if s.Profile != "table" || s.Since == "" || s.Object != "" || s.Key != "" || len(s.Mapping) != 0 || s.Every != "" {
			return refuse("A continuous source uses an incremental PostgreSQL table, without a materialization target or import schedule")
		}
		if s.Dataset != "" {
			ds, ok := platform.Get[Dataset](c, s.Dataset)
			if !ok {
				return refuse("The target dataset does not exist")
			}
			fixed, err := b.ExecutionDataset(ds)
			if err != nil || !fixed.Stream {
				return refuse("A continuous source writes to a streaming dataset")
			}
		}
		return nil
	}
	if s.Dataset != "" {
		if s.Object != "" {
			return refuse("A source feeds either a dataset or an object")
		}
		if _, ok := platform.Get[Dataset](c, s.Dataset); !ok {
			return refuse("The target dataset does not exist")
		}
		return checkEvery(s.Every)
	}
	if s.Connection != "" {
		if conn, ok := platform.Get[Connection](c, s.Connection); ok {
			fixed, err := b.ConnectionConfiguration(conn)
			if err != nil {
				return refuse(err.Error())
			}
			if Guarded(fixed.Marking) {
				return refuse("A confidential or restricted connection feeds a dataset; use a pipeline to apply field access rules")
			}
		}
	}
	info, ok := b.lookupEntity(s.Object)
	if !ok {
		return refuse("The target object {object} is not installed", s.Object)
	}
	if slices.ContainsFunc(info.DataSources, func(binding platform.DataSourceBinding) bool {
		return binding.Kind == "source" && binding.Source == s.ID
	}) {
		if s.Key != "" || len(s.Mapping) > 0 {
			return refuse("This source is mapped by its object type; leave its legacy key and mapping empty")
		}
		return checkEvery(s.Every)
	}
	if s.Key == "" || len(s.Mapping) == 0 {
		return refuse("A data source names the row field that identifies a record and maps at least one field")
	}
	seen := map[string]bool{}
	for _, m := range s.Mapping {
		f, known := info.Field(m.To)
		if m.From == "" || !known || f.ReadOnly || seen[m.To] {
			return refuse("Field mapping {to} must name a writable field of {object} once", m.To, s.Object)
		}
		if !slices.Contains([]string{"", "string", "number", "boolean", "date"}, m.Convert) {
			return refuse("Conversion {convert} is not one of string, number, boolean, date", m.Convert)
		}
		seen[m.To] = true
	}
	return checkEvery(s.Every)
}

func (b *Build) checkSourceConnection(c platform.Caller, s Source) *kernel.Error {
	refuse := func(message string, args ...any) *kernel.Error {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, message, args...)
	}
	if s.Connection == "" {
		if s.Profile == "odata" || s.Profile == "table" {
			return refuse("An {profile} source reads through a connection", s.Profile)
		}
		u, err := url.Parse(s.URL)
		if err != nil || u.Host == "" || u.Scheme != "https" && !(u.Scheme == "http" && s.AllowPrivate) {
			return refuse("A data source answers over https; http only to a private address when allowed")
		}
		if s.Header != "" && !strings.Contains(s.Header, ":") {
			return refuse("A request header reads Name: value")
		}
		return nil
	}
	conn, ok := platform.Get[Connection](c, s.Connection)
	if ok {
		fixed, err := b.ConnectionConfiguration(conn)
		if err != nil {
			return refuse(err.Error())
		}
		conn = fixed
	}
	if !ok {
		return refuse("Connection {connection} does not exist", s.Connection)
	}
	switch {
	case (s.Profile == "odata") != (conn.Kind == "odata"), (s.Profile == "table") != (conn.Kind == "postgres"):
		return refuse("A {profile} source needs a matching connection kind", s.Profile)
	case s.Profile == "odata" && s.Entity == "":
		return refuse("An OData source names its entity set")
	case s.Profile == "table" && !tableName(s.Entity):
		return refuse("A table source names schema.table")
	case s.Profile == "table" && !simpleWhere(s.Filter):
		return refuse("A table filter is a simple comparison such as plant = '1000' and active = true")
	}
	return nil
}

func checkEvery(every string) *kernel.Error {
	if every != "" {
		d, parseErr := time.ParseDuration(every)
		if parseErr != nil || d < time.Minute || d > 366*24*time.Hour {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A source pulls every period between 1m and a year, such as 15m, 1h or 24h")
		}
	}
	return nil
}

func (b *Build) submitSourcePulled(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return b.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		var payload struct {
			Pull SourcePull `json:"pull"`
		}
		src, ok := platform.Get[Source](c, s.GetTarget().GetId())
		if !ok || json.Unmarshal(s.GetPayload(), &payload) != nil || payload.Pull.At.IsZero() {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Pull result has no source")
		}
		if src.Last != nil && src.Last.Execution != nil && src.Last.Execution.Pending {
			if err := checkIntegrationExecution(src.Last.Execution, payload.Pull.Execution, c.Member); err != nil {
				return nil, err
			}
			if payload.Pull.DefinitionVersion != src.Last.DefinitionVersion || !payload.Pull.At.Equal(src.Last.At) {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Integration execution changed before its result was accepted")
			}
		} else {
			equivalent, versionErr := retainedExecutionEquivalent(src, payload.Pull.DefinitionVersion)
			if versionErr != nil || !equivalent {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Integration definition changed before its result was accepted")
			}
			if err := checkIntegrationExecution(nil, payload.Pull.Execution, c.Member); err != nil {
				return nil, err
			}
		}
		return func(r *pb.ChangeRecord) {
			pull := payload.Pull
			src.Last, src.Requested = &pull, pull.Execution != nil && pull.Execution.Pending
			if (pull.Execution == nil || !pull.Execution.Pending) && pull.Cursor != "" && pull.Error == "" && pull.Failed == 0 {
				src.Cursor = pull.Cursor
			}
			c.Put(r, src)
		}, nil
	})
}

const (
	SourceType              = "build.source"
	SchemaSourcePulled      = "build.source.pulled"
	SourceBody              = 16 << 20
	ValueSourceRows         = 5000
	ValueSourceFailuresKept = 20
)

type Source struct {
	platform.Record
	Name              string        `json:"name" field:"required,search"`
	Title             string        `json:"title" field:"required,search"`
	Connection        string        `json:"connection,omitempty" ref:"build.connection"`
	Profile           string        `json:"profile,omitempty" choices:"json,csv,odata,table"`
	URL               string        `json:"url,omitempty" title:"URL"`
	Entity            string        `json:"entity,omitempty" title:"Entity or table"`
	Filter            string        `json:"filter,omitempty" title:"Filter" help:"OData $filter, or a WHERE fragment of simple comparisons"`
	Since             string        `json:"since,omitempty" title:"Incremental column" help:"A timestamp or sequence property; each pull reads rows past the cursor"`
	Cursor            string        `json:"cursor,omitempty" field:"readonly"`
	AllowPrivate      bool          `json:"allowPrivate,omitempty" title:"Allow private address" help:"Also accept http and private networks, for on-premise systems"`
	Header            string        `json:"header,omitempty" title:"Request header" help:"One header such as Authorization: Bearer …"`
	Path              string        `json:"path,omitempty" title:"Rows at" help:"Dotted path to the array, such as data.items; empty when the answer is the array"`
	Dataset           string        `json:"dataset,omitempty" ref:"build.dataset" title:"Target dataset"`
	Object            string        `json:"object,omitempty" title:"Target object"`
	Key               string        `json:"key,omitempty" title:"Row id field"`
	Mapping           []SourceField `json:"mapping,omitempty" title:"Field mapping"`
	Stream            bool          `json:"stream,omitempty" title:"Continuous Flow source"`
	Every             string        `json:"every,omitempty" title:"Pull every" help:"A period such as 15m, 1h or 24h; empty: only when asked"`
	State             string        `json:"state" field:"readonly"`
	Publisher         string        `json:"publisher,omitempty" field:"readonly"`
	PublisherScopes   string        `json:"publisherScopes,omitempty" field:"readonly" type:"json" title:"Publisher scopes"`
	Puller            string        `json:"puller,omitempty" field:"readonly" title:"Pulls as"`
	PullerScopes      string        `json:"pullerScopes,omitempty" field:"readonly" type:"json"`
	Requested         bool          `json:"requested,omitempty" field:"readonly" title:"Pull requested"`
	Last              *SourcePull   `json:"last,omitempty" field:"readonly" type:"json" title:"Last pull"`
	DefinitionVersion int           `json:"definitionVersion,omitempty" field:"readonly" title:"Version"`
	Published         string        `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed"`
	Versions          []string      `json:"definitionVersions,omitempty" field:"readonly" type:"json" title:"Published versions"`
}

type SourceField struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Convert string `json:"convert,omitempty"`
}

type SourcePull struct {
	Execution         *IntegrationExecution `json:"execution,omitempty"`
	DefinitionVersion string                `json:"definitionVersion,omitempty"`
	At                time.Time             `json:"at"`
	Rows              int                   `json:"rows"`
	Applied           int                   `json:"applied"`
	Deleted           int                   `json:"deleted,omitempty"`
	Failed            int                   `json:"failed"`
	Merged            int                   `json:"merged,omitempty"`
	Error             string                `json:"error,omitempty"`
	Failures          []SourceFailure       `json:"failures,omitempty"`
	Cursor            string                `json:"cursor,omitempty"`
}

type SourceFailure struct {
	ID      string `json:"id"`
	Outcome string `json:"outcome"`
}

func (s Source) Due(now time.Time) bool {
	if s.State != "published" || s.Stream && s.Dataset == "" {
		return false
	}
	if s.Last != nil && s.Last.Execution != nil && s.Last.Execution.Pending {
		return true
	}
	if s.Last != nil && s.Last.DefinitionVersion != s.ExecutionVersion() {
		return true
	}
	if s.Requested || s.Stream {
		return true
	}
	every, err := time.ParseDuration(s.Every)
	if s.Every == "" || err != nil {
		return false
	}
	return s.Last == nil || !now.Before(s.Last.At.Add(every))
}

type SourceRow struct {
	ID      string
	Payload json.RawMessage
	Key     string
	Verb    string
	Error   string
	Deleted bool       `json:",omitempty"`
	Origin  string     `json:",omitempty"`
	At      *time.Time `json:",omitempty"`
}

func (s Source) Decode(body []byte) ([]map[string]any, error) {
	if s.Profile == "csv" {
		_, rows, err := platform.IntegrationCSVRows(body)
		return rows, err
	}
	return platform.IntegrationRowsAt(body, s.Path)
}

func (s Source) MapRows(rows []map[string]any) ([]SourceRow, error) {
	if len(rows) > ValueSourceRows {
		return nil, fmt.Errorf("the answer holds %d rows; a pull takes at most %d", len(rows), ValueSourceRows)
	}
	out := make([]SourceRow, 0, len(rows))
	for _, row := range rows {
		r := SourceRow{ID: platform.IntegrationScalar(row[s.Key])}
		payload := map[string]any{}
		for _, m := range s.Mapping {
			v, convErr := platform.IntegrationConvert(row[m.From], m.Convert)
			if convErr != nil {
				r.Error = m.To + ": " + convErr.Error()
				break
			}
			if v != nil {
				payload[m.To] = v
			}
		}
		if r.ID == "" && r.Error == "" {
			r.Error = "row has no id"
		}
		r.Payload, _ = json.Marshal(payload)
		out = append(out, r)
	}
	return out, nil
}

func (p *SourcePull) Fail(id, outcome string) {
	p.Failed++
	if len(p.Failures) < ValueSourceFailuresKept {
		p.Failures = append(p.Failures, SourceFailure{ID: id, Outcome: outcome})
	}
}

func (s Source) Advance(rows []map[string]any) string {
	if s.Since == "" {
		return ""
	}
	best := s.Cursor
	for _, row := range rows {
		if v := platform.IntegrationScalar(row[s.Since]); v != "" {
			a, anum := new(big.Rat).SetString(v)
			b, bnum := new(big.Rat).SetString(best)
			at, aerr := time.Parse(time.RFC3339Nano, v)
			bt, berr := time.Parse(time.RFC3339Nano, best)
			if best == "" || anum && bnum && a.Cmp(b) > 0 || aerr == nil && berr == nil && at.After(bt) || (!anum || !bnum) && (aerr != nil || berr != nil) && v > best {
				best = v
			}
		}
	}
	return best
}
