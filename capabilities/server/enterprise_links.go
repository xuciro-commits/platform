package platformserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/construction"
	enterprisemodel "platformserver/internal/enterprise"
	"platformserver/internal/host"
	"platformserver/platform"
)

type EnterpriseLinkChange struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

// EnterpriseLinkPreview carries the exact content reviewed by the member.
// The original Enterprise import action confirms it without another fetch.
type EnterpriseLinkPreview struct {
	Link               string                 `json:"link"`
	Base               string                 `json:"base"`
	Content            string                 `json:"content"`
	ConnectionRevision uint32                 `json:"connectionRevision"`
	Slice              enterprisemodel.Slice  `json:"slice"`
	Changes            []EnterpriseLinkChange `json:"changes"`
}

type enterpriseLinkModel interface {
	Model() enterprisemodel.Model
	ValidateImportedModel(enterprisemodel.Model, string, platform.Date) error
}

func (t *Tenant) tenantLinkModel() (enterprisemodel.Model, *kernel.Error) {
	owner, ok := t.app(enterprisemodel.ID).(enterpriseLinkModel)
	if !ok {
		return enterprisemodel.Model{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The enterprise model is unavailable")
	}
	return owner.Model(), nil
}

// The caller keeps its original runtime, so a staged import reads the same
// authorized connection record as its other original owner decisions.
func (t *Tenant) tenantConnection(c platform.Caller, id string, now time.Time) (construction.Connection, *kernel.Error) {
	c = platform.RouteCaller(c, construction.ID)
	c.Automation = false
	permission := c.Authorize(platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "read", Type: construction.ConnectionType, Record: id, Fields: []string{"kind", "address", "secret", "allowPrivate", "state", "published", "marking"}}, now)
	if err := authorizationRefusal(permission); err != nil {
		return construction.Connection{}, err
	}
	if len(permission.Withheld) > 0 {
		return construction.Connection{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The connection settings are not readable")
	}
	raw, refusal := c.ReadRecord(construction.ConnectionType, id, now)
	if refusal != nil {
		return construction.Connection{}, refusal
	}
	var connection construction.Connection
	if json.Unmarshal(raw, &connection) != nil {
		return construction.Connection{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The connection settings are not readable")
	}
	owner, ok := t.app(construction.ID).(host.IntegrationDefinitions)
	if !ok {
		return construction.Connection{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The connection owner is unavailable")
	}
	fixed, err := owner.ExecutionConnection(connection)
	if err != nil || fixed.Archived || fixed.State != "ready" || fixed.Kind != "http" {
		return construction.Connection{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The tenant source needs a ready HTTP connection")
	}
	address, err := url.Parse(fixed.Address)
	if err != nil || address.Host == "" || address.User != nil || address.RawQuery != "" || address.Fragment != "" || address.Scheme != "https" && !(address.Scheme == "http" && fixed.AllowPrivate) {
		return construction.Connection{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The tenant source needs an HTTP service root")
	}
	if fixed.Secret == "" || t.authorization(fixed) == "" {
		return construction.Connection{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The tenant source needs an available connection credential")
	}
	return fixed, nil
}

func (t *Tenant) checkTenantConnection(c platform.Caller, id string, now time.Time) *kernel.Error {
	_, err := t.tenantConnection(c, id, now)
	return err
}

func (t *Tenant) checkTenantLink(c platform.Caller, id string, revision uint32, now time.Time) *kernel.Error {
	model, refusal := t.tenantLinkModel()
	if refusal != nil {
		return refusal
	}
	link := model.TenantLinkOf(id)
	if link == nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The tenant link is unavailable")
	}
	connection, refusal := t.tenantConnection(c, link.Connection, now)
	if refusal != nil {
		return refusal
	}
	if connection.Revision != revision {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The source connection changed; preview the import again")
	}
	return nil
}

func (t *Tenant) authorizeEnterpriseImport(c platform.Caller, now time.Time) *kernel.Error {
	const schema = "enterprise.slice.import"
	if !c.InScope(schema) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The source preview requires permission to import shared enterprise data")
	}
	return authorizationRefusal(c.Authorize(platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "action", Type: enterprisemodel.ModelType, Action: schema}, now))
}

// PreviewEnterpriseLink snapshots local declarations under the tenant lock,
// fetches outside it, then rejects a stale base before returning any preview.
func (t *Tenant) PreviewEnterpriseLink(ctx context.Context, m platform.Member, id string, now time.Time) (EnterpriseLinkPreview, *kernel.Error) {
	var answer EnterpriseLinkPreview
	t.mu.Lock()
	c := platform.NewCaller(runtime{t}, t.authorizationMember(m), enterprisemodel.ID, false, false)
	if err := t.authorizeEnterpriseImport(c, now); err != nil {
		t.mu.Unlock()
		return answer, err
	}
	model, refusal := t.tenantLinkModel()
	if refusal != nil {
		t.mu.Unlock()
		return answer, refusal
	}
	link := model.TenantLinkOf(id)
	if link == nil {
		t.mu.Unlock()
		return answer, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The tenant link is unavailable")
	}
	connection, refusal := t.tenantConnection(c, link.Connection, now)
	if refusal != nil {
		t.mu.Unlock()
		return answer, refusal
	}
	answer.Link, answer.Base, answer.ConnectionRevision = id, model.Fingerprint(), connection.Revision
	sourceTenant := link.Tenant
	t.mu.Unlock()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, connection.Resolve("v1/enterprise-published"), nil)
	if err != nil {
		return answer, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The tenant source needs an HTTP service root")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Platform-Tenant", sourceTenant)
	_, credential, _ := strings.Cut(t.authorization(connection), ":")
	request.Header.Set("Authorization", strings.TrimSpace(credential))
	send := t.Outbound
	if send == nil {
		send = guarded
	}
	response, err := send(request, connection.AllowPrivate)
	if err != nil {
		return answer, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The enterprise source did not answer")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, construction.SourceBody+1))
	if err != nil || response.StatusCode != http.StatusOK || len(body) > construction.SourceBody || json.Unmarshal(body, &answer.Slice) != nil {
		return answer, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The enterprise source did not return a bounded published slice")
	}
	answer.Content = answer.Slice.Fingerprint()

	t.mu.Lock()
	defer t.mu.Unlock()
	c = platform.NewCaller(runtime{t}, t.authorizationMember(m), enterprisemodel.ID, false, false)
	if refusal = t.authorizeEnterpriseImport(c, now); refusal != nil {
		return answer, refusal
	}
	if refusal = t.checkTenantLink(c, id, answer.ConnectionRevision, now); refusal != nil {
		return answer, refusal
	}
	current, refusal := t.tenantLinkModel()
	if refusal != nil {
		return answer, refusal
	}
	next, err := current.PreviewImport(answer.Slice, id, answer.Content, answer.Base, now.UTC().Format(time.DateOnly))
	if err != nil {
		return answer, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
	}
	if err := t.app(enterprisemodel.ID).(enterpriseLinkModel).ValidateImportedModel(next, answer.Slice.Tenant, now.UTC().Format(time.DateOnly)); err != nil {
		return answer, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	answer.Changes = enterpriseImportChanges(current, next, now.UTC().Format(time.DateOnly))
	return answer, nil
}

func enterpriseImportChanges(before, after enterprisemodel.Model, day string) []EnterpriseLinkChange {
	changes := []EnterpriseLinkChange{}
	for _, element := range after.Elements {
		previous := before.ElementOf(element.ID)
		kind := "changed"
		if previous == nil {
			kind = "added"
		} else if string(platform.Raw(*previous)) == string(platform.Raw(element)) {
			continue
		} else if previous.ActiveOn(day) && !element.ActiveOn(day) {
			kind = "closed"
		}
		changes = append(changes, EnterpriseLinkChange{Kind: kind, ID: element.ID, Name: element.Name})
	}
	for _, relationship := range after.Relationships {
		previous := before.RelationshipOf(relationship.ID)
		kind := "changed"
		if previous == nil {
			kind = "added"
		} else if string(platform.Raw(*previous)) == string(platform.Raw(relationship)) {
			continue
		} else if previous.ActiveOn(day) && !relationship.ActiveOn(day) {
			kind = "closed"
		}
		name := relationship.Relation
		if name == "" {
			name = relationship.Stereotype
		}
		changes = append(changes, EnterpriseLinkChange{Kind: kind, ID: relationship.ID, Name: name})
	}
	slices.SortFunc(changes, func(a, b EnterpriseLinkChange) int {
		if a.Kind != b.Kind {
			return strings.Compare(a.Kind, b.Kind)
		}
		return strings.Compare(a.ID, b.ID)
	})
	return changes
}
