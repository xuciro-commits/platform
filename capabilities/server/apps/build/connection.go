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

// Connections (ADR-0070, ADR-0069 I-A): how the tenant reaches one external
// system - a kind, an address and the *name* of a secret in the host's store.
// Sources read through a connection; the credential itself never enters a
// record, a candidate or a release, so the same definition promotes to another
// environment that holds its own secret under the same name.
const (
	ConnectionType          = "build.connection"
	SchemaConnectionChecked = "build.connection.checked"
)

// ConnectionKinds are the systems a connection reaches.
var ConnectionKinds = []string{"http", "odata", "postgres"}

type Connection struct {
	platform.Record
	Name  string `json:"name" field:"required,search"`
	Title string `json:"title" field:"required,search"`
	// Kind is http (REST or files), odata (an OData v2/v4 service root, e.g. SAP Gateway) or postgres (a read-only database).
	Kind string `json:"kind" field:"required" choices:"http,odata,postgres"`
	// Address is the service root URL, or for postgres a DSN without a password.
	Address string `json:"address" field:"required" help:"Service root URL, or postgres://user@host:5432/db?sslmode=require without the password"`
	// Secret names the credential in the host's secret store: the Authorization header's value for http/odata, the password for postgres.
	Secret       string           `json:"secret,omitempty" title:"Secret name" help:"The credential's name in the host's secret store, never the credential"`
	AllowPrivate bool             `json:"allowPrivate,omitempty" title:"Allow private address" help:"Also accept http and private networks, for on-premise systems"`
	State        string           `json:"state" field:"readonly"`
	Requested    bool             `json:"requested,omitempty" field:"readonly" title:"Check requested"`
	Last         *ConnectionCheck `json:"last,omitempty" field:"readonly" type:"json" title:"Last check"`
}

// ConnectionCheck is what one check found.
type ConnectionCheck struct {
	At    time.Time `json:"at"`
	OK    bool      `json:"ok"`
	Error string    `json:"error,omitempty"`
	// Detail is what answered: a status, a server version, an OData version.
	Detail string `json:"detail,omitempty"`
}

func (b *Build) connectionEntity() platform.Entity {
	return platform.Entity{Type: ConnectionType, Title: "Connection", Plural: "Connections", Model: Connection{}, Display: "title",
		Description: "An external system the tenant reads from: a kind, an address and the name of its credential in the host's secret store.",
		Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant}},
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder}, Capability: "integrations"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "ready", Title: "Ready", Tone: "success"}},
			Transitions: []platform.Transition{
				{Name: "check", Title: "Check", Description: "Reach the system once from the host and keep what answered.", From: []string{"draft", "ready"}, To: []string{"ready"}, Roles: []string{Builder}, Capability: "integrations", Payload: []platform.Field{}, Do: b.checkConnection},
				{Name: "retire", Title: "Retire", Description: "Stop sources from using the connection; the definition stays.", From: []string{"ready"}, To: []string{"draft"}, Roles: []string{Builder}, Capability: "integrations", Payload: []platform.Field{}}}}}
}

func connectionActions() []platform.Action {
	return []platform.Action{{Schema: SchemaConnectionChecked, Target: ConnectionType, Capability: "integrations", Title: "Keep check result", Description: "Retain what one check of a connection found.", Roles: []string{Builder},
		Payload: []platform.Field{{Name: "check", Type: "json", Required: true, Description: "When, whether it answered and what"}}}}
}

func (b *Build) checkConnection(_ platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	c, ok := record.(*Connection)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if err := b.validConnection(*c); err != nil {
		return err
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
	u, err := url.Parse(c.Address)
	if err != nil || u.Host == "" {
		return refuse("The address is not a URL")
	}
	switch c.Kind {
	case "postgres":
		if u.Scheme != "postgres" && u.Scheme != "postgresql" {
			return refuse("A postgres connection's address starts with postgres://")
		}
		if _, has := u.User.Password(); has {
			return refuse("The address carries no password; name a secret instead")
		}
	default:
		if u.Scheme != "https" && !(u.Scheme == "http" && c.AllowPrivate) {
			return refuse("A connection answers over https; http only to a private address when allowed")
		}
	}
	return nil
}

// Resolve is the address a source on this connection reads: an absolute URL
// stays, a relative one joins the service root.
func (c Connection) Resolve(ref string) string {
	if ref == "" {
		return c.Address
	}
	if strings.Contains(ref, "://") {
		return ref
	}
	return strings.TrimRight(c.Address, "/") + "/" + strings.TrimLeft(ref, "/")
}

func (b *Build) submitConnectionChecked(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return b.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		var payload struct {
			Check ConnectionCheck `json:"check"`
		}
		conn, ok := platform.Get[Connection](c, s.GetTarget().GetId())
		if !ok || json.Unmarshal(s.GetPayload(), &payload) != nil || payload.Check.At.IsZero() {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Check result has no connection")
		}
		return func(r *pb.ChangeRecord) {
			check := payload.Check
			conn.Last, conn.Requested = &check, false
			c.Put(r, conn)
		}, nil
	})
}
