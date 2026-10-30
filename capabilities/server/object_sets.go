package platformserver

import transport "platformserver/kernel/transport"

import (
	"encoding/json"
	"io"
	"net/http"
	"platformserver/internal/rules"
	"platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/studio/build"
	"reflect"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

type ObjectSetRequest struct {
	Set        platform.ObjectSet            `json:"set"`
	Sort       []string                      `json:"sort,omitempty"`
	Offset     int                           `json:"offset,omitempty"`
	Limit      int                           `json:"limit,omitempty"`
	Groups     []string                      `json:"groups,omitempty"`
	Measures   []string                      `json:"measures,omitempty"`
	Window     *records.AggregateWindowQuery `json:"window,omitempty"`
	Histogram  *records.HistogramQuery       `json:"histogram,omitempty"`
	MaxRows    int                           `json:"maxRows,omitempty"`
	Collection *platform.RecordSetRef        `json:"collection,omitempty"`
	Format     bool                          `json:"format,omitempty"`
}

type ObjectSetCount struct {
	Count   int  `json:"count"`
	Partial bool `json:"partial,omitempty"`
}

type objectWindow struct {
	Sort          []string
	Offset, Limit int
	Bounded       bool
	Format        bool
}

const (
	objectSetStartLimit    = rules.MaxSet
	objectSetInterfaceRows = 200
	objectSetObjectRows    = 500
)

type typeRead struct {
	entity, view *records.Type
	hidden       []platform.FieldInfo
	visible      func(reflect.Value) bool
}

type interfaceRead struct {
	query platform.NamedQuery
	info  platform.EntityInfo
	reads []*typeRead
}

type setCondition struct {
	field, op string
	value     any
}

type setNode struct {
	set      platform.ObjectSet
	object   string
	iface    string
	terms    []setCondition
	archived bool
	asOf     *platform.BusinessTime
	op       string
	operands []*setNode
}

type compiledSet struct {
	reads []*typeRead
	plans []records.Plan
}

type setRead struct {
	t         *Tenant
	store     *records.Store
	c         platform.Caller
	now       time.Time
	bound     map[string]platform.NamedQuery
	aggregate bool
	types     map[string]*typeRead
	ifaces    map[string]*interfaceRead
}

func (t *Tenant) newSetRead(store *records.Store, c platform.Caller, now time.Time, bound ...platform.NamedQuery) (*setRead, *kernel.Error) {
	c.Member = t.authorizationMember(c.Member)
	if err := t.admits(c.Member); err != nil {
		return nil, err
	}
	r := &setRead{t: t, store: store, c: c, now: now, bound: map[string]platform.NamedQuery{}, types: map[string]*typeRead{}, ifaces: map[string]*interfaceRead{}}
	for _, q := range bound {
		r.bound[q.Interface] = q
	}
	return r, nil
}

func invalidSet(message string) *kernel.Error {
	return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, message)
}

func (r *setRead) typeRead(name string) (*typeRead, *kernel.Error) {
	if read, ok := r.types[name]; ok {
		return read, nil
	}
	r.store.Lock()
	et := r.store.Type(name)
	r.store.Unlock()
	if et == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	visible, err := r.t.visibleForCallerIn(r.store, r.c, et, r.now)
	if err != nil {
		return nil, err
	}
	read := &typeRead{entity: et, visible: visible}
	if r.aggregate {
		read.view = r.t.aggregateView(r.c.Member, et)
		_, read.hidden = viewOf(r.c.Member, et)
	} else {
		read.view, read.hidden = viewOf(r.c.Member, et)
	}
	r.types[name] = read
	return read, nil
}

func (r *setRead) interfaceRead(name string) (*interfaceRead, *kernel.Error) {
	if read, ok := r.ifaces[name]; ok {
		return read, nil
	}
	declared, ok := r.bound[name]
	if !ok {
		bound, err := r.t.bindInterfaceQuery(platform.NamedQuery{Name: "lookup", Title: "Interface lookup", Interface: name})
		if err != nil {
			return nil, invalidSet(err.Error())
		}
		declared = bound
	}
	if err := declared.Check(); err != nil {
		return nil, invalidSet(err.Error())
	}
	if err := checkInterfaceQueryFields(declared); err != nil {
		return nil, invalidSet(err.Error())
	}
	read := &interfaceRead{query: declared, info: interfaceShapeInfo(declared)}
	for _, typ := range declared.Implementations {
		r.store.Lock()
		et := r.store.Type(typ)
		r.store.Unlock()
		if et == nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A published query implementation is unavailable")
		}
		visible, err := r.t.visibleForCallerIn(r.store, r.c, et, r.now)
		if err != nil {
			if err.Code == pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
				continue
			}
			return nil, err
		}
		view, hidden := viewOf(r.c.Member, et)
		if declared.InterfaceShape.Implements(view.Info()) != nil {
			continue
		}
		common := view.Info()
		common.Fields = slices.DeleteFunc(slices.Clone(view.Info().Fields), func(field platform.FieldInfo) bool {
			return !slices.ContainsFunc(declared.InterfaceShape.Fields, func(c platform.InterfaceField) bool { return c.Name == field.Name })
		})
		read.reads = append(read.reads, &typeRead{entity: et, view: records.NewTypeView(common, view.Rows(), view.RetainedProjection()), hidden: hidden, visible: visible})
	}
	r.ifaces[name] = read
	return read, nil
}

func interfaceShapeInfo(q platform.NamedQuery) platform.EntityInfo {
	info := platform.EntityInfo{}
	for _, f := range q.InterfaceShape.Fields {
		info.Fields = append(info.Fields, platform.FieldInfo{Name: f.Name, Type: f.Type})
	}
	return info
}

func (r *setRead) prepare(s platform.ObjectSet, archived bool, asOf *platform.BusinessTime) (*setNode, *kernel.Error) {
	n := &setNode{set: s, archived: archived || s.Archived, asOf: asOf}
	if s.AsOf != nil {
		n.asOf = s.AsOf
	}
	if len(s.IDs) > 0 {
		n.terms = append(n.terms, setCondition{"id", "in", s.IDs})
	}
	op, operands := s.Operation()
	switch {
	case s.Object != "":
		n.object = s.Object
		if _, err := r.typeRead(n.object); err != nil {
			return nil, err
		}
	case s.Interface != "":
		n.iface = s.Interface
		read, err := r.interfaceRead(n.iface)
		if err != nil {
			return nil, err
		}
		if s.Domain != nil {
			if _, err := records.CompileDomain(read.info, s.Domain); err != nil {
				return nil, invalidSet(err.Error())
			}
		}
	case s.Around != nil:
		target, terms, err := r.around(*s.Around)
		if err != nil {
			return nil, err
		}
		n.object, n.terms = target, append(n.terms, terms...)
		if _, err := r.typeRead(n.object); err != nil {
			return nil, err
		}
	case operands != nil:
		n.op = op
		for _, operand := range operands {
			child, err := r.prepare(operand, n.archived, n.asOf)
			if err != nil {
				return nil, err
			}
			if len(n.operands) > 0 && (child.object != n.operands[0].object || child.iface != n.operands[0].iface) {
				return nil, invalidSet("Set operation operands must share one element type")
			}
			n.operands = append(n.operands, child)
		}
		n.object, n.iface = n.operands[0].object, n.operands[0].iface
	default:
		return nil, invalidSet("An object set needs a base")
	}
	return n, nil
}

func explicitIDs(s platform.ObjectSet) ([]string, bool) {
	if s.Object == "" || len(s.IDs) == 0 || s.Filter != nil || len(s.Domain) > 0 || s.Search != "" || s.AsOf != nil {
		return nil, false
	}
	return s.IDs, true
}

func (r *setRead) around(a platform.SearchAround) (string, []setCondition, *kernel.Error) {
	t, m := r.t, r.c.Member
	notFound := platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Link type or start record is unavailable")
	if a.Link.Ref.Kind != platform.AssetLinkType || a.Link.SourceVersion == "" || a.Direction != "forward" && a.Direction != "reverse" {
		return "", nil, invalidSet("A link traversal needs a version, direction and start record")
	}
	if r.store != t.records {
		return "", nil, invalidSet("Link traversal is unavailable in a staged read")
	}
	link := t.visibleLink(m, a.Link)
	if link == nil {
		return "", nil, notFound
	}
	start, target := link.Parent.Name, link.Child.Name
	if a.Direction == "reverse" {
		start, target = target, start
	}
	from := a.From
	if from.Object == "" && from.Interface == "" && from.Around == nil && len(from.IDs) > 0 && len(from.Union)+len(from.Intersect)+len(from.Subtract) == 0 {
		from.Object = start
	}
	explicit, strict := explicitIDs(from)
	strict = strict && from.Object == start
	var ids []string
	var parents []string
	if strict {
		ids = slices.Clone(explicit)
	} else {
		node, err := r.prepare(from, false, nil)
		if err != nil {
			return "", nil, err
		}
		if node.object != start {
			return "", nil, invalidSet("Link start object does not match its direction")
		}
		rows, read, err := r.startRows(node)
		if err != nil {
			return "", nil, err
		}
		via, hasVia := read.view.Info().Field(link.Via)
		for _, row := range rows {
			ids = append(ids, records.Header(row).ID)
			if a.Direction == "reverse" && link.Storage != "edge" && hasVia {
				if parent := row.FieldByIndex(via.Index).String(); parent != "" {
					parents = append(parents, parent)
				}
			}
		}
	}
	slices.Sort(ids)
	ids = append([]string{}, slices.Compact(ids)...)
	if link.Storage == "edge" {
		caller := platform.NewCaller(runtime{t}, m, link.Child.App, false, false)
		var targets []string
		for _, id := range ids {
			found, _, err := t.ownedRelationTargets(r.store, caller, *link, a.Direction, id, r.now)
			if err != nil {
				return "", nil, err
			}
			targets = append(targets, found...)
		}
		slices.Sort(targets)
		return target, []setCondition{{"id", "in", append([]string{}, slices.Compact(targets)...)}}, nil
	}
	if a.Direction == "forward" {
		if strict {
			for _, id := range ids {
				if _, err := t.recordOf(m, link.Parent.Name, id, r.now); err != nil {
					return "", nil, notFound
				}
			}
		}
		if len(ids) == 1 {
			return target, []setCondition{{link.Via, "=", ids[0]}}, nil
		}
		return target, []setCondition{{link.Via, "in", ids}}, nil
	}
	if strict {
		for _, id := range ids {
			view, err := t.recordOf(m, link.Child.Name, id, r.now)
			if err != nil {
				return "", nil, notFound
			}
			var fields map[string]json.RawMessage
			var parent string
			if json.Unmarshal(platform.Raw(view.Record), &fields) != nil {
				return "", nil, notFound
			}
			if len(fields[link.Via]) > 0 && json.Unmarshal(fields[link.Via], &parent) != nil {
				return "", nil, notFound
			}
			if parent != "" {
				if _, err := t.recordOf(m, link.Parent.Name, parent, r.now); err != nil {
					return "", nil, notFound
				}
				parents = append(parents, parent)
			}
		}
	}
	slices.Sort(parents)
	parents = append([]string{}, slices.Compact(parents)...)
	if len(parents) == 1 {
		return target, []setCondition{{"id", "=", parents[0]}}, nil
	}
	return target, []setCondition{{"id", "in", parents}}, nil
}

func (r *setRead) startRows(node *setNode) ([]reflect.Value, *typeRead, *kernel.Error) {
	read := r.types[node.object]
	r.store.Lock()
	defer r.store.Unlock()
	cs, err := r.compile(node)
	if err != nil {
		return nil, nil, err
	}
	var rows []reflect.Value
	r.store.VisitPlan(cs.reads[0].view, cs.plans[0], cs.reads[0].visible, func(v reflect.Value) { rows = append(rows, v) })
	if len(rows) > objectSetStartLimit {
		return nil, nil, invalidSet("Search around starts from too many records")
	}
	return rows, read, nil
}

func (r *setRead) leaf(n *setNode, read *typeRead) (records.Plan, *kernel.Error) {
	invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	info := read.view.Info()
	var parts []rules.Expr
	for _, term := range n.terms {
		expr, err := records.ConditionExpr(info, term.field, term.op, term.value)
		if err != nil {
			return records.Plan{}, invalid
		}
		parts = append(parts, expr)
	}
	if len(n.set.Domain) > 0 {
		expr, err := records.DomainToExpr(info, n.set.Domain)
		if err != nil {
			return records.Plan{}, invalid
		}
		parts = append(parts, expr)
	}
	if n.set.Filter != nil {
		filter := *n.set.Filter
		var bind func(*rules.Expr) *kernel.Error
		bind = func(expr *rules.Expr) *kernel.Error {
			if expr.Ref != "" {
				if value, ok := n.set.Parameters[expr.Ref]; ok {
					if value.Kind != expr.Kind {
						return invalidSet("Object set parameter type differs")
					}
					raw := value.Data
					if value.Status == "null" {
						raw = json.RawMessage("null")
					}
					*expr = rules.Expr{Kind: value.Kind, Lit: &rules.Literal{Value: raw}}
				} else if len(expr.Ref) >= len("root.") && expr.Ref[:len("root.")] == "root." {
					return invalidSet("Object set root parameter is missing")
				}
			}
			expr.Args = slices.Clone(expr.Args)
			for i := range expr.Args {
				if err := bind(&expr.Args[i]); err != nil {
					return err
				}
			}
			if expr.In != nil {
				in := *expr.In
				expr.In = &in
				return bind(expr.In)
			}
			return nil
		}
		if err := bind(&filter); err != nil {
			return records.Plan{}, err
		}
		parts = append(parts, filter)
	}
	plan, err := records.CompilePlan(info, records.AllOf(parts...), n.set.Search)
	if err != nil {
		return records.Plan{}, invalid
	}
	return plan, nil
}

func (r *setRead) compile(n *setNode) (compiledSet, *kernel.Error) {
	var reads []*typeRead
	if n.iface != "" {
		reads = r.ifaces[n.iface].reads
	} else {
		reads = []*typeRead{r.types[n.object]}
	}
	out := compiledSet{reads: reads, plans: make([]records.Plan, len(reads))}
	if n.op == "" {
		for i, read := range reads {
			plan, err := r.leaf(n, read)
			if err != nil {
				return compiledSet{}, err
			}
			query := businessQuery(read.entity.Info(), platform.Query{BusinessTime: n.asOf}, r.c.Member, r.now)
			effective, refusal := r.store.EffectivePredicate(read.view, query)
			if refusal != nil {
				return compiledSet{}, refusal
			}
			out.plans[i] = plan.Scoped(n.archived, effective)
		}
		return out, nil
	}
	operands := make([]compiledSet, len(n.operands))
	for i, operand := range n.operands {
		compiled, err := r.compile(operand)
		if err != nil {
			return compiledSet{}, err
		}
		operands[i] = compiled
	}
	narrowed := len(n.terms) > 0 || len(n.set.Domain) > 0 || n.set.Filter != nil || n.set.Search != ""
	for i, read := range reads {
		plans := make([]records.Plan, len(operands))
		for j := range operands {
			plans[j] = operands[j].plans[i]
		}
		switch n.op {
		case "union":
			out.plans[i] = records.Union(plans...)
		case "intersect":
			out.plans[i] = records.Intersect(plans...)
		default:
			out.plans[i] = records.Subtract(plans[0], plans[1:]...)
		}
		if narrowed {
			plan, err := r.leaf(n, read)
			if err != nil {
				return compiledSet{}, err
			}
			out.plans[i] = records.Intersect(plan, out.plans[i])
		}
	}
	return out, nil
}

func (r *setRead) narrowRecord(read *typeRead, w *narrower, value reflect.Value) any {
	record := records.Mask(read.entity.Info(), value, read.hidden)
	if read.entity.Info().HasReadSources() || len(read.entity.Info().VersionPins) > 0 {
		narrowed, _ := w.derive(read.entity, reflect.ValueOf(record))
		record = narrowed.Interface()
	}
	return record
}

func (t *Tenant) loadObjectSet(store *records.Store, c platform.Caller, set platform.ObjectSet, w objectWindow, now time.Time, bound ...platform.NamedQuery) (RecordPage, string, *kernel.Error) {
	r, err := t.newSetRead(store, c, now, bound...)
	if err != nil {
		return RecordPage{}, "", err
	}
	node, err := r.prepare(set, false, nil)
	if err != nil {
		return RecordPage{}, "", err
	}
	if node.iface != "" {
		page, err := r.loadInterface(node, w)
		return page, "", err
	}
	page, err := r.loadObjects(node, w)
	return page, node.object, err
}

func (r *setRead) loadObjects(node *setNode, w objectWindow) (RecordPage, *kernel.Error) {
	t, m, store := r.t, r.c.Member, r.store
	read := r.types[node.object]
	if w.Bounded && (w.Limit < 1 || w.Limit > objectSetObjectRows) {
		w.Limit = objectSetObjectRows
	}
	store.Lock()
	defer store.Unlock()
	order, problem := records.CompileOrder(read.view.Info(), w.Sort)
	if problem != nil {
		return RecordPage{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	cs, err := r.compile(node)
	if err != nil {
		return RecordPage{}, err
	}
	collector := order.Page(w.Offset, w.Limit)
	matched := store.VisitPlan(read.view, cs.plans[0], read.visible, collector.Add)
	page, total := collector.Result()
	out := RecordPage{Records: make([]any, len(page)), Total: total, Partial: matched > total}
	ids := make([]string, 0, len(page))
	narrower := t.narrowerIn(store, m, r.now, true)
	for i, v := range page {
		out.Records[i] = r.narrowRecord(read, narrower, v)
		ids = append(ids, records.Header(v).ID)
		if w.Format {
			if formats := read.entity.Formats().Evaluate(out.Records[i], read.hidden); len(formats) > 0 {
				if out.Formats == nil {
					out.Formats = map[string]map[string]platform.FieldFormat{}
				}
				out.Formats[ids[i]] = formats
			}
		}
	}
	t.readPersonal(m, read.view, ids, r.now)
	return out, nil
}

type interfaceRow struct {
	read  *typeRead
	value reflect.Value
	keys  []any
}

func (r *setRead) loadInterface(node *setNode, w objectWindow) (RecordPage, *kernel.Error) {
	t, m, store := r.t, r.c.Member, r.store
	if w.Offset < 0 || w.Limit < 0 {
		return RecordPage{}, invalidSet("offset and limit are non-negative")
	}
	if w.Limit == 0 || w.Limit > objectSetInterfaceRows {
		w.Limit = objectSetInterfaceRows
	}
	declared := r.ifaces[node.iface]
	shape := declared.query.InterfaceShape
	if len(w.Sort) > 4 {
		return RecordPage{}, invalidSet("query has an invalid identity or budget")
	}
	if _, problem := records.CompileOrder(declared.info, w.Sort); problem != nil {
		return RecordPage{}, invalidSet(problem.Error())
	}
	store.Lock()
	defer store.Unlock()
	cs, err := r.compile(node)
	if err != nil {
		return RecordPage{}, err
	}
	var rows []interfaceRow
	var order records.SortOrder
	for i, read := range cs.reads {
		var problem error
		order, problem = records.CompileOrder(read.view.Info(), w.Sort)
		if problem != nil {
			return RecordPage{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		}
		store.VisitPlan(read.view, cs.plans[i], read.visible, func(value reflect.Value) {
			rows = append(rows, interfaceRow{read: read, value: value, keys: append(order.Keys(value), read.entity.Info().Type)})
		})
	}
	slices.SortFunc(rows, func(a, b interfaceRow) int { return order.Compare(a.keys, b.keys) })
	out := RecordPage{Records: []any{}, Total: len(rows)}
	start := min(w.Offset, len(rows))
	end := min(start+w.Limit, len(rows))
	for _, row := range rows[start:end] {
		value := reflect.ValueOf(records.Mask(row.read.entity.Info(), row.value, row.read.hidden))
		if row.read.entity.Info().HasReadSources() {
			value, _ = t.narrowerIn(store, m, r.now, true).derive(row.read.entity, value)
		}
		rec := records.Header(row.value)
		fields := map[string]any{"id": rec.ID, "revision": rec.Revision, "created": rec.Created, "changed": rec.Changed}
		for _, common := range shape.Fields {
			f, _ := row.read.entity.Info().Field(common.Name)
			fields[f.Name] = value.FieldByIndex(f.Index).Interface()
		}
		out.Records = append(out.Records, InterfaceRecord{Type: row.read.entity.Info().Type, ID: rec.ID, Record: fields})
		t.readPersonal(m, row.read.view, []string{rec.ID}, r.now)
	}
	return out, nil
}

func (t *Tenant) countObjectSet(store *records.Store, c platform.Caller, set platform.ObjectSet, now time.Time) (ObjectSetCount, *kernel.Error) {
	r, err := t.newSetRead(store, c, now)
	if err != nil {
		return ObjectSetCount{}, err
	}
	node, err := r.prepare(set, false, nil)
	if err != nil {
		return ObjectSetCount{}, err
	}
	store.Lock()
	defer store.Unlock()
	cs, err := r.compile(node)
	if err != nil {
		return ObjectSetCount{}, err
	}
	var out ObjectSetCount
	matched := 0
	for i, read := range cs.reads {
		matched += store.VisitPlan(read.view, cs.plans[i], read.visible, func(reflect.Value) { out.Count++ })
	}
	out.Partial = matched > out.Count
	return out, nil
}

func (t *Tenant) aggregateObjectSet(m platform.Member, req ObjectSetRequest, now time.Time) (records.Aggregate, *kernel.Error) {
	c := platform.NewCaller(runtime{t}, m, "", false, false)
	r, err := t.newSetRead(t.records, c, now)
	if err != nil {
		return records.Aggregate{}, err
	}
	m = r.c.Member
	options := records.AggregateQuery{Window: req.Window, Histogram: req.Histogram, MaxRows: req.MaxRows, Groups: req.Groups, Measures: req.Measures}
	if req.Collection != nil {
		return r.aggregateCollection(req, options)
	}
	r.aggregate = true
	node, err := r.prepare(req.Set, false, nil)
	if err != nil {
		return records.Aggregate{}, err
	}
	if node.iface != "" {
		return records.Aggregate{}, invalidSet("An aggregate reads one object type")
	}
	read := r.types[node.object]
	s := t.records
	s.Lock()
	defer s.Unlock()
	cs, err := r.compile(node)
	if err != nil {
		return records.Aggregate{}, err
	}
	var rows []reflect.Value
	matched := s.VisitPlan(read.view, cs.plans[0], read.visible, func(v reflect.Value) { rows = append(rows, v) })
	shown := len(rows)
	out, refusal := s.Aggregate(read.view, options, rows, node.op != "")
	if refusal == nil && options.Window == nil && options.Histogram == nil {
		out.Partial = matched != shown
	}
	return out, refusal
}

func (r *setRead) aggregateCollection(req ObjectSetRequest, options records.AggregateQuery) (records.Aggregate, *kernel.Error) {
	t, m := r.t, r.c.Member
	set := req.Set
	typ := set.Object
	if options.Window != nil || options.Histogram != nil || typ == "" || set.IDs != nil || set.Filter != nil || len(set.Domain) > 0 || set.Search != "" || set.Archived {
		return records.Aggregate{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A declared collection cannot be combined with another selection")
	}
	root, ok := t.entity(req.Collection.Object)
	if !ok {
		return records.Aggregate{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Collection owner is unavailable")
	}
	caller := platform.NewCaller(runtime{t}, m, root.App, false, false)
	page, err := caller.ReadRecordSet(root.Type, req.Collection.ID, req.Collection.Name, r.now)
	if err != nil {
		return records.Aggregate{}, err
	}
	if page.Object != typ {
		return records.Aggregate{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Collection element differs from aggregate object")
	}
	t.records.Lock()
	defer t.records.Unlock()
	et := t.records.Type(typ)
	if et == nil {
		return records.Aggregate{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Collection object is unavailable")
	}
	view := t.aggregateView(m, et)
	rows := make([]reflect.Value, len(page.Records))
	for i, raw := range page.Records {
		value := reflect.New(et.Info().Go).Elem()
		if json.Unmarshal(raw, value.Addr().Interface()) != nil {
			return records.Aggregate{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Collection row is invalid")
		}
		rows[i] = value
	}
	out, refusal := t.records.Aggregate(view, options, rows, false)
	out.Partial = page.Partial
	return out, refusal
}

func (h *Host) objectSetRoutes(rt *routes) {
	decode := func(w http.ResponseWriter, r *http.Request, op string) (ObjectSetRequest, bool) {
		var req *ObjectSetRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, platform.QuerySetMaxBytes))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&req) != nil || req == nil || decoder.Decode(new(any)) != io.EOF || req.Offset < 0 || req.Limit < 0 || req.Set.Check() != nil {
			Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT})
			return ObjectSetRequest{}, false
		}
		paged := len(req.Sort) > 0 || req.Offset != 0 || req.Limit != 0
		aggregated := len(req.Groups) > 0 || len(req.Measures) > 0 || req.Window != nil || req.Histogram != nil || req.MaxRows != 0 || req.Collection != nil
		if op == "load" && aggregated || op == "aggregate" && (paged || req.Format) || op == "count" && (paged || aggregated || req.Format) {
			Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The request carries options of another object set operation"))
			return ObjectSetRequest{}, false
		}
		return *req, true
	}
	rt.recordRead(transport.Route{Pattern: "POST /v1/object-sets/load", Summary: "Load a page of one composable object set in the caller's scope: sort, offset, limit and the complete visible total", Body: ObjectSetRequest{}, Answer: RecordPage{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		req, ok := decode(w, r, "load")
		if !ok {
			return
		}
		page, object, err := t.loadObjectSet(t.records, platform.NewCaller(runtime{t}, m, "", false, false), req.Set, objectWindow{Sort: req.Sort, Offset: req.Offset, Limit: req.Limit, Bounded: true, Format: req.Format}, h.Now())
		if err == nil && object == build.ObjectType {
			page, err = t.objectDraftPage(m, page)
		}
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, page)
	})
	rt.recordRead(transport.Route{Pattern: "POST /v1/object-sets/aggregate", Summary: "Aggregate one composable object set: groups and measures, a histogram or a recent-event window", Body: ObjectSetRequest{}, Answer: records.Aggregate{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		req, ok := decode(w, r, "aggregate")
		if !ok {
			return
		}
		out, err := t.aggregateObjectSet(m, req, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
	rt.recordRead(transport.Route{Pattern: "POST /v1/object-sets/count", Summary: "Count the records of one composable object set that the caller may read", Body: ObjectSetRequest{}, Answer: ObjectSetCount{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		req, ok := decode(w, r, "count")
		if !ok {
			return
		}
		out, err := t.countObjectSet(t.records, platform.NewCaller(runtime{t}, m, "", false, false), req.Set, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
}
