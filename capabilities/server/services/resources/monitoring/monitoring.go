package monitoring

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

const (
	ID               = "monitoring"
	ViewType         = platform.MonitoringViewType
	SchemaViewSave   = "monitoring.view.save"
	SchemaViewRemove = "monitoring.view.remove"
	ReadViews        = "monitoring-views"
)

type SavedView = platform.MonitoringView

type Monitoring struct {
	ledger *platform.Ledger
}

func New(tenant string) *Monitoring {
	roles := []string{platform.AnyMember}
	actions := []platform.Action{
		{Schema: SchemaViewSave, Target: ViewType, Capability: "views", Title: "Save monitoring view", Description: "Save a named run type and status filter, or change your own.", Roles: roles,
			Payload: []platform.Field{
				{Name: "title", Type: "string", Required: true, Description: "Its name"},
				{Name: "family", Type: "string", Description: "runs or logs", Choices: []string{"runs", "logs"}},
				{Name: "query", Type: "string", Description: "The page filters as a URL query string"},
				{Name: "time", Type: "string", Description: "15m, 1h, 24h, 7d, 30d or custom", Choices: []string{"15m", "1h", "24h", "7d", "30d", "custom"}},
				{Name: "from", Type: "string", Description: "RFC 3339 start for a custom window"},
				{Name: "to", Type: "string", Description: "RFC 3339 end for a custom window"},
				{Name: "columns", Type: "string", Description: "Comma-separated column keys"},
				{Name: "density", Type: "string", Description: "compact or comfortable", Choices: []string{"compact", "comfortable"}},
				{Name: "type", Type: "string", Description: "Legacy run type filter", Choices: []string{"sync", "build", "automation", "function", "delivery"}},
				{Name: "status", Type: "string", Description: "Legacy run status filter", Choices: []string{"ok", "failed", "pending", "running", "warning", "paused", "cancelled", "unknown"}},
			}},
		{Schema: SchemaViewRemove, Target: ViewType, Capability: "views", Title: "Remove monitoring view", Description: "Remove one of your saved monitoring filters.", Roles: roles, Payload: []platform.Field{}},
	}
	return &Monitoring{ledger: platform.NewLedger(tenant, ID, platform.NewCatalog(actions...), ViewType)}
}

func (m *Monitoring) Manifest() platform.Manifest {
	return platform.Manifest{ID: ID, Title: platform.BuiltinManifestTitle(ID), Version: "1", Actions: m.ledger.Catalog,
		Entities: []platform.Entity{{Type: ViewType, Title: "Monitoring view", Model: SavedView{}, Display: "title",
			Scope: platform.Scope{Default: platform.ScopeOwn, Owner: "owner", Participants: func(value any) []string { return []string{value.(SavedView).Owner} }}}},
		Reads: []string{ReadViews}, Everyone: []string{ReadViews}}
}

func (m *Monitoring) AcceptedLedger() *platform.Ledger { return m.ledger }

func (m *Monitoring) ForkRead(ledger *platform.Ledger) (platform.App, error) {
	return &Monitoring{ledger: ledger}, nil
}
func (m *Monitoring) Declarations() []*pb.AuthorityDeclaration { return m.ledger.Declarations() }
func (m *Monitoring) SnapshotFormat() string                   { return "platform.ledger.v1" }
func (m *Monitoring) Snapshot() (json.RawMessage, error)       { return m.ledger.Snapshot() }
func (m *Monitoring) Restore(raw json.RawMessage) error        { return m.ledger.Restore(raw) }

func (m *Monitoring) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return m.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		name := s.GetSchema().GetName()
		if name != SchemaViewSave && name != SchemaViewRemove {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
		}
		id := s.GetTarget().GetId()
		view, exists := platform.Get[SavedView](c, id)
		if exists && view.Owner != c.ID {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
		}
		if name == SchemaViewRemove {
			if !exists || view.Archived {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
			}
			view.Archived = true
		} else {
			var payload struct {
				Title, Family, Query, Time, From, To, Columns, Density, Type, Status string
			}
			if json.Unmarshal(s.GetPayload(), &payload) != nil || strings.TrimSpace(payload.Title) == "" {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
			}
			if payload.Family == "" {
				payload.Family = "runs"
			}
			if payload.Family != "runs" && payload.Family != "logs" {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose a runs or logs view")
			}
			if len(payload.Query) > 4096 || len(payload.Columns) > 1024 {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The view query or columns are too long")
			}
			if payload.Time == "" {
				payload.Time = "1h"
			}
			if !slices.Contains([]string{"15m", "1h", "24h", "7d", "30d", "custom"}, payload.Time) {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose a valid view time range")
			}
			if payload.Time == "custom" {
				from, fromErr := time.Parse(time.RFC3339, payload.From)
				to, toErr := time.Parse(time.RFC3339, payload.To)
				if fromErr != nil || toErr != nil || !from.Before(to) || to.Sub(from) > 30*24*time.Hour {
					return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A custom view needs a start and end within 30 days")
				}
				view.From, view.To = from.UTC(), to.UTC()
			}
			if payload.Density == "" {
				payload.Density = "comfortable"
			}
			if payload.Density != "compact" && payload.Density != "comfortable" {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose a valid view density")
			}
			view.ID, view.Title, view.Family, view.Query, view.Time, view.Columns, view.Density = id, payload.Title, payload.Family, payload.Query, payload.Time, payload.Columns, payload.Density
			view.Type, view.Status, view.Owner, view.Archived = payload.Type, payload.Status, c.ID, false
			if err := c.Check(view); err != nil {
				return nil, err
			}
		}
		return func(r *pb.ChangeRecord) { c.Put(r, view) }, nil
	})
}

func (m *Monitoring) Read(c platform.Caller, name string) (any, *kernel.Error) {
	if name != ReadViews {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	filter := platform.Expr{Op: "=", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("text", "owner"), platform.ExprLiteralValue("text", c.ID)}}
	views, _, err := platform.Find[SavedView](c, platform.Query{Filter: &filter, Sort: []string{"title"}})
	for index := range views {
		if views[index].Family == "" {
			views[index].Family = "runs"
		}
		if views[index].Query == "" && (views[index].Type != "" || views[index].Status != "") {
			query := url.Values{}
			if views[index].Type != "" {
				query.Set("type", views[index].Type)
			}
			if views[index].Status != "" {
				query.Set("status", views[index].Status)
			}
			views[index].Query = query.Encode()
		}
		if views[index].Time == "" {
			views[index].Time = "1h"
		}
		if views[index].Density == "" {
			views[index].Density = "comfortable"
		}
	}
	return views, err
}
