package buildapi

import (
	"strings"
	"time"

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
	Secret       string `json:"secret,omitempty" title:"Secret name" help:"The credential's name in the host's secret store, never the credential"`
	AllowPrivate bool   `json:"allowPrivate,omitempty" title:"Allow private address" help:"Also accept http and private networks, for on-premise systems"`
	// Marking classifies everything read through it (ADR-0075); datasets loaded from it are marked at least as high.
	Marking   string           `json:"marking,omitempty" choices:"internal,confidential,restricted" title:"Marking" help:"What is read through it is at least this: internal, confidential or restricted"`
	State     string           `json:"state" field:"readonly"`
	Requested bool             `json:"requested,omitempty" field:"readonly" title:"Check requested"`
	Last      *ConnectionCheck `json:"last,omitempty" field:"readonly" type:"json" title:"Last check"`
	// Definition publication is independent of data/runtime versions.
	DefinitionVersion int      `json:"definitionVersion,omitempty" field:"readonly" title:"Version"`
	Published         string   `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed"`
	Versions          []string `json:"definitionVersions,omitempty" field:"readonly" type:"json" title:"Published versions"`
}

// ConnectionCheck is what one check found.
type ConnectionCheck struct {
	At    time.Time `json:"at"`
	OK    bool      `json:"ok"`
	Error string    `json:"error,omitempty"`
	// Detail is what answered: a status, a server version, an OData version.
	Detail string `json:"detail,omitempty"`
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
