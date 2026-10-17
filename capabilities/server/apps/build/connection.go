package build

import (
	"encoding/json"
	"net/url"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
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
	At              time.Time `json:"at"`
	OK              bool      `json:"ok"`
	Error           string    `json:"error,omitempty"`
	Detail          string    `json:"detail,omitempty"`
	BindingRevision string    `json:"bindingRevision,omitempty"`
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
