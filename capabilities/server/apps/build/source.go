package build

import (
	"encoding/hex"
	"encoding/json"

	"net/url"
	construction "platformserver/internal/host/buildapi"
	"reflect"
	"regexp"
	"slices"

	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (b *Build) sourceEntity() platform.Entity {
	return platform.Entity{Type: construction.SourceType, Title: "Data source", Plural: "Data sources", Model: construction.Source{}, Display: "title", Description: "An external JSON endpoint whose rows become records of one object, pulled on a period or on request.",
		Validate: func(c platform.Caller, record any) *kernel.Error {
			s := record.(*construction.Source)
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
		Scope:    platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{construction.Builder: platform.ScopeTenant, construction.Integrator: platform.ScopeTenant}},
		Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{construction.Builder, construction.Integrator}, Capability: "integrations"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}},
			Transitions: []platform.Transition{
				{Name: "publish", Title: "Publish", Description: "Check the source and let the builder's job pull it.", From: []string{"draft", "published"}, To: []string{"published"}, Roles: []string{construction.Builder, construction.Integrator}, Capability: "integrations", Payload: []platform.Field{}, Do: b.publishSource},
				{Name: "pull", Title: "Pull now", Description: "Ask for one pull at the next tick.", From: []string{"published"}, To: []string{"published"}, Roles: []string{construction.Builder, construction.Integrator}, Capability: "integrations", Payload: []platform.Field{}, Do: requestPull},
				{Name: "reset", Title: "Reset cursor", Description: "Forget the incremental cursor and pull everything at the next tick.", From: []string{"published"}, To: []string{"published"}, Roles: []string{construction.Builder, construction.Integrator}, Capability: "integrations", Payload: []platform.Field{}, Do: resetCursor},
				{Name: "pause", Title: "Pause", Description: "Stop pulling; the mapping stays.", From: []string{"published"}, To: []string{"draft"}, Roles: []string{construction.Builder, construction.Integrator}, Capability: "integrations", Payload: []platform.Field{}}}}}
}

func sourceActions() []platform.Action {
	return []platform.Action{{Schema: construction.SchemaSourcePulled, Target: construction.SourceType, Capability: "integrations", Title: "Keep pull result", Description: "Retain what one pull of a data source decided.", Roles: []string{construction.Builder, construction.Integrator},
		Payload: []platform.Field{{Name: "pull", Type: "json", Required: true, Description: "The pull's counts and first failures"}}}}
}

func requestPull(_ platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	s, ok := record.(*construction.Source)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if s.Stream {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A continuous source is consumed through its Flow instance")
	}
	s.Requested = true
	return nil
}

func resetCursor(_ platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	s, ok := record.(*construction.Source)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if s.Stream {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A continuous source cursor belongs to its Flow instance")
	}
	if s.Last != nil && s.Last.Execution != nil && s.Last.Execution.Pending {
		last, execution := *s.Last, *s.Last.Execution
		execution.Pending = false
		last.Execution = &execution
		last.Error = "The previous pull was stopped by resetting its cursor"
		s.Last = &last
	}
	s.Cursor, s.Requested = "", true
	return nil
}

func (b *Build) publishSource(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	s, ok := record.(*construction.Source)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if err := b.checkSource(c, *s); err != nil {
		return err
	}
	s.Puller, s.Requested = c.ID, !s.Stream
	if !c.Replaying {
		fixed, err := b.freezeDirectIntegration(platform.AssetSource, *s)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		*s = fixed.(construction.Source)
	}
	if s.Stream {
		s.Cursor, s.Last = "", nil
	}
	return nil
}

func (b *Build) checkSource(c platform.Caller, s construction.Source) *kernel.Error {
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
		if s.Profile != "table" || s.Since == "" || s.Object != "" || s.Dataset != "" || s.Key != "" || len(s.Mapping) != 0 || s.Every != "" {
			return refuse("A continuous source uses an incremental PostgreSQL table, without a materialization target or import schedule")
		}
		return nil
	}
	if s.Dataset != "" {
		if s.Object != "" {
			return refuse("A source feeds either a dataset or an object")
		}
		if _, ok := platform.Get[construction.Dataset](c, s.Dataset); !ok {
			return refuse("The target dataset does not exist")
		}
		return checkEvery(s.Every)
	}
	if s.Connection != "" {
		if conn, ok := platform.Get[construction.Connection](c, s.Connection); ok {
			fixed, err := b.ExecutionConnection(conn)
			if err != nil {
				return refuse(err.Error())
			}
			if construction.Guarded(fixed.Marking) {
				return refuse("A confidential or restricted connection feeds a dataset; use a pipeline to apply field access rules")
			}
		}
	}
	info, ok := b.lookupEntity(s.Object)
	if !ok {
		return refuse("The target object {object} is not installed", s.Object)
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

func (b *Build) checkSourceConnection(c platform.Caller, s construction.Source) *kernel.Error {
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
	conn, ok := platform.Get[construction.Connection](c, s.Connection)
	if ok {
		fixed, err := b.ExecutionConnection(conn)
		if err != nil {
			return refuse(err.Error())
		}
		conn = fixed
	}
	if !ok || conn.State != "ready" {
		return refuse("The connection is not ready; check it first")
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
			Pull construction.SourcePull `json:"pull"`
		}
		src, ok := platform.Get[construction.Source](c, s.GetTarget().GetId())
		if !ok || json.Unmarshal(s.GetPayload(), &payload) != nil || payload.Pull.At.IsZero() {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Pull result has no source")
		}
		if src.Last != nil && src.Last.Execution != nil && src.Last.Execution.Pending {
			if err := checkIntegrationExecution(src.Last.Execution, payload.Pull.Execution, c.ID); err != nil {
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
			if err := checkIntegrationExecution(nil, payload.Pull.Execution, c.ID); err != nil {
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

func checkIntegrationExecution(prior, next *construction.IntegrationExecution, member string) *kernel.Error {
	if next == nil {
		if prior != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The pending integration execution must finish first")
		}
		return nil // historical/direct-development result without a preparation
	}
	digest, err := hex.DecodeString(next.Snapshot)
	if next.ID == "" || len(next.ID) > 64 || err != nil || len(digest) != 32 || next.Member != member {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Integration execution has no retained input")
	}
	if prior != nil {
		fixed := *next
		fixed.Pending = prior.Pending
		if !reflect.DeepEqual(*prior, fixed) {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The pending integration execution must finish first")
		}
	} else if !next.Pending {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Integration execution was not prepared")
	}
	return nil
}

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

// simpleWhere accepts "col op literal [and ...]" with quoted strings, numbers,
// true/false and null - enough to pick a plant or a status, never a statement.
var simpleWhereRE = regexp.MustCompile(`^\s*[A-Za-z_][A-Za-z0-9_]*\s*(=|<>|!=|<|>|<=|>=)\s*('[^';]*'|-?[0-9]+(\.[0-9]+)?|true|false|null)(\s+and\s+[A-Za-z_][A-Za-z0-9_]*\s*(=|<>|!=|<|>|<=|>=)\s*('[^';]*'|-?[0-9]+(\.[0-9]+)?|true|false|null))*\s*$`)

func simpleWhere(s string) bool { return s == "" || simpleWhereRE.MatchString(strings.ToLower(s)) }
