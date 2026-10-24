package platformserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/internal/construction"
	"platformserver/platform"
)

// ConnectionKind is a projection of built-in transports and the original Package owner.
type ConnectionKind struct {
	ID                 string                  `json:"id"`
	Title              string                  `json:"title"`
	Kind               string                  `json:"kind"`
	AddressPlaceholder string                  `json:"addressPlaceholder,omitempty"`
	Package            string                  `json:"package,omitempty"`
	Version            string                  `json:"version,omitempty"`
	Digest             string                  `json:"digest,omitempty"`
	Profile            *platform.DriverProfile `json:"profile,omitempty"`
}
type ConnectionKindPage struct {
	Items []ConnectionKind `json:"items"`
}

func (h hostView) CheckDriverBinding(binding platform.DriverBinding) error {
	return h.t.checkDriverBinding(binding)
}
func (t *Tenant) checkDriverBinding(binding platform.DriverBinding) error {
	if err := binding.Check(); err != nil {
		return err
	}
	owner, ok := t.app(PlatformApp).(ProtocolPackageCatalog)
	if !ok {
		return fmt.Errorf("protocol packages are unavailable on this host")
	}
	permitted := false
	for _, pkg := range owner.ProtocolPackages() {
		if pkg.ID == binding.Package && pkg.Version == binding.Version && strings.TrimPrefix(pkg.Digest, "sha256:") == strings.TrimPrefix(binding.Digest, "sha256:") && (pkg.State == "active" || pkg.State == "retained") {
			permitted = true
			break
		}
	}
	if !permitted {
		return fmt.Errorf("the protocol package is not active for new connections")
	}
	runtime, err := owner.ResolveDriver(binding.Package, binding.Version, binding.Digest, binding.Profile)
	if err != nil {
		return err
	}
	for _, profile := range runtime.Profiles {
		if profile.ID == binding.Profile && len(binding.Configuration.Commands) > 0 && !slices.Contains(profile.Operations, "execute") {
			return fmt.Errorf("this protocol profile is read-only")
		}
	}
	return nil
}

func (t *Tenant) connectionKinds() ConnectionKindPage {
	out := ConnectionKindPage{Items: []ConnectionKind{
		{ID: "http", Kind: "http", Title: "HTTP / REST / files", AddressPlaceholder: "https://erp.example.com/api/"},
		{ID: "odata", Kind: "odata", Title: "OData (SAP Gateway)", AddressPlaceholder: "https://sap.example.com/sap/opu/odata/sap/API_PRODUCT_SRV/"},
		{ID: "postgres", Kind: "postgres", Title: "PostgreSQL (read-only)", AddressPlaceholder: "postgres://reader@mes-db.example.com:5432/mes?sslmode=require"},
	}}
	if owner, ok := t.app(PlatformApp).(ProtocolPackageCatalog); ok {
		for _, pkg := range owner.ProtocolPackages() {
			if pkg.State != "active" && pkg.State != "retained" {
				continue
			}
			for _, profile := range pkg.Runtime.Profiles {
				out.Items = append(out.Items, ConnectionKind{ID: pkg.ID + "@" + pkg.Version + "/" + profile.ID + "/" + pkg.Digest, Kind: "http", Title: profile.Title, Package: pkg.ID, Version: pkg.Version, Digest: pkg.Digest, Profile: &profile, AddressPlaceholder: "http://127.0.0.1:8510"})
			}
		}
	}
	return out
}

func (h *Host) routesProtocolConnections(rt *routes) {
	roles := []string{construction.Builder, construction.Integrator}
	rt.authorized(Route{Pattern: "GET /v1/connection-kinds", Summary: "Built-in transports and installed versioned protocol profiles", Answer: ConnectionKindPage{}}, construction.ID, roles, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, t.i18n.Translate(t.connectionKinds(), t.Language(m, r)))
	})
	rt.authorized(Route{Pattern: "POST /v1/connections/preview", Summary: "Read-only preview of a protocol connection draft without changing its published binding", Body: construction.Connection{}, Answer: construction.ConnectionCheck{}}, construction.ID, roles, func(w http.ResponseWriter, r *http.Request, _ platform.Member, t *Tenant) {
		var connection construction.Connection
		if err := decodeBodyLimit(r, &connection, 128<<10); err != nil {
			Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Protocol connection preview needs a valid draft"))
			return
		}
		result := construction.ConnectionCheck{At: h.Now()}
		if connection.Protocol == nil {
			result.Error = "Choose an installed protocol package"
		} else {
			binding := *connection.Protocol
			binding.Scope = platform.DriverScope(t.ID)
			connection.Protocol = &binding
			connection.ID = "preview-" + connection.Protocol.Revision()[:24]
			result = t.checkProtocolConnection(r.Context(), connection, h.Now())
		}
		WriteJSON(w, http.StatusOK, result)
	})
}

// protocolHTTP is the existing outbound policy, shared by checks and original integration reads.
func (t *Tenant) protocolHTTP(ctx context.Context, connection construction.Connection, method, path string, body any, answer any) error {
	u, err := url.Parse(connection.Address)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Scheme != "https" && !(u.Scheme == "http" && connection.AllowPrivate) {
		return fmt.Errorf("the protocol gateway requires a service root without credentials; HTTP needs private access enabled")
	}
	var raw []byte
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	request, err := http.NewRequestWithContext(ctx, method, connection.Resolve(path), bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("the protocol gateway address is invalid")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Platform-Tenant", t.ID)
	if connection.Secret != "" {
		credential, found := t.credentialHeader(connection.Secret, "connection/"+connection.ID, connection.Resolve(path))
		if !found || strings.TrimSpace(string(credential)) == "" {
			return fmt.Errorf("the named protocol gateway secret is unavailable")
		}
		request.Header.Set("Authorization", strings.TrimSpace(string(credential)))
	}
	send := t.Outbound
	if send == nil {
		send = guarded
	}
	response, err := send(request, connection.AllowPrivate)
	if err != nil {
		return fmt.Errorf("the protocol gateway did not answer: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil {
		return fmt.Errorf("the protocol gateway answer could not be read: %w", err)
	}
	if len(data) > 1<<20 {
		return fmt.Errorf("the protocol gateway answer exceeds its read budget")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("protocol gateway answered %d: %s", response.StatusCode, clipDetail(data))
	}
	if answer != nil && json.Unmarshal(data, answer) != nil {
		return fmt.Errorf("the protocol gateway returned an invalid answer")
	}
	return nil
}

func (t *Tenant) configureProtocol(ctx context.Context, connection construction.Connection, readOnly bool) error {
	if connection.Protocol == nil {
		return nil
	}
	if readOnly {
		owner, ok := t.app(PlatformApp).(ProtocolPackageCatalog)
		if !ok {
			return fmt.Errorf("protocol packages are unavailable on this host")
		}
		if _, err := owner.ResolveDriver(connection.Protocol.Package, connection.Protocol.Version, connection.Protocol.Digest, connection.Protocol.Profile); err != nil {
			return err
		}
	} else if err := t.checkDriverBinding(*connection.Protocol); err != nil {
		return err
	}
	path := "configure"
	if readOnly {
		path += "?readOnly=1"
	}
	return t.protocolHTTP(ctx, connection, http.MethodPost, path, connection.Protocol, nil)
}

func (t *Tenant) checkProtocolConnection(ctx context.Context, connection construction.Connection, at time.Time) construction.ConnectionCheck {
	result := construction.ConnectionCheck{At: at}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := t.configureProtocol(ctx, connection, strings.HasPrefix(connection.ID, "preview-")); err != nil {
		result.Error = err.Error()
		return result
	}
	var read platform.DriverReadResult
	if err := t.protocolHTTP(ctx, connection, http.MethodGet, "check", nil, &read); err != nil {
		result.Error = err.Error()
		return result
	}
	result.OK, result.Samples, result.BindingRevision = true, read.Samples, connection.Protocol.Revision()
	result.Detail = connection.Protocol.Package + " " + connection.Protocol.Version + " / " + connection.Protocol.Profile
	for _, sample := range read.Samples {
		if sample.Quality != "good" {
			result.OK = false
			result.Error = "Some configured points are unavailable; inspect their quality and reason"
			break
		}
	}
	return result
}
