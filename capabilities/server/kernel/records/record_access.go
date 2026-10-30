package records

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/rules"
	"platformserver/journal"
	"platformserver/platform"
	"reflect"
	"slices"
	"strings"
	"time"
)

type ScopeDirectory interface {
	Units(party, structure string, day platform.Date) []string
	Below(units []string, structure string, day platform.Date) []string
}

func FieldView(m platform.Member, source platform.EntityInfo) (view platform.EntityInfo, hidden []platform.FieldInfo) {
	roles := m.RolesIn(source.App)
	readable := func(f platform.FieldInfo) bool {
		if !f.ReadsAny(roles) || m.AIContext && f.AIInput != nil && !*f.AIInput {
			return false
		}
		if f.AliasOf != "" {
			target, found := source.Field(f.AliasOf)
			return found && target.ReadsAny(roles) && !(m.AIContext && target.AIInput != nil && !*target.AIInput)
		}
		return true
	}
	for _, f := range source.Fields {
		if !readable(f) {
			hidden = append(hidden, f)
		}
	}
	if len(hidden) == 0 {
		return source, nil
	}
	info := source
	info.Fields = slices.DeleteFunc(slices.Clone(info.Fields), func(f platform.FieldInfo) bool { return !readable(f) })
	info.LocalRules = maps.Clone(source.LocalRules)
	var visible func(platform.Expr) bool
	visible = func(expr platform.Expr) bool {
		if expr.Ref != "" {
			name, _, _ := strings.Cut(expr.Ref, ".")
			if name != "caller" && name != "input" && name != "item" && name != "id" {
				field, ok := source.Field(name)
				if !ok || !readable(field) {
					return false
				}
			}
		}
		for _, arg := range expr.Args {
			if !visible(arg) {
				return false
			}
		}
		return expr.In == nil || visible(*expr.In)
	}
	for name, expr := range info.LocalRules {
		field, ok := source.Field(name)
		if !ok || !readable(field) || !visible(expr) {
			delete(info.LocalRules, name)
		}
	}
	return info, hidden
}

func ScopePolicy(info platform.EntityInfo, m platform.Member, directory ScopeDirectory, now time.Time, through func(reflect.Value) bool) (*RowFilter, *kernel.Error) {
	roles, scope := m.RolesIn(info.App), info.Scope
	text := rules.Type{Kind: "text"}
	deny := func() (*RowFilter, *kernel.Error) {
		filter, err := CompilePolicy(info, boolLit(false))
		if err != nil {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
		}
		return filter, nil
	}
	compiled := func(expr rules.Expr, attributes ...RowAttribute) (*RowFilter, *kernel.Error) {
		filter, err := CompilePolicy(info, expr, attributes...)
		if err != nil {
			return deny()
		}
		return filter, nil
	}
	if scope.Through != nil {
		return compiled(rules.RefOf("boolean", "$through"), RowAttribute{Ref: "$through", Type: rules.Type{Kind: "boolean"}, Read: func(v reflect.Value) rules.Value { return rules.BoolValue(through(v)) }})
	}
	var participant []RowAttribute
	var participantExpr rules.Expr
	if scope.Participants != nil {
		participant = []RowAttribute{{Ref: "$participants", Type: rules.Type{Kind: "set", Element: &text}, Read: func(v reflect.Value) rules.Value {
			return rules.SetValue(scope.Participants(v.Interface()))
		}}}
		participantExpr = rules.Expr{Op: "in", Kind: "boolean", Args: []rules.Expr{textLit(m.ID), setRef("$participants")}}
	}
	if len(roles) == 0 {
		if scope.Participants == nil {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
		}
		return compiled(participantExpr, participant...)
	}
	base, restricted := scopeExpr(directory, m, info, roles, now)
	if !restricted {
		return nil, nil
	}
	attributes := append([]RowAttribute{createdBy}, participant...)
	if scope.Participants != nil {
		base = combine("or", base, participantExpr)
	}
	return compiled(base, attributes...)
}

var createdBy = RowAttribute{Ref: "$created.by", Type: rules.Type{Kind: "text"}, Read: func(v reflect.Value) rules.Value {
	return rules.TextValue("text", Header(v).Created.By)
}}

func scopeExpr(directory ScopeDirectory, m platform.Member, info platform.EntityInfo, roles []string, now time.Time) (rules.Expr, bool) {
	if len(roles) > 1 {
		var parts []rules.Expr
		for _, role := range roles {
			part, restricted := scopeExpr(directory, m, info, []string{role}, now)
			if !restricted {
				return rules.Expr{}, false
			}
			parts = append(parts, part)
		}
		return combine("or", parts...), true
	}
	scope := info.Scope
	level := scope.LevelFor(roles)
	textField := func(name string) (rules.Expr, bool) {
		f, ok := info.Field(name)
		if !ok || !slices.Contains([]string{"text", "longtext", "choice", "reference"}, f.Type) || f.AliasOf != "" || len(f.Index) == 0 {
			return rules.Expr{}, false
		}
		return rules.RefOf("text", name+"#text"), true
	}
	switch level {
	case platform.ScopeNone:
		return boolLit(false), true
	case platform.ScopeOwn:
		if scope.Owner == platform.OwnerCreated {
			return compare("=", rules.RefOf("text", createdBy.Ref), textLit(m.ID)), true
		}
		owner, ok := textField(scope.Owner)
		if !ok {
			return boolLit(false), true
		}
		return compare("=", owner, textLit(m.ID)), true
	case platform.ScopeUnit, platform.ScopeBelow:
		var units []string
		if directory != nil {
			structure := scope.Structure
			if level == platform.ScopeUnit {
				structure = ""
			}
			held, unbound := false, false
			for _, g := range m.Grants {
				if g.App != info.App || !slices.Contains(roles, g.Role) {
					continue
				}
				held = true
				if g.Unit == "" {
					unbound = true
					continue
				}
				if level == platform.ScopeBelow && g.Structure != "" && g.Structure != scope.Structure {
					continue
				}
				units = append(units, directory.Below([]string{g.Unit}, structure, m.Today(now))...)
			}
			if !held || unbound {
				units = append(units, directory.Units("member:"+m.ID, structure, m.Today(now))...)
			}
		}
		unit, ok := textField(scope.Unit)
		if !ok || len(units) == 0 {
			return boolLit(false), true
		}
		items := make([]any, len(units))
		for i, u := range units {
			items[i] = u
		}
		var options []rules.Expr
		for chunk := range slices.Chunk(items, rules.MaxSet) {
			options = append(options, rules.Expr{Op: "in", Kind: "boolean", Args: []rules.Expr{unit, setLit("text", chunk)}})
		}
		return combine("or", options...), true
	}
	return rules.Expr{}, false
}

func Write(info platform.EntityInfo, receipt *pb.ChangeRecord, prev *Row, v reflect.Value) (*Row, *kernel.Error) {
	rec := Header(v)
	stamp := platform.Stamp{By: receipt.GetSubmission().GetPrincipalId(), At: receipt.GetRecordedTime().AsTime(), Change: receipt.GetChangeId()}
	change := RecordChange{Change: receipt.GetChangeId(), Schema: receipt.GetSubmission().GetSchema().GetName(), By: stamp.By, At: stamp.At, Fields: []FieldChange{}}
	rec.Changed, rec.Created, rec.Revision = stamp, stamp, 0
	target := receipt.GetSubmission().GetTarget()
	named := target.GetType() == info.Type && target.GetId() == rec.ID
	if l := info.Lifecycle; prev == nil && l != nil {
		if f, _ := info.Field(l.Field); v.FieldByIndex(f.Index).String() == "" {
			v.FieldByIndex(f.Index).SetString(l.Initial)
		}
	}
	if owner := info.Scope.Owner; prev == nil && owner != "" && owner != platform.OwnerCreated {
		if f, _ := info.Field(owner); v.FieldByIndex(f.Index).String() == "" {
			v.FieldByIndex(f.Index).SetString(stamp.By)
		}
	}
	if prev != nil {
		old := Header(prev.Value)
		rec.Created, rec.Revision = old.Created, old.Revision
		if old.Archived != rec.Archived {
			change.Fields = append(change.Fields, FieldChange{Field: "archived", Before: rowJSON(old.Archived), After: rowJSON(rec.Archived)})
		}
	}
	for _, f := range info.Fields {
		after := rowJSON(v.FieldByIndex(f.Index).Interface())
		var before json.RawMessage
		if prev != nil {
			before = rowJSON(prev.Value.FieldByIndex(f.Index).Interface())
		}
		if string(before) != string(after) && !(prev == nil && v.FieldByIndex(f.Index).IsZero()) {
			change.Fields = append(change.Fields, FieldChange{Field: f.Name, Before: before, After: after})
		}
	}
	if named {
		rec.Revision = receipt.GetRevision()
	}
	var projection *Projection
	var extraction *Extraction
	if prev != nil {
		extraction = prev.Extraction
		projection = CloneProjection(prev.Projection)
		if err := RefreshProjection(projection, v.Interface()); err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
		}
	}
	next := &Row{Value: v, Format: info.RecordFormat, Projection: projection, Extraction: extraction}
	if err := AppendRowHistory(next, prev, []RecordChange{change}); err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	return next, nil
}

type CurrentScope struct {
	backend         journal.RuntimeState
	view            journal.StateReadView
	tenant, release string
	applied         int64
	position        func() int64
	info            func(string) platform.EntityInfo
}

func NewCurrentScope(info func(string) platform.EntityInfo) *CurrentScope {
	return &CurrentScope{info: info}
}
func (s *CurrentScope) Fork(info func(string) platform.EntityInfo) *CurrentScope {
	copy := *s
	copy.info = info
	return &copy
}
func (s *CurrentScope) Configure(backend journal.RuntimeState, tenant string, applied int64, position func() int64) {
	s.backend, s.tenant, s.applied, s.position = backend, tenant, applied, position
}
func (s *CurrentScope) Backend() journal.RuntimeState           { return s.backend }
func (s *CurrentScope) UseBackend(backend journal.RuntimeState) { s.backend = backend }
func (s *CurrentScope) View() journal.StateReadView             { return s.view }
func (s *CurrentScope) Bind(view journal.StateReadView)         { s.view = view }
func (s *CurrentScope) Tenant() string                          { return s.tenant }
func (s *CurrentScope) Applied() int64                          { return s.applied }
func (s *CurrentScope) MarkApplied(position int64)              { s.applied = position }
func (s *CurrentScope) Position() func() int64                  { return s.position }
func (s *CurrentScope) Release() string                         { return s.release }
func (s *CurrentScope) SelectRelease(release string)            { s.release = release }
func (s *CurrentScope) Source(typ string, info *platform.EntityInfo) RowSource {
	return &currentRows{store: s, typ: typ, info: info}
}

type currentRows struct {
	store *CurrentScope
	typ   string
	info  *platform.EntityInfo
}

func (r *currentRows) view() journal.StateReadView {
	if r.store.view == nil {
		panic("SQL record read has no consistent view")
	}
	return r.store.view
}

func (r *currentRows) Get(id string) *Row {
	view := r.view()
	current, err := view.StateCurrent(context.Background(), r.store.tenant, r.typ, id)
	if err != nil {
		panic(fmt.Errorf("record %s/%s: %w", r.typ, id, err))
	}
	if len(current.Image) == 0 {
		return nil
	}
	var image Image
	if err := json.Unmarshal(current.Image, &image); err != nil {
		panic(err)
	}
	info := r.store.info(r.typ)
	if r.info != nil {
		info = *r.info
	}
	decoded, err := DecodeFormattedImage(info, image.Value, image.Projection, r.store.release, image.Format)
	if err != nil {
		panic(err)
	}
	value := reflect.New(info.Go).Elem()
	if err := json.Unmarshal(decoded, value.Addr().Interface()); err != nil {
		panic(err)
	}
	EmptyLists(value)
	if Header(value).ID != id {
		panic("SQL current record identity differs")
	}
	row := &Row{Value: value, Format: image.Format, HistoryCount: current.HistoryCount, HistoryHead: current.HistoryHead, Projection: image.Projection, Extraction: image.Extraction}
	row.RetainOriginal(image.Value)
	count := current.HistoryCount
	row.LoadHistory = func(after int) []RecordChange {
		list, err := view.StateHistory(context.Background(), r.store.tenant, r.typ, id, after, count)
		if err != nil {
			panic(fmt.Errorf("record history %s/%s: %w", r.typ, id, err))
		}
		out := make([]RecordChange, len(list))
		for i, raw := range list {
			if err := json.Unmarshal(raw, &out[i]); err != nil {
				panic(err)
			}
		}
		return out
	}
	return row
}

func (r *currentRows) IDs(after string, limit int) []string {
	ids, err := r.view().StateCandidates(context.Background(), r.store.tenant, r.typ, nil, after, limit, r.store.applied)
	if err != nil {
		panic(fmt.Errorf("record candidates: %w", err))
	}
	return ids
}

func (s *CurrentScope) Open(ctx context.Context) (journal.StateReadView, error) {
	views, ok := s.backend.(journal.StateReadViews)
	if !ok {
		return nil, nil
	}
	view, err := views.BeginStateRead(ctx, s.tenant, s.applied)
	if err != nil {
		return nil, err
	}
	s.view = view
	return view, nil
}
func (s *CurrentScope) Detach() journal.StateReadView { view := s.view; s.view = nil; return view }
func (s *CurrentScope) Reader() interface {
	StateCandidates(context.Context, string, string, []journal.StateFilter, string, int, int64) ([]string, error)
} {
	if s.view != nil {
		return s.view
	}
	return s.backend
}

func VisitPlan(image IndexImage, lookup ImageLookup, plan Plan, visible func(reflect.Value) bool, writes iter.Seq[string], accept func(reflect.Value)) int {
	current := image.Current()
	matched := 0
	visit := func(r *Row) {
		if r == nil {
			return
		}
		v := r.Value
		if !plan.Match(v) {
			return
		}
		matched++
		if visible != nil && !visible(v) {
			return
		}
		accept(v)
	}
	if ids, indexed := plan.candidates(image, lookup); indexed {
		for _, id := range ids {
			visit(image.Rows.Get(id))
		}
	} else if current.Backend() != nil {
		var filters []journal.StateFilter
		if !image.RetainedProjection && len(image.Info.RecordImages) == 0 {
			filters = plan.Pushdown
		}
		if image.RetainedProjection {
			for _, filter := range plan.Pushdown {
				if len(filter.Path) == 2 && filter.Path[0] == "value" && filter.Path[1] == "id" {
					filters = append(filters, filter)
				}
			}
		}
		pending := map[string]bool{}
		for id := range image.Rows.Overlay().All() {
			pending[id] = true
		}
		for id := range writes {
			pending[id] = true
		}

		after := ""
		for {
			ids, readErr := current.Reader().StateCandidates(context.Background(), current.Tenant(), image.Info.Type, filters, after, 256, current.Applied())
			if readErr != nil {
				panic(fmt.Errorf("record candidates: %w", readErr))
			}
			for _, id := range ids {
				delete(pending, id)
				visit(image.Rows.Get(id))
			}
			if len(ids) < 256 {
				break
			}
			after = ids[len(ids)-1]
		}
		for id := range pending {
			visit(image.Rows.Get(id))
		}
	} else {
		for _, r := range image.Rows.All() {
			visit(r)
		}
	}
	return matched
}
