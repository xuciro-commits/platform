package data

import (
	"crypto/hmac"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	ontology "platformserver/services/ontology"
)

const ListenerType = "build.listener"

var listenerSegment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,255}$`)

type Listener struct {
	platform.Record
	Name              string   `json:"name" field:"required,search"`
	Title             string   `json:"title" field:"required,search"`
	Connector         string   `json:"connector" field:"required" title:"Connector input"`
	Path              string   `json:"path" field:"required" title:"Webhook path"`
	Secret            string   `json:"secret" field:"required" title:"Secret name" help:"The credential's name in the host's secret store, never the credential"`
	Enabled           bool     `json:"enabled"`
	State             string   `json:"state" field:"readonly"`
	DefinitionVersion int      `json:"definitionVersion,omitempty" field:"readonly" title:"Version"`
	Published         string   `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed"`
	Versions          []string `json:"definitionVersions,omitempty" field:"readonly" type:"json" title:"Published versions"`
}

func (o *Owner) listenerEntity() platform.Entity {
	roles := []string{Builder, Integrator}
	return platform.Entity{Type: ListenerType, Title: "Listener", Plural: "Listeners", Model: Listener{}, Display: "title",
		Description: "An inbound webhook that delivers payloads to an existing connector input.",
		Scope:       integrationScope(),
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: roles, Capability: "integrations"},
		Validate: func(c platform.Caller, value any) *kernel.Error {
			next := value.(*Listener)
			if !ontology.Named(next.Name) || !listenerSegment.MatchString(next.Path) || !listenerSegment.MatchString(next.Secret) || strings.TrimSpace(next.Connector) == "" {
				return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
			}
			filter := platform.Expr{Op: "or", Kind: "boolean", Args: []platform.Expr{
				{Op: "=", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("text", "path"), platform.ExprLiteralValue("text", next.Path)}},
				{Op: "=", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("text", "name"), platform.ExprLiteralValue("text", next.Name)}},
			}}
			rows, _, err := platform.Find[Listener](c, platform.Query{Filter: &filter})
			if err != nil {
				return err
			}
			for _, row := range rows {
				if row.ID != next.ID {
					return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
				}
			}
			return nil
		},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}},
			Transitions: []platform.Transition{
				{Name: "publish", Title: "Publish", From: []string{"draft", "published"}, To: []string{"published"}, Roles: roles, Capability: "integrations", Payload: []platform.Field{}, Do: o.publishListener},
				{Name: "pause", Title: "Pause", From: []string{"published"}, To: []string{"draft"}, Roles: roles, Capability: "integrations", Payload: []platform.Field{}, Do: func(_ platform.Caller, value any, _ json.RawMessage, _ time.Time) *kernel.Error {
					value.(*Listener).Enabled = false
					return nil
				}},
			}},
	}
}

func (o *Owner) publishListener(c platform.Caller, value any, _ json.RawMessage, _ time.Time) *kernel.Error {
	listener := value.(*Listener)
	if !c.Replaying {
		fixed, err := o.freezeDirectIntegration(platform.AssetListener, *listener)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
		}
		*listener = fixed.(Listener)
	}
	listener.Enabled = true
	return nil
}

func (o *Owner) ExecutionListener(live Listener) (Listener, error) {
	if live.Published == "" {
		return Listener{}, fmt.Errorf("accepted listener definition is missing")
	}
	fixed, err := o.acceptedIntegration(live, platform.AssetListener)
	if err != nil {
		return Listener{}, err
	}
	listener := fixed.(Listener)
	listener.Record, listener.State, listener.Enabled = live.Record, live.State, live.Enabled
	listener.DefinitionVersion, listener.Published, listener.Versions = live.DefinitionVersion, live.Published, live.Versions
	return listener, nil
}

func (o *Owner) AuthorizeListener(live []Listener, path, secret, origin string, credential func(name, consumer, address string) ([]byte, bool)) (string, *kernel.Error) {
	var matches []Listener
	for _, row := range live {
		if !row.Enabled || row.State != "published" {
			continue
		}
		listener, err := o.ExecutionListener(row)
		if err != nil {
			return "", &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
		if listener.Path == path {
			matches = append(matches, listener)
		}
	}
	if len(matches) != 1 {
		return "", &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	listener := matches[0]
	value, ok := credential(listener.Secret, "listener/"+listener.ID, origin)
	if !ok || len(value) == 0 || secret == "" || !hmac.Equal(value, []byte(secret)) {
		return "", &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	return listener.Connector, nil
}
