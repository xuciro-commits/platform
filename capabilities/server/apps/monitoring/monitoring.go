package monitoring

import (
	"encoding/json"
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
			Payload: []platform.Field{{Name: "title", Type: "string", Required: true, Description: "Its name"}, {Name: "type", Type: "string", Description: "Run type; empty selects all", Choices: []string{"sync", "build", "automation", "function", "delivery"}}, {Name: "status", Type: "string", Description: "Run status; empty selects all", Choices: []string{"ok", "failed", "pending", "running", "warning", "paused", "cancelled", "unknown"}}}},
		{Schema: SchemaViewRemove, Target: ViewType, Capability: "views", Title: "Remove monitoring view", Description: "Remove one of your saved monitoring filters.", Roles: roles, Payload: []platform.Field{}},
	}
	return &Monitoring{ledger: platform.NewLedger(tenant, ID, platform.NewCatalog(actions...), ViewType)}
}

func (m *Monitoring) Manifest() platform.Manifest {
	return platform.Manifest{ID: ID, Title: "Monitoring", Version: "1", Actions: m.ledger.Catalog,
		Entities: []platform.Entity{{Type: ViewType, Title: "Monitoring view", Model: SavedView{}, Display: "title",
			Scope: platform.Scope{Default: platform.ScopeOwn, Owner: "owner", Participants: func(value any) []string { return []string{value.(SavedView).Owner} }}}},
		Reads: []string{ReadViews}, Everyone: []string{ReadViews}}
}

func (m *Monitoring) AcceptedLedger() *platform.Ledger         { return m.ledger }
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
			var payload struct{ Title, Type, Status string }
			if json.Unmarshal(s.GetPayload(), &payload) != nil || strings.TrimSpace(payload.Title) == "" {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
			}
			view.ID, view.Title, view.Type, view.Status, view.Owner, view.Archived = id, payload.Title, payload.Type, payload.Status, c.ID, false
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
	return views, err
}
