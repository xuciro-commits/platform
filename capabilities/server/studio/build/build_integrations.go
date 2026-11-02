package build

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	dataowner "platformserver/services/data"
)

func (b *Build) connectionEntity() platform.Entity {
	return platform.Entity{Type: ConnectionType, Title: "Connection", Plural: "Connections", Model: Connection{}, Display: "title",
		Validate: func(c platform.Caller, record any) *kernel.Error {
			next := record.(*Connection)
			if err := b.validConnection(*next); err != nil {
				return err
			}
			if old, ok := platform.Get[Connection](c, next.ID); ok && (old.Kind != next.Kind || old.Address != next.Address || old.Secret != next.Secret || old.AllowPrivate != next.AllowPrivate) {
				next.State, next.Requested, next.Last = "draft", false, nil
			}
			return nil
		},
		Description: "An external system the tenant reads from: a kind, an address and the name of its credential in the host's secret store.",
		Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant, Integrator: platform.ScopeTenant}},
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder, Integrator}, Capability: "integrations"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "ready", Title: "Ready", Tone: "success"}},
			Transitions: []platform.Transition{
				{Name: "check", Title: "Check", Description: "Reach the system once from the host and keep what answered.", From: []string{"draft", "ready"}, To: []string{"draft"}, Roles: []string{Builder, Integrator}, Capability: "integrations", Payload: []platform.Field{}, Do: b.checkConnection},
				{Name: "retire", Title: "Retire", Description: "Stop sources from using the connection; the definition stays.", From: []string{"ready"}, To: []string{"draft"}, Roles: []string{Builder, Integrator}, Capability: "integrations", Payload: []platform.Field{}}}}}
}

func connectionActions() []platform.Action {
	return []platform.Action{{Schema: SchemaConnectionChecked, Target: ConnectionType, Capability: "integrations", Title: "Keep check result", Description: "Retain what one check of a connection found.", Automation: true,
		Payload: []platform.Field{{Name: "revision", Type: "integer", Required: true, Description: "The connection revision checked"}, {Name: "check", Type: "json", Required: true, Description: "When, whether it answered and what"}}}}
}

func (b *Build) checkConnection(_ platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	c, ok := record.(*Connection)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if err := b.validConnection(*c); err != nil {
		return err
	}
	if c.Address == "" {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Configure the connection address before checking it")
	}
	c.Requested = true
	return nil
}

func (b *Build) validConnection(c Connection) *kernel.Error {
	refuse := func(message string, args ...any) *kernel.Error {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, message, args...)
	}
	if err := b.checkName(c.Name, c.ID); err != nil {
		return refuse(err.Error())
	}
	if !slices.Contains(ConnectionKinds, c.Kind) {
		return refuse("A connection is one of http, odata, postgres")
	}
	if c.Address == "" && c.State != "ready" && !c.Requested {
		return nil
	}
	u, err := url.Parse(c.Address)
	if err != nil || u.Host == "" {
		return refuse("The address is not a URL")
	}
	if _, has := u.User.Password(); has || u.Query().Has("password") || u.Query().Has("passfile") {
		return refuse("The address carries no password; name a secret instead")
	}
	switch c.Kind {
	case "postgres":
		if u.Scheme != "postgres" && u.Scheme != "postgresql" {
			return refuse("A postgres connection's address starts with postgres://")
		}
	default:
		if u.Scheme != "https" && !(u.Scheme == "http" && c.AllowPrivate) {
			return refuse("A connection answers over https; http only to a private address when allowed")
		}
	}
	return nil
}

func (b *Build) submitConnectionChecked(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return b.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		var payload struct {
			Revision uint32          `json:"revision"`
			Check    ConnectionCheck `json:"check"`
		}
		conn, ok := platform.Get[Connection](c, s.GetTarget().GetId())
		if !ok || json.Unmarshal(s.GetPayload(), &payload) != nil || payload.Check.At.IsZero() || !conn.Requested || conn.Revision != payload.Revision {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Check result has no connection")
		}
		return func(r *pb.ChangeRecord) {
			check := payload.Check
			conn.Last, conn.Requested = &check, false
			conn.State = "draft"
			if check.OK {
				conn.State = "ready"
			}
			c.Put(r, conn)
		}, nil
	})
}

func (b *Build) datasetEntity() platform.Entity {
	return platform.Entity{Type: DatasetType, Title: "Dataset", Plural: "Datasets", Model: Dataset{}, Display: "title",
		Validate: func(c platform.Caller, value any) *kernel.Error {
			next := value.(*Dataset)
			if current, ok := platform.Get[Dataset](c, next.ID); ok && current.Key != next.Key && next.Key != "" {
				rows, _, err := DatasetRows(c, next.ID, 0)
				if err == nil {
					_, err = dataowner.DatasetTransaction(next.Key, "snapshot", nil, rows)
				}
				if err != nil {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
				}
			}
			return nil
		},
		Description: "Rows as they came from a source or a pipeline: versioned, schema inferred, read only by builders.",
		Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant, Integrator: platform.ScopeTenant}},
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder, Integrator}, Capability: "integrations"}}
}

func (b *Build) datasetVersionEntity() platform.Entity {
	return platform.Entity{Type: DatasetVersionType, Title: "Dataset version", Plural: "Dataset versions", Model: DatasetVersion{}, Display: "version",
		Description: "One version of a dataset: its rows at one load.",
		Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant, Integrator: platform.ScopeTenant}},
		Standard:    platform.Standard{Roles: []string{Builder, Integrator}, Capability: "integrations"}}
}

func datasetActions() []platform.Action {
	return []platform.Action{{Schema: SchemaDatasetLoad, Target: DatasetType, Capability: "integrations", Title: "Load a version", Description: "Load rows as the dataset's next transaction and version.", Roles: []string{Builder, Integrator},
		Payload: []platform.Field{{Name: "type", Type: "choice", Choices: []string{string(DatasetTransactionSnapshot), string(DatasetTransactionAppend), string(DatasetTransactionUpdate), string(DatasetTransactionDelete)}, Default: json.RawMessage(`"snapshot"`), Description: "The transaction type; defaults to snapshot"}, {Name: "rows", Type: "json", Description: "The rows, an array of objects"}, {Name: "csv", Type: "longtext", Description: "Or the rows as CSV under a header line"}, {Name: "producer", Type: "string", Description: "The source or pipeline that produced them"}, {Name: "marking", Type: "string", Description: "The marking the rows carry; raises the dataset's"}}},
		{Schema: SchemaDatasetProjected, Target: DatasetType, Capability: "integrations", Title: "Keep projection result", Description: "Retain what projecting one version into an object type did.", Roles: []string{Builder},
			Payload: []platform.Field{{Name: "projection", Type: "json", Required: true, Description: "The object type, version, counts and why the projection paused"}}}}
}

func (b *Build) submitDatasetProjected(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return b.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		var payload struct {
			Projection DatasetProjection `json:"projection"`
		}
		ds, ok := platform.Get[Dataset](c, s.GetTarget().GetId())
		if !ok || json.Unmarshal(s.GetPayload(), &payload) != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Projection result has no dataset")
		}
		p := payload.Projection
		if p.Object == "" || p.Version < 1 || p.Version > ds.Version || p.At.IsZero() || len(p.Problems) > 5 || p.Written+p.Refused > p.Rows {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A projection result names an object type, a loaded version and counts that add up")
		}
		return func(r *pb.ChangeRecord) {
			ds.Projections = append(slices.DeleteFunc(slices.Clone(ds.Projections), func(x DatasetProjection) bool { return x.Object == p.Object }), p)
			slices.SortFunc(ds.Projections, func(a, b DatasetProjection) int { return strings.Compare(a.Object, b.Object) })
			c.Put(r, ds)
		}, nil
	})
}

func DatasetSchema(rows []map[string]any, order []string) []DatasetField {
	types := map[string]string{}
	for _, row := range rows {
		for k, v := range row {
			t := platform.IntegrationTypeOf(v)
			if prev, ok := types[k]; !ok || prev == "" {
				types[k] = t
			} else if prev != t && t != "" {
				types[k] = "string"
			}
		}
	}
	out := make([]DatasetField, 0, len(types))
	for k, t := range types {
		if t == "" {
			t = "string"
		}
		out = append(out, DatasetField{Name: k, Type: t})
	}
	rank := map[string]int{}
	for _, name := range order {
		if _, ok := rank[name]; !ok {
			rank[name] = len(rank)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, aok := rank[out[i].Name]
		b, bok := rank[out[j].Name]
		if aok != bok {
			return aok
		}
		if aok {
			return a < b
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func (b *Build) submitDatasetLoad(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return b.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		var payload struct {
			Type     DatasetTransactionType `json:"type"`
			Rows     []map[string]any       `json:"rows"`
			CSV      string                 `json:"csv"`
			Producer string                 `json:"producer"`
			Marking  string                 `json:"marking"`
		}
		ds, ok := platform.Get[Dataset](c, s.GetTarget().GetId())
		if !ok {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The dataset does not exist")
		}
		fixed, err := b.ExecutionDataset(ds)
		if err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
		}
		if json.Unmarshal(s.GetPayload(), &payload) != nil || (payload.Rows == nil) == (payload.CSV == "") {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A load carries an array of rows or a CSV body")
		}
		if payload.Type == "" {
			payload.Type = DatasetTransactionSnapshot
		}
		switch payload.Type {
		case DatasetTransactionSnapshot, DatasetTransactionAppend, DatasetTransactionUpdate, DatasetTransactionDelete:
		default:
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		}
		var order []string
		for _, f := range ds.Schema {
			order = append(order, f.Name)
		}
		if payload.CSV != "" {
			columns, rows, err := platform.IntegrationCSVRows([]byte(payload.CSV))
			if err != nil || len(rows) == 0 {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The file is not CSV with a header line and at least one row")
			}
			payload.Rows, order = rows, append(order, columns...)
		}
		var prior []map[string]any
		if payload.Type != DatasetTransactionSnapshot {
			prior, _, err = DatasetRows(c, ds.ID, ds.Version)
			if err != nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
			}
		}
		payload.Rows, err = dataowner.DatasetTransaction(fixed.Key, string(payload.Type), prior, payload.Rows)
		if err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		if payload.Rows == nil {
			payload.Rows = []map[string]any{}
		}

		if len(payload.Rows) > ValueSourceRows {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A version holds at most {n} rows", ValueSourceRows)
		}
		return func(r *pb.ChangeRecord) {
			schema := DatasetSchema(payload.Rows, order)
			var drift []string
			before := map[string]bool{}
			for _, f := range ds.Schema {
				before[f.Name] = true
			}
			after := map[string]bool{}
			for _, f := range schema {
				after[f.Name] = true
				if ds.Version > 0 && !before[f.Name] {
					drift = append(drift, "+"+f.Name)
				}
			}
			for name := range before {
				if !after[name] {
					drift = append(drift, "-"+name)
				}
			}
			sort.Strings(drift)
			data, _ := json.Marshal(payload.Rows)
			ds.Version++
			ds.Schema = schema
			ds.Last = &DatasetLoad{At: now, Version: ds.Version, Rows: len(payload.Rows), Bytes: len(data), Drift: drift}
			if payload.Producer != "" {
				ds.Producer = payload.Producer
			}
			ds.Marking = platform.HigherMarking(ds.Marking, payload.Marking)
			c.Put(r, ds)
			version := DatasetVersion{Record: platform.Record{ID: VersionID(ds.ID, ds.Version)}, Dataset: ds.ID, Version: ds.Version, Type: payload.Type, At: now, Rows: len(payload.Rows), Data: data}
			if !c.Replaying {
				version.Request = s.GetIdempotencyKey()
			}
			c.Put(r, version)
			keep := fixed.Keep
			if keep <= 0 {
				keep = DatasetKeepDefault
			}
			if old := ds.Version - keep; old >= 1 {
				if v, ok := platform.Get[DatasetVersion](c, VersionID(ds.ID, old)); ok && !v.Archived {
					v.Archived, v.Data = true, json.RawMessage("[]")
					c.Put(r, v)
				}
			}
		}, nil
	})
}

func checkIntegrationExecution(prior, next *IntegrationExecution, member platform.Member) *kernel.Error {
	if next == nil {
		if prior != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The pending integration execution must finish first")
		}
		return nil
	}
	digest, err := hex.DecodeString(next.Snapshot)
	if next.ID == "" || len(next.ID) > 64 || err != nil || len(digest) != 32 || next.Member != member.ID {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Integration execution has no retained input")
	}
	if prior != nil {
		fixed := *next
		fixed.Pending = prior.Pending
		if !reflect.DeepEqual(*prior, fixed) {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The pending integration execution must finish first")
		}
	} else if !next.Pending || !reflect.DeepEqual(next.Scopes, member.Scopes) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Integration execution was not prepared")
	}
	return nil
}

const (
	ConnectionType          = "build.connection"
	SchemaConnectionChecked = "build.connection.checked"
)

var ConnectionKinds = []string{"http", "odata", "postgres"}

type Connection struct {
	platform.Record
	Name              string           `json:"name" field:"required,search" source:"record"`
	Title             string           `json:"title" field:"required,search" source:"record"`
	Kind              string           `json:"kind" field:"required" choices:"http,odata,postgres" source:"record"`
	Address           string           `json:"address" help:"Service root URL, or postgres://user@host:5432/db?sslmode=require without the password" source:"record"`
	Secret            string           `json:"secret,omitempty" title:"Secret name" help:"The credential's name in the host's secret store, never the credential" source:"record"`
	AllowPrivate      bool             `json:"allowPrivate,omitempty" title:"Allow private address" help:"Also accept http and private networks, for on-premise systems"`
	Marking           string           `json:"marking,omitempty" choices:"internal,confidential,restricted" title:"Marking" help:"What is read through it is at least this: internal, confidential or restricted"`
	State             string           `json:"state" field:"readonly" source:"record"`
	Requested         bool             `json:"requested,omitempty" field:"readonly" title:"Check requested"`
	Last              *ConnectionCheck `json:"last,omitempty" field:"readonly" type:"json" title:"Last check"`
	DefinitionVersion int              `json:"definitionVersion,omitempty" field:"readonly" title:"Version"`
	Published         string           `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed" source:"record"`
	Versions          []string         `json:"definitionVersions,omitempty" field:"readonly" type:"json" title:"Published versions"`
}

type ConnectionCheck struct {
	At     time.Time `json:"at"`
	OK     bool      `json:"ok"`
	Error  string    `json:"error,omitempty"`
	Detail string    `json:"detail,omitempty"`
}

func (c Connection) Resolve(ref string) string {
	if ref == "" {
		return c.Address
	}
	if strings.Contains(ref, "://") {
		return ref
	}
	return strings.TrimRight(c.Address, "/") + "/" + strings.TrimLeft(ref, "/")
}

const (
	DatasetType            = "build.dataset"
	DatasetVersionType     = "build.datasetversion"
	SchemaDatasetLoad      = "build.dataset.load"
	SchemaDatasetProjected = "build.dataset.projected"
	DatasetKeepDefault     = 3
)

type DatasetTransactionType string

const (
	DatasetTransactionSnapshot DatasetTransactionType = "snapshot"
	DatasetTransactionAppend   DatasetTransactionType = "append"
	DatasetTransactionUpdate   DatasetTransactionType = "update"
	DatasetTransactionDelete   DatasetTransactionType = "delete"
)

type Dataset struct {
	platform.Record
	Key               string              `json:"key,omitempty" title:"Key column"`
	Stream            bool                `json:"stream,omitempty" title:"Streaming dataset"`
	Name              string              `json:"name" field:"required,search"`
	Title             string              `json:"title" field:"required,search"`
	Keep              int                 `json:"keep,omitempty" title:"Versions kept" help:"How many versions stay readable; default 3"`
	Marking           string              `json:"marking,omitempty" choices:"internal,confidential,restricted" title:"Marking"`
	Producer          string              `json:"producer,omitempty" field:"readonly"`
	Schema            []DatasetField      `json:"schema,omitempty" field:"readonly" type:"json"`
	Version           int                 `json:"version,omitempty" field:"readonly" title:"Latest version"`
	Last              *DatasetLoad        `json:"last,omitempty" field:"readonly" type:"json" title:"Latest load"`
	Projections       []DatasetProjection `json:"projections,omitempty" field:"readonly" type:"json" title:"Projections"`
	DefinitionVersion int                 `json:"definitionVersion,omitempty" field:"readonly" title:"Version"`
	Published         string              `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed"`
	Versions          []string            `json:"definitionVersions,omitempty" field:"readonly" type:"json" title:"Published versions"`
}

type DatasetField struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type DatasetProjection struct {
	Object   string    `json:"object"`
	Version  int       `json:"version"`
	At       time.Time `json:"at"`
	Rows     int       `json:"rows"`
	Written  int       `json:"written"`
	Refused  int       `json:"refused"`
	Paused   string    `json:"paused,omitempty"`
	Problems []string  `json:"problems,omitempty"`
}

func (d Dataset) Projection(object string) (DatasetProjection, bool) {
	for _, p := range d.Projections {
		if p.Object == object {
			return p, true
		}
	}
	return DatasetProjection{}, false
}

type DatasetLoad struct {
	At      time.Time `json:"at"`
	Version int       `json:"version"`
	Rows    int       `json:"rows"`
	Bytes   int       `json:"bytes"`
	Drift   []string  `json:"drift,omitempty"`
}

type DatasetVersion struct {
	platform.Record
	Dataset string                 `json:"dataset" field:"required" ref:"build.dataset"`
	Version int                    `json:"version" field:"required"`
	Type    DatasetTransactionType `json:"type" choices:"snapshot,append,update,delete"`
	At      time.Time              `json:"at"`
	Rows    int                    `json:"rows"`
	Data    json.RawMessage        `json:"data" type:"json"`
	Request string                 `json:"requestKey,omitempty" field:"readonly" title:"Request key"`
}

func VersionID(dataset string, version int) string { return fmt.Sprintf("%s@%d", dataset, version) }

func DatasetRows(c platform.Caller, dataset string, version int) ([]map[string]any, int, error) {
	ds, ok := platform.Get[Dataset](c, dataset)
	if !ok {
		return nil, 0, fmt.Errorf("the dataset %s does not exist", dataset)
	}
	if version <= 0 {
		version = ds.Version
	}
	if version == 0 {
		return nil, 0, nil
	}
	v, ok := platform.Get[DatasetVersion](c, VersionID(dataset, version))
	if !ok {
		return nil, 0, fmt.Errorf("version %d of %s is no longer kept", version, ds.Title)
	}
	var rows []map[string]any
	if err := json.Unmarshal(v.Data, &rows); err != nil {
		return nil, 0, fmt.Errorf("version %d of %s is unreadable", version, ds.Title)
	}
	return rows, version, nil
}

type IntegrationExecution struct {
	ID       string         `json:"id"`
	Member   string         `json:"member"`
	Scopes   []string       `json:"scopes,omitempty"`
	Pending  bool           `json:"pending,omitempty"`
	Snapshot string         `json:"snapshot"`
	Inputs   map[string]int `json:"inputs,omitempty"`
}
