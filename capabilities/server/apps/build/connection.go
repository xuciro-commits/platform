package build

import (
	"encoding/json"
	"net/url"
	"reflect"
	"slices"

	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/host"
	"platformserver/platform"
)

func (b *Build) connectionEntity() platform.Entity {
	return platform.Entity{Type: ConnectionType, Title: "Connection", Plural: "Connections", Model: Connection{}, Display: "title",
		Validate: func(c platform.Caller, record any) *kernel.Error {
			next := record.(*Connection)
			if err := b.validConnection(*next); err != nil {
				return err
			}
			if old, ok := platform.Get[Connection](c, next.ID); ok && (old.Kind != next.Kind || old.Address != next.Address || old.Secret != next.Secret || old.AllowPrivate != next.AllowPrivate || !reflect.DeepEqual(old.Protocol, next.Protocol)) {
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
	if c.Protocol != nil {
		if c.Kind != "http" {
			return refuse("A protocol package uses an HTTP gateway connection")
		}
		if err := c.Protocol.Check(); err != nil {
			return refuse("Protocol configuration is invalid: {reason}", err.Error())
		}
		packages, ok := b.host.(host.DriverPackages)
		if !ok {
			return refuse("Protocol packages are unavailable on this host")
		}
		if err := packages.CheckDriverBinding(*c.Protocol); err != nil {
			return refuse("Protocol package is unavailable: {reason}", err.Error())
		}
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
			check.Samples = nil // Samples are read-only preview data, never accepted business state.
			conn.Last, conn.Requested = &check, false
			conn.State = "draft"
			if check.OK {
				conn.State = "ready"
			}
			c.Put(r, conn)
		}, nil
	})
}
