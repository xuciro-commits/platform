package platformserver

import automation "platformserver/engine/automation"

import (
	"encoding/json"

	"platformserver/kernel/transport"

	"reflect"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	enterprisemodel "platformserver/engine/organization"
	"platformserver/platform"
	"platformserver/studio/build"
)

// The host API contract. The Go types are the source: each
// route is declared once with its handler, the body it takes and the answer
// it gives, and the host describes them as OpenAPI 3.1 at /v1/openapi.json.
// The TypeScript types of the web edge are generated from the same document
// (cmd/api-types), never written by hand. Per tenant, the document also
// describes each entity type the member may read and each action's payload.

// namedReads document the platform apps' reads, all served by GET /v1/{read}:
// the contract names each with the Go type it answers with.
var namedReads = []transport.Route{
	{Pattern: "GET /v1/contributions", Summary: "This tenant's namespaced contributions: every app's views and installed packages' views", Answer: []platform.Contribution{}},
	{Pattern: "GET /v1/packages", Summary: "Administrators read the package index; application builders discover installed blueprints", Answer: []platform.PackageView{}},
	{Pattern: "GET /v1/notifications", Summary: "The caller's notifications, newest first, in their language", Answer: []platform.Notification{}},
	{Pattern: "GET /v1/members", Summary: "The tenant's members with their roles and profiles (administrators)", Answer: []platform.MemberView{}},
	{Pattern: "GET /v1/permissions", Summary: "The permission catalog: every app's roles and what each may do, derived from the manifests (administrators, auditors)", Answer: []AppPermissions{}},
	{Pattern: "GET /v1/projects", Summary: "The tenant's projects: the members and assets each covers; editors of a live project build within it (administrators, auditors; #141)", Answer: []platform.BuildProject{}},
	{Pattern: "GET /v1/access", Summary: "The tenant's own access configuration: custom roles, policies and teams (administrators, auditors)", Answer: platform.AccessConfig{}},
	{Pattern: "GET /v1/tenant", Summary: "The tenant's record: its settings, apps and standing (every member)", Answer: platform.TenantRecord{}},
	{Pattern: "GET /v1/account", Summary: "The caller's profile over the tenant's defaults", Answer: platform.Account{}},
	{Pattern: "GET /v1/audit", Summary: "Accepted inputs, newest first, rebuilt from the journal (administrators)", Answer: []platform.AuditEntry{}},
	{Pattern: "GET /v1/deliveries", Summary: "Delivery attempts of events to subscribers (administrators)", Answer: []platform.Delivery{}},
	{Pattern: "GET /v1/work", Summary: "Owned work: deliveries and jobs (administrators)", Answer: []automation.Task{}},
	{Pattern: "GET /v1/connectors", Summary: "Connectors with their health and cursor (administrators)", Answer: []ConnectorView{}},
	{Pattern: "GET /v1/settings", Summary: "Every app's settings with their values, in the caller's language (administrators)", Answer: []AppSettings{}},
	{Pattern: "GET /v1/endpoints", Summary: "Outbound endpoints (administrators)", Answer: []EndpointView{}},
	{Pattern: "GET /v1/effects", Summary: "Outbound effects and their state (administrators)", Answer: []platform.Effect{}},
	{Pattern: "GET /v1/personal-reads", Summary: "Who read which records' personal data, newest first (administrators)", Answer: []platform.PersonalRead{}},
	{Pattern: "GET /v1/health", Summary: "How the tenant's work stands: queues, failed work, deferral, breakers, failing connectors and endpoints (administrators)", Answer: TenantHealth{}},
	{Pattern: "GET /v1/inbox", Summary: "Tasks offered to the caller, overdue first, in their language", Answer: []InboxTask{}},
	{Pattern: "GET /v1/requests", Summary: "The caller's approval requests", Answer: []platform.ApprovalRequest{}},
	{Pattern: "GET /v1/views", Summary: "The caller's saved views", Answer: []platform.SavedView{}},
	{Pattern: "GET /v1/agents", Summary: "The agents the apps declare", Answer: []platform.AgentInfo{}},
	{Pattern: "GET /v1/agent-overview", Summary: "Every agent, declared and outside, with what it did, what it cost, what people made of it and whether it is suspended", Answer: []platform.AgentOverview{}},
	{Pattern: "GET /v1/ai-limits", Summary: "Members', agents' and apps' own AI limits", Answer: []platform.AILimit{}},
	{Pattern: "GET /v1/runs", Summary: "Agent runs on the caller's behalf", Answer: []platform.AgentRunRecord{}},
	{Pattern: "GET /v1/memories", Summary: "What agents remember about the caller", Answer: []platform.Memory{}},
	{Pattern: "GET /v1/credentials", Summary: "Credential metadata for administrators; never values", Answer: platform.CredentialCatalog{}},
	{Pattern: "GET /v1/ai-providers", Summary: "AI providers (AI administrators)", Answer: []platform.AIProvider{}},
	{Pattern: "GET /v1/ai-models", Summary: "Models the caller may call, or all for AI administrators", Answer: []platform.AIModel{}},
	{Pattern: "GET /v1/ai-training", Summary: "Training policies and qualified candidate evidence", Answer: platform.AITrainingReport{}},
	{Pattern: "GET /v1/ai-usage", Summary: "Model calls and their totals", Answer: platform.AIUsageReport{}},
	{Pattern: "GET /v1/organization", Summary: "The organisation's structures, units, edges and memberships (a projection of the enterprise model)", Answer: platform.OrgSeed{}},
	{Pattern: "GET /v1/enterprise", Summary: "The tenant's enterprise model: kinds, elements, relationships and views", Answer: enterprisemodel.Model{}},
	{Pattern: "GET /v1/enterprise-metamodel", Summary: "The fixed organisation directory types and relationship endpoints", Answer: enterprisemodel.Metamodel{}},
	{Pattern: "GET /v1/flows", Summary: "The flows the apps declare, in the caller's language", Answer: []platform.FlowDefinition{}},
	{Pattern: "GET /v1/links", Summary: "Links between entities", Answer: []platform.RelationLink{}},
	{Pattern: "GET /v1/timeline", Summary: "Notes on entities' timelines", Answer: []platform.RelationNote{}},
}

// InboxTask is a task as the inbox serves it: with its answers in the
// reader's language, beside the values submitted.
type InboxTask struct {
	platform.WorkTask
	AnswerTitles []string `json:"answerTitles,omitempty"`
}

// SubmissionAnswer is what a decision's route answers: the accepted change
// record, or an error with the code the kernel's errors name (contract/spec/errors.md).
type SubmissionAnswer struct {
	Record *pb.ChangeRecord    `json:"record,omitempty"`
	Error  *platform.ErrorBody `json:"error,omitempty"`
}

// ProtocolCall is the body of a call to a protocol's action.
type ProtocolCall struct {
	Target         string          `json:"target"`
	IdempotencyKey string          `json:"idempotencyKey"`
	Payload        json.RawMessage `json:"payload"`
}

// MeView is who the caller is on this host, and what they may open.
type MeView struct {
	TenantID    string                `json:"tenantId"`
	PrincipalID string                `json:"principalId"`
	Profile     platform.Member       `json:"profile"`
	Apps        []AppEntry            `json:"apps"`
	Tenants     []string              `json:"tenants"`
	Language    string                `json:"language"`            // the language the host serves this member in; "" is English
	Languages   []string              `json:"languages"`           // the languages the tenant has dictionaries for
	Preferred   string                `json:"preferred,omitempty"` // the member's own choice
	Currency    string                `json:"currency"`            // the tenant's, the default of amounts people enter
	Account     platform.Account      `json:"account"`             // the member's profile over the tenant's defaults
	Tenant      platform.TenantRecord `json:"tenant"`              // the tenant's record
}

// SignIn tells the workspace how to sign in: an OpenID issuer and client, or
// development identities.
type SignIn struct {
	Issuer     string                   `json:"issuer,omitempty"`
	Client     string                   `json:"client,omitempty"`
	Identities []platform.LoginIdentity `json:"identities,omitempty"`
}

// OpenAPI captures already-authorized declarations, then describes their HTTP contract.
func (h *Host) OpenAPI(t *Tenant, m *platform.Member) map[string]any {
	var entities []platform.EntityInfo
	var actions []platform.Action
	if t != nil && m != nil {
		entities, actions = t.Entities(*m), t.Catalog(*m)
	}
	return transport.OpenAPI(h.routes, []reflect.Type{
		reflect.TypeFor[platform.PageCollectionInput](), reflect.TypeFor[platform.InputOptions](),
		reflect.TypeFor[platform.ImageRegionsRequest](), reflect.TypeFor[build.Table](),
		reflect.TypeFor[build.Source](), reflect.TypeFor[build.Connection](),
		reflect.TypeFor[platform.BusinessTxn](), reflect.TypeFor[platform.ConfirmationRun](),
		reflect.TypeFor[platform.DispatchRun](),
	}, reflect.TypeFor[SubmissionAnswer](), entities, actions)
}
