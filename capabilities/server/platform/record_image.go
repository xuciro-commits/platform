package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"reflect"
	"slices"
	"time"
)

// CompactFacts uses the owner's original packed fact representation. A later
// committed image may contain more facts, but readers see only their applied
// immutable prefix; private decisions keep their own new facts separately.
func CompactFacts(log *kernel.FactLog, load func() (json.RawMessage, error)) {
	log.Compact(packedFactArchive{load: load})
}

type packedFactArchive struct {
	load func() (json.RawMessage, error)
}

func (a packedFactArchive) Records(_ string, through int) []*pb.FactRecord {
	if through == 0 {
		return nil
	}
	raw, err := a.load()
	if err != nil {
		panic(fmt.Errorf("fact history: %w", err))
	}
	records, err := Unprotos[*pb.FactRecord](raw)
	if err != nil {
		panic(fmt.Errorf("fact history: %w", err))
	}
	if len(records) < through {
		panic(fmt.Errorf("fact history prefix is incomplete"))
	}
	return records[:through:through]
}
func (a packedFactArchive) Accepted(tenant, key string, through int) *pb.FactRecord {
	for _, r := range a.Records(tenant, through) {
		if r.GetFact().GetTenantId() == tenant && r.GetFact().GetIdempotencyKey() == key {
			return r
		}
	}
	return nil
}
func (a packedFactArchive) Contains(tenant, id string, through int) bool {
	return slices.ContainsFunc(a.Records(tenant, through), func(r *pb.FactRecord) bool { return r.GetFact().GetTenantId() == tenant && r.GetFactId() == id })
}

// ReadQuery projects the owner query through the caller's current member scope.
// Unlike an app's internal Find, this is suitable for page/flow data bindings.
func (c Caller) ReadQuery(app, name string, inputs json.RawMessage, now time.Time) (json.RawMessage, *kernel.Error) {
	if c.rt != nil {
		return c.rt.ReadQuery(c, app, name, inputs, now)
	}
	return nil, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Named queries require the host read path")
}

// ReadQueryVersion selects a retained tenant query. Version zero is reserved
// for code declarations; missing retained sources never fall back to latest.
func (c Caller) ReadQueryVersion(app, name string, version int, inputs json.RawMessage, now time.Time) (json.RawMessage, *kernel.Error) {
	if c.rt != nil {
		return c.rt.ReadQueryVersion(c, app, name, version, inputs, now)
	}
	return nil, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Retained queries require the host read path")
}

func (c Caller) ReadRecord(typ, id string, now time.Time) (json.RawMessage, *kernel.Error) {
	if c.rt != nil {
		return c.rt.ReadRecord(c, typ, id, now)
	}
	return nil, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Record bindings require the host read path")
}

// ReadRecordPath traverses declared single-record references with this caller's
// current permissions. The host supplies field provenance for each hop.
func (c Caller) ReadRecordPath(typ, id string, path []string, now time.Time) (json.RawMessage, []string, *kernel.Error) {
	if c.rt != nil {
		return c.rt.ReadRecordPath(c, typ, id, path, now)
	}
	return nil, nil, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Record paths require the host's scoped reads")
}

// RequestApproval stays in the existing Work request/accepted-result boundary.
func (c Caller) RequestApproval(app string, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	if c.rt != nil {
		return c.rt.RequestApproval(c, app, s, now)
	}
	return nil, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Approval requests require an accepted decision")
}

// RecordImageFormat is a previous persisted shape declared by its model owner.
// Model is the original struct embedding Record. Convert changes only business
// fields and returns the current shape; it must not read state or emit effects.
// The host strictly decodes Model before calling Convert and checks the current
// model and unchanged record identity afterwards.
type RecordImageFormat struct {
	Format  string
	Model   any
	Convert func(json.RawMessage) (json.RawMessage, error)
}

func checkRecordImageFormats(info EntityInfo) error {
	seen := map[string]bool{info.RecordFormat: true}
	for _, image := range info.RecordImages {
		t := reflect.TypeOf(image.Model)
		if seen[image.Format] || t == nil || t.Kind() != reflect.Struct || t.NumField() == 0 ||
			t.Field(0).Type != reflect.TypeFor[Record]() || !t.Field(0).Anonymous || image.Convert == nil {
			return fmt.Errorf("entity %s: invalid or duplicate historical record format %q", info.Type, image.Format)
		}
		seen[image.Format] = true
	}
	return nil
}

func decodeRecordImage(raw json.RawMessage, model reflect.Type) (Record, error) {
	value := reflect.New(model)
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value.Interface()); err != nil {
		return Record{}, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Record{}, fmt.Errorf("record image has trailing data")
	}
	return value.Elem().Field(0).Interface().(Record), nil
}

// DecodeRecordImage validates one saved shape and converts it only through its
// owner's explicit declaration. It leaves the supplied original bytes untouched;
// callers retain those bytes for historical identity and predecessor checks.
func DecodeRecordImage(info EntityInfo, format string, raw json.RawMessage) (json.RawMessage, error) {
	if format == info.RecordFormat {
		_, err := decodeRecordImage(raw, info.Go)
		return raw, err
	}
	for _, image := range info.RecordImages {
		if image.Format != format {
			continue
		}
		before, err := decodeRecordImage(raw, reflect.TypeOf(image.Model))
		if err != nil {
			return nil, fmt.Errorf("record format %q: %w", format, err)
		}
		converted, err := image.Convert(slices.Clone(raw))
		if err != nil {
			return nil, err
		}
		after, err := decodeRecordImage(converted, info.Go)
		if err != nil {
			return nil, fmt.Errorf("record format %q: %w", info.RecordFormat, err)
		}
		if !reflect.DeepEqual(before, after) {
			return nil, fmt.Errorf("record format conversion changed record identity")
		}
		return converted, nil
	}
	return nil, fmt.Errorf("entity %s: unknown record image format %q", info.Type, format)
}

// A record path crosses only declared single-record references. The same
// lookup can validate a publication or a member-filtered discovery projection.
func RecordPathField(typ string, path []string, entity func(string) (EntityInfo, bool)) (FieldInfo, error) {
	if len(path) == 0 || len(path) > 16 {
		return FieldInfo{}, fmt.Errorf("a record path needs 1–16 declared fields")
	}
	var field FieldInfo
	for i, name := range path {
		info, known := entity(typ)
		var found bool
		field, found = info.Field(name)
		if !known || !found {
			return field, fmt.Errorf("record path field %s.%s is unavailable", typ, name)
		}
		if i < len(path)-1 {
			if field.Type != "reference" || field.Ref == "" {
				return field, fmt.Errorf("record path field %s.%s is not a single-record reference", typ, name)
			}
			typ = field.Ref
		}
	}
	return field, nil
}

const RecordSetMaxRows = ExprMaxSet
const RecordSetMaxDeclarations = 64

// RecordSet is an object's bounded collection. From is a declared reference /
// inverse name, or Binding's retained query/link name. It is not a query string.
type RecordSet struct {
	BusinessTime *BusinessTime   `json:"businessTime,omitempty"`
	Name         string          `json:"name"`
	From         string          `json:"from"`
	Binding      *AssetBinding   `json:"binding,omitempty"`
	Direction    string          `json:"direction,omitempty"`
	Object       string          `json:"object"`
	Path         []string        `json:"path,omitempty"`
	Where        []Expr          `json:"where,omitempty"`
	Order        []RecordSetSort `json:"order"`
	Limit        int             `json:"limit"`
}
type RecordSetSort struct {
	Expr Expr `json:"expr"`
	Desc bool `json:"desc,omitempty"`
}

type RecordSetResult struct {
	Authorization *AuthzCollection  `json:"authorization,omitempty"`
	Object        string            `json:"object"`
	Records       []json.RawMessage `json:"records"`
	Partial       bool              `json:"partial"`
	Sources       []string          `json:"sources,omitempty"`
	Definition    string            `json:"definition"`
}

type RecordSetRef struct {
	Object string `json:"object"`
	ID     string `json:"id"`
	Name   string `json:"name"`
}

type RecordSetRuntime interface {
	ReadRecordSet(Caller, string, string, string, time.Time) (RecordSetResult, *kernel.Error)
}

type RecordSetCandidateRuntime interface {
	ReadRecordSetCandidate(Caller, string, string, string, json.RawMessage, time.Time) (RecordSetResult, *kernel.Error)
}

func (c Caller) ruleRecordSet(object, id, name string, record any, at time.Time) (RecordSetResult, *kernel.Error) {
	if runtime, ok := c.rt.(RecordSetCandidateRuntime); ok && c.Staging() {
		return runtime.ReadRecordSetCandidate(c, object, id, name, Raw(record), at)
	}
	return c.ReadRecordSet(object, id, name, at)
}

// RecordSetPredicate lowers an optional reference prefix into the same Expr
// path operation used everywhere else. Order always addresses the set element.
func LowerRecordSetPredicate(set RecordSet, expr Expr, lookup func(string) (EntityInfo, bool), engines ...RuleEngine) (Expr, error) {
	if len(set.Path) == 0 {
		return expr, nil
	}
	field, err := RecordPathField(set.Object, set.Path, lookup)
	if err != nil || field.Type != "reference" || field.Ref == "" {
		return Expr{}, fmt.Errorf("set %s filter path must end at a declared record reference", set.Name)
	}
	schemaLookup := func(typ string) (EntityInfo, bool) { info, ok := lookup(typ); return ConstraintEntity(info), ok }
	if _, err := RuleSchema(expr, RuleReadSpec{Object: field.Ref}, schemaLookup, engines...); err != nil {
		return Expr{}, err
	}
	var lower func(Expr) Expr
	lower = func(node Expr) Expr {
		ref, tail := node.Ref, []string{}
		if node.Op == "path" && len(node.Args) == 1 {
			ref, tail = node.Args[0].Ref, node.Path
		}
		if ref != "" && ref != "caller.id" {
			path := append(slices.Clone(set.Path[1:]), ref)
			path = append(path, tail...)
			return Expr{Op: "path", Kind: node.Kind, Args: []Expr{ExprReference("reference", set.Path[0])}, Path: path}
		}
		node.Args = slices.Clone(node.Args)
		for i := range node.Args {
			node.Args[i] = lower(node.Args[i])
		}
		if node.In != nil {
			value := lower(*node.In)
			node.In = &value
		}
		return node
	}
	lowered := lower(expr)
	_, err = RuleSchema(lowered, RuleReadSpec{Object: set.Object}, schemaLookup, engines...)
	return lowered, err
}

func (c Caller) ReadRecordSet(object, id, name string, at time.Time) (RecordSetResult, *kernel.Error) {
	runtime, ok := c.rt.(RecordSetRuntime)
	if !ok {
		return RecordSetResult{}, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Declared record sets require the host")
	}
	return runtime.ReadRecordSet(c, object, id, name, at)
}

func (s *RecordSet) UnmarshalJSON(raw []byte) error {
	type plain RecordSet
	return strictTxn(raw, (*plain)(s))
}

func CheckRecordSets(info EntityInfo, lookup func(string) (EntityInfo, bool), engines ...RuleEngine) error {
	if len(info.Sets) > RecordSetMaxDeclarations {
		return fmt.Errorf("object has %d sets; limit is %d", len(info.Sets), RecordSetMaxDeclarations)
	}
	seen := map[string]bool{}
	for _, set := range info.Sets {
		if !pageNodeID.MatchString(set.Name) || seen[set.Name] || set.From == "" || set.Limit < 1 || set.Limit > RecordSetMaxRows || len(set.Path) > 16 || len(set.Where) > 32 || len(set.Order) == 0 || len(set.Order) > 16 {
			return fmt.Errorf("set %s needs a unique name, declared source, stable order and limit 1-%d", set.Name, RecordSetMaxRows)
		}
		seen[set.Name] = true
		target, ok := lookup(set.Object)
		if !ok {
			return fmt.Errorf("set %s object is undeclared", set.Name)
		}
		if set.Binding != nil {
			if set.Binding.Ref.Check() != nil || set.Binding.SourceVersion == "" || !slices.Contains([]AssetKind{AssetQuery, AssetLinkType}, set.Binding.Ref.Kind) || set.Binding.Ref.Name != set.From || set.Direction != "forward" && set.Direction != "reverse" && set.Binding.Ref.Kind == AssetLinkType {
				return fmt.Errorf("set %s needs a retained query or single-hop link binding", set.Name)
			}
		} else {
			if set.Direction != "reference" && set.Direction != "inverse" {
				return fmt.Errorf("set %s needs a reference direction", set.Name)
			}
			if set.Direction == "reference" {
				field, found := info.Field(set.From)
				if !found || field.Type != "reference" || field.Ref != set.Object {
					return fmt.Errorf("set %s source is not its typed reference", set.Name)
				}
			} else if !slices.ContainsFunc(target.Fields, func(field FieldInfo) bool { return field.Ref == info.Type && field.Inverse == set.From }) {
				return fmt.Errorf("set %s inverse is undeclared", set.Name)
			}
		}
		schemaLookup := func(typ string) (EntityInfo, bool) { other, ok := lookup(typ); return ConstraintEntity(other), ok }
		check := func(expr Expr) error {
			var visit func(Expr) error
			visit = func(expr Expr) error {
				if expr.Agg != nil {
					return fmt.Errorf("record sets cannot nest collections")
				}
				for _, child := range expr.Args {
					if err := visit(child); err != nil {
						return err
					}
				}
				if expr.In != nil {
					return visit(*expr.In)
				}
				return nil
			}
			if err := visit(expr); err != nil {
				return err
			}
			_, err := RuleSchema(expr, RuleReadSpec{Object: target.Type}, schemaLookup, engines...)
			return err
		}
		for _, where := range set.Where {
			if where.Kind != "boolean" {
				return fmt.Errorf("set predicate must be boolean")
			}
			where, err := LowerRecordSetPredicate(set, where, lookup, engines...)
			if err != nil {
				return err
			}
			if err := check(where); err != nil {
				return err
			}
		}
		for _, order := range set.Order {
			if !slices.Contains([]string{"text", "choice", "integer", "decimal", "number", "date", "datetime", "boolean", "reference"}, order.Expr.Kind) {
				return fmt.Errorf("set order needs a scalar expression")
			}
			if err := check(order.Expr); err != nil {
				return err
			}
		}
		if len(set.Path) > 0 {
			field, err := RecordPathField(set.Object, set.Path, lookup)
			if err != nil {
				return err
			}
			if field.Type != "reference" || field.Ref == "" {
				return fmt.Errorf("set filter path must resolve a record")
			}
		}
	}
	return nil
}
