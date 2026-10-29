package records

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
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

// FieldView is a type as m may see it: without the fields m's role
// in the app may not read, so search, filters, sort, grouping and forms never
// reach them; the rows are shared. hidden are those fields.
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

func ScopeVisibility(info platform.EntityInfo, m platform.Member, directory ScopeDirectory, now time.Time) (func(reflect.Value) bool, *kernel.Error) {
	roles, scope := m.RolesIn(info.App), info.Scope
	participant := func(v reflect.Value) bool {
		return scope.Participants != nil && slices.Contains(scope.Participants(v.Interface()), m.ID)
	}
	if len(roles) == 0 {
		if scope.Participants == nil {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
		}
		return participant, nil
	}
	base, err := scoped(directory, m, info, roles, now)
	if base == nil || err != nil || scope.Participants == nil {
		return base, err
	}
	return func(v reflect.Value) bool { return base(v) || participant(v) }, nil
}

// scoped is the widest scope m's roles give over a type's records (nil: all of them).
func scoped(directory ScopeDirectory, m platform.Member, info platform.EntityInfo, roles []string, now time.Time) (func(reflect.Value) bool, *kernel.Error) {
	// Each role contributes its own predicate; a wider level in one unit must
	// not discard a narrower role's independent unit.
	if len(roles) > 1 {
		var predicates []func(reflect.Value) bool
		for _, role := range roles {
			predicate, err := scoped(directory, m, info, []string{role}, now)
			if err != nil {
				return nil, err
			}
			if predicate == nil {
				return nil, nil
			}
			predicates = append(predicates, predicate)
		}
		return func(v reflect.Value) bool {
			return slices.ContainsFunc(predicates, func(p func(reflect.Value) bool) bool { return p(v) })
		}, nil
	}
	scope := info.Scope
	level := scope.LevelFor(roles)
	field := func(name string) func(reflect.Value) string {
		f, _ := info.Field(name)
		return func(v reflect.Value) string { return v.FieldByIndex(f.Index).String() }
	}
	switch level {
	case platform.ScopeNone:
		return func(reflect.Value) bool { return false }, nil
	case platform.ScopeOwn:
		if scope.Owner == platform.OwnerCreated { // whoever's decision created it
			return func(v reflect.Value) bool { return Header(v).Created.By == m.ID }, nil
		}
		owner := field(scope.Owner)
		return func(v reflect.Value) bool { return owner(v) == m.ID }, nil
	case platform.ScopeUnit, platform.ScopeBelow:
		var units []string
		if directory != nil {
			structure := scope.Structure
			if level == platform.ScopeUnit {
				structure = ""
			}
			// Combine membership-based grants and explicit unit grants of this role.
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
		unit := field(scope.Unit)
		return func(v reflect.Value) bool { return slices.Contains(units, unit(v)) }, nil
	}
	return nil, nil
}

func Write(info platform.EntityInfo, receipt *pb.ChangeRecord, prev *Row, v reflect.Value) (*Row, *kernel.Error) {
	rec := Header(v)
	stamp := platform.Stamp{By: receipt.GetSubmission().GetPrincipalId(), At: receipt.GetRecordedTime().AsTime(), Change: receipt.GetChangeId()}
	change := RecordChange{Change: receipt.GetChangeId(), Schema: receipt.GetSubmission().GetSchema().GetName(), By: stamp.By, At: stamp.At, Fields: []FieldChange{}}
	// The revision is the kernel's (K4 C12): the decisions naming the record as
	// their target, which clients send back as the revision they saw.
	rec.Changed, rec.Created, rec.Revision = stamp, stamp, 0
	target := receipt.GetSubmission().GetTarget()
	named := target.GetType() == info.Type && target.GetId() == rec.ID
	if l := info.Lifecycle; prev == nil && l != nil { // a new record starts in the initial state
		if f, _ := info.Field(l.Field); v.FieldByIndex(f.Index).String() == "" {
			v.FieldByIndex(f.Index).SetString(l.Initial)
		}
	}
	if owner := info.Scope.Owner; prev == nil && owner != "" && owner != platform.OwnerCreated { // a new record belongs to its creator unless the rules say who
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

// CurrentScope is the original fixed SQL read view and decoding position.
// Forked records share its current view, with independent private row overlays.
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

// currentRows resolves committed records through the coordinator's fixed SQL
// view. The collection itself retains only private replacements, never fetched
// values or a tenant-wide copy of the current table.
type currentRows struct {
	store *CurrentScope
	typ   string
	// A declaration replacement decodes persisted values against its target
	// shape before publishing that shape in the store catalog.
	info *platform.EntityInfo
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

// VisitMatching reads the original fixed SQL view and this decision's private
// overlay through the same compiled predicate and permission masks.
func VisitMatching(image IndexImage, lookup ImageLookup, q platform.Query, visible, effective, match func(reflect.Value) bool, writes iter.Seq[string], accept func(reflect.Value)) {
	current := image.Current()
	visit := func(r *Row) {
		if r == nil {
			return
		}
		v := r.Value
		if !q.Archived && Header(v).Archived || visible != nil && !visible(v) || effective != nil && !effective(v) || !match(v) {
			return
		}
		accept(v)
	}
	if ids, indexed := IndexedCandidates(image, lookup, q); indexed {
		for _, id := range ids {
			visit(image.Rows.Get(id))
		}
	} else if current.Backend() != nil {
		var filters []journal.StateFilter
		// SQL retains the original image, while the owner may have renamed or
		// transformed its fields. Filter the converted values below instead.
		if !image.RetainedProjection && len(image.Info.RecordImages) == 0 {
			filters = sqlRecordFilters(image.Info, q.Domain)
		}
		if image.RetainedProjection {
			// Record identity is unchanged by aliases or historical field projections.
			for _, filter := range sqlRecordFilters(image.Info, q.Domain) {
				if len(filter.Path) == 2 && filter.Path[0] == "value" && filter.Path[1] == "id" {
					filters = append(filters, filter)
				}
			}
		}
		pending := map[string]bool{}
		// During checkpoint recovery the fixed SQL archive predates accepted
		// tail writes promoted into the root overlay. They must participate in
		// owner inventories even when their IDs do not exist in that archive.
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
		// Private writes may newly match a condition their committed predecessor
		// did not. Include this decision's overlay before applying the same rules.
		for id := range pending {
			visit(image.Rows.Get(id))
		}
	} else {
		for _, r := range image.Rows.All() {
			visit(r)
		}
	}

}

// CompileSet has already checked the complete expression. Only plain
// conjunctions of physical text/reference equality are reduced here; the
// original evaluator remains the authority for every supported expression.
func sqlRecordFilters(info platform.EntityInfo, domain json.RawMessage) []journal.StateFilter {
	var terms []json.RawMessage
	if len(domain) == 0 || json.Unmarshal(domain, &terms) != nil {
		return nil
	}
	for _, term := range terms {
		if len(term) == 0 || term[0] != '[' {
			return nil
		}
	}
	var out []journal.StateFilter
	for _, raw := range terms {
		var term []json.RawMessage
		var name, op string
		if json.Unmarshal(raw, &term) != nil || len(term) != 3 || json.Unmarshal(term[0], &name) != nil || json.Unmarshal(term[1], &op) != nil {
			continue
		}
		f, ok := info.Field(name)
		if name == "id" {
			ok = true
			f = platform.FieldInfo{Name: "id", Type: "text", Index: []int{0}}
		}
		if !ok || len(f.Index) == 0 || f.AliasOf != "" || f.Property != nil {
			continue
		}
		list := f.Type == "references" || f.Type == "tags"
		if !list && f.Type != "text" && f.Type != "longtext" && f.Type != "reference" && f.Type != "choice" && f.Type != "date" {
			continue
		}
		filter := journal.StateFilter{Path: []string{"value", name}, List: list}
		switch op {
		case "=":
			var value string
			if json.Unmarshal(term[2], &value) != nil {
				continue
			}
			filter.Values = []string{value}
		case "in":
			if json.Unmarshal(term[2], &filter.Values) != nil {
				continue
			}
		default:
			continue
		}
		out = append(out, filter)
	}
	return out
}
