package automation

import (
	"encoding/json"
	"fmt"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
	"reflect"
	"slices"
)

type RetainedSource func() (shape string, value json.RawMessage, ok bool)

func RetainedFields(current, info platform.EntityInfo, raw []byte, source RetainedSource) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil, fmt.Errorf("retained record cannot be decoded")
	}
	var prior map[string]json.RawMessage
	for _, old := range info.Fields {
		next, found := current.Field(old.Name)
		_, _, hasSource := source()
		if found && next.AliasOf != "" {
			fields[old.Name] = fields[next.AliasOf]
		} else if !found || next.Type != old.Type || next.Ref != old.Ref || next.ReadOnly && !old.ReadOnly && hasSource {
			if prior == nil {
				shape, value, ok := source()
				digest, _ := platform.CanonicalDigest(info.Fields)
				if !ok || shape != digest || json.Unmarshal(value, &prior) != nil {
					return nil, fmt.Errorf("retained field %s.%s has no saved source image", info.Type, old.Name)
				}
			}
			fields[old.Name] = prior[old.Name]
		}
	}
	return json.Marshal(fields)
}

func RetainedView(reads *records.ForkCache, base *records.Store, release string, shapes map[string]platform.EntityInfo) (*records.Store, error) {
	return reads.Fork(base, func(store *records.Store, original func(string) records.Rows) error {
		for typ, info := range shapes {
			current := store.Type(typ)
			if current == nil {
				return fmt.Errorf("retained object %s has no current storage", typ)
			}
			stored, rows := current.Info(), original(typ)
			store.ReplaceReadType(info, records.SourceRows(RetainedRows[*records.Row]{
				List:  func(after string, limit int) []string { return rows.IDs(after, limit) },
				Fetch: func(id string) *records.Row { return rows.Get(id) },
				Batch: func(ids []string) map[string]*records.Row { return rows.GetBatch(ids) },
				Convert: func(row *records.Row) (*records.Row, *kernel.Error) {
					converted, err := RetainedRow(stored, info, release, row)
					if err != nil {
						return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
					}
					return converted, nil
				},
			}), true)
		}
		return nil
	})
}

func RetainedRow(current, info platform.EntityInfo, release string, value *records.Row) (*records.Row, error) {
	raw, err := value.Image()
	if err != nil {
		return nil, err
	}
	fields, err := RetainedFields(current, info, raw, func() (string, json.RawMessage, bool) {
		source := records.ProjectionFor(value.Projection, release)
		if source == nil {
			return "", nil, false
		}
		return source.Shape, source.Value, true
	})
	if err != nil {
		return nil, err
	}
	decoded := reflect.New(info.Go)
	if err := json.Unmarshal(fields, decoded.Interface()); err != nil {
		return nil, err
	}
	return &records.Row{Value: decoded.Elem(), Format: value.Format, HistoryHead: value.HistoryHead, HistoryParent: value.HistoryParent, History: value.History, LoadHistory: value.LoadHistory, HistoryCount: value.HistoryCount}, nil
}

func RetainedGet(base *records.Store, shapes map[string]platform.EntityInfo, release string, c platform.Caller, typ reflect.Type, id string) (any, bool, error) {
	for name, info := range shapes {
		if info.Go != typ || info.App != c.App {
			continue
		}
		var value *records.Row
		var stored platform.EntityInfo
		func() {
			base.Lock()
			defer base.Unlock()
			current := base.Type(name)
			if current != nil {
				value, stored = current.Rows().Get(id), current.Info()
			}
		}()
		if value == nil {
			return nil, false, nil
		}
		decoded, err := RetainedRow(stored, info, release, value)
		if err != nil {
			return nil, false, err
		}
		return decoded.Value.Interface(), true, nil
	}
	out, ok := base.Get(c, typ, id)
	return out, ok, nil
}

func RetainedWrite(shape, old, current platform.EntityInfo, before, input, stored map[string]json.RawMessage, hasStored bool) (map[string]json.RawMessage, []string, error) {
	before, input = maps.Clone(before), maps.Clone(input)
	for _, target := range current.Fields {
		if string(before[target.Name]) == string(input[target.Name]) || target.AliasOf != "" {
			continue
		}
		field, found := shape.Field(target.Name)
		if !found || field.Type != target.Type || field.Ref != target.Ref {
			return nil, nil, fmt.Errorf("retained write %s lacks its declared field conversion", target.Name)
		}
	}
	result := stored
	if !hasStored {
		result = maps.Clone(input)
	}
	fields := []string{}
	for _, target := range current.Fields {
		if string(input[target.Name]) == string(before[target.Name]) {
			continue
		}
		name := target.Name
		if target.AliasOf != "" {
			target, _ = current.Field(target.AliasOf)
		}
		lifecycle := current.Lifecycle != nil && current.Lifecycle.Field == target.Name
		original, declared := old.Field(name)
		if target.ReadOnly && !lifecycle && (!declared || !original.ReadOnly) {
			return nil, nil, fmt.Errorf("retained action writes a retired field %s", target.Name)
		}
		result[target.Name] = input[name]
		fields = append(fields, target.Name)
	}
	return result, fields, nil
}

func ReadableFields(raw []byte, info *platform.EntityInfo, c platform.Caller) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	if info != nil {
		for name := range fields {
			field, declared := info.Field(name)
			if declared && field.AliasOf != "" {
				field, declared = info.Field(field.AliasOf)
			}
			if declared && !field.ReadsAny(c.RolesIn(info.App), c.Rules()) {
				delete(fields, name)
			}
		}
	}
	return json.Marshal(fields)
}

func RetainedCompatible(selected, current []platform.ReleaseAsset, native func(platform.ReleaseAsset) (platform.ReleaseAsset, bool)) error {
	for _, asset := range selected {
		a := slices.IndexFunc(current, func(x platform.ReleaseAsset) bool { return x.Ref == asset.Ref })
		before, _ := platform.CanonicalDigest(asset)
		var after string
		if a >= 0 {
			chosen := current[a]
			if asset.Ref.Kind == platform.AssetFlow {
				if candidate, ok := native(asset); ok {
					chosen = candidate
				}
			}
			after, _ = platform.CanonicalDigest(chosen)
		}
		if before != after {
			return fmt.Errorf("retained dependency %s has no compatible execution owner", asset.Ref)
		}
	}
	return nil
}

func RetainedDefinitions(definitions []platform.Definition, assets []platform.ReleaseAsset) ([]platform.Definition, error) {
	definitions = slices.Clone(definitions)
	for i, definition := range definitions {
		for _, asset := range assets {
			if asset.Ref != definition.Ref {
				continue
			}
			switch asset.Ref.Kind {
			case platform.AssetQuery:
				var query platform.NamedQuery
				if err := json.Unmarshal(asset.Body, &query); err != nil {
					return nil, err
				}
				definition.Query = &query
			case platform.AssetFunction:
				var function platform.AIFunction
				if err := json.Unmarshal(asset.Body, &function); err != nil {
					return nil, err
				}
				definition.Function = &function
			}
			definitions[i] = definition
		}
	}
	return definitions, nil
}

func AssetsOf(assets []platform.ReleaseAsset, app string) []platform.ReleaseAsset {
	selected := []platform.ReleaseAsset{}
	for _, asset := range assets {
		if asset.Ref.App == app {
			selected = append(selected, asset)
		}
	}
	return selected
}

func RetainedShapes(app string, entities []platform.Entity, selected []platform.ReleaseAsset) ([]platform.EntityInfo, error) {
	var out []platform.EntityInfo
	for _, entity := range entities {
		if !slices.ContainsFunc(selected, func(a platform.ReleaseAsset) bool {
			return a.Ref.Kind == platform.AssetObject && a.Ref.Name == entity.Type
		}) {
			continue
		}
		info, err := platform.Describe(app, entity, func(reflect.Type) string { return "" })
		if err != nil {
			return nil, err
		}
		out = append(out, info)
	}
	return out, nil
}

type retainedReadRefusal struct{ refusal *kernel.Error }

func RecoverRetained(fail func(*kernel.Error), refusal **kernel.Error) {
	if failure := recover(); failure != nil {
		saved, ok := failure.(retainedReadRefusal)
		if !ok {
			panic(failure)
		}
		fail(saved.refusal)
		if refusal != nil {
			*refusal = saved.refusal
		}
	}
}

type RetainedRows[R comparable] struct {
	List    func(after string, limit int) []string
	Fetch   func(id string) R
	Batch   func(ids []string) map[string]R
	Convert func(R) (R, *kernel.Error)
}

func (s RetainedRows[R]) IDs(after string, limit int) []string { return s.List(after, limit) }

func (s RetainedRows[R]) Get(id string) R {
	var none R
	row := s.Fetch(id)
	if row == none {
		return none
	}
	return s.convert(row)
}

func (s RetainedRows[R]) GetBatch(ids []string) map[string]R {
	var none R
	rows := s.Batch(ids)
	for id, row := range rows {
		if row != none {
			rows[id] = s.convert(row)
		}
	}
	return rows
}

func (s RetainedRows[R]) convert(row R) R {
	value, refusal := s.Convert(row)
	if refusal != nil {
		panic(retainedReadRefusal{refusal})
	}
	return value
}

func PlanRetainedPut(shape, old, current platform.EntityInfo, entity, prior any, stored json.RawMessage, hasStored bool) (result, input map[string]json.RawMessage, fields []string, err error) {
	raw, _ := json.Marshal(entity)
	if json.Unmarshal(raw, &input) != nil {
		return nil, nil, nil, fmt.Errorf("retained action wrote an invalid record")
	}
	var before, saved map[string]json.RawMessage
	if prior != nil {
		image, _ := json.Marshal(prior)
		_ = json.Unmarshal(image, &before)
	}
	if hasStored {
		_ = json.Unmarshal(stored, &saved)
	}
	result, fields, err = RetainedWrite(shape, old, current, before, input, saved, hasStored)
	return result, input, fields, err
}

func RetainedRecord(info platform.EntityInfo, result, input map[string]json.RawMessage) (any, error) {
	result["id"], result["archived"] = input["id"], input["archived"]
	decoded := reflect.New(info.Go)
	raw, _ := json.Marshal(result)
	if json.Unmarshal(raw, decoded.Interface()) != nil {
		return nil, fmt.Errorf("retained action result does not match current storage")
	}
	return decoded.Elem().Interface(), nil
}

func NativeFlowCandidate(flows []platform.Flow, asset platform.ReleaseAsset, load func(name string, version int) (platform.ReleaseAsset, error)) (platform.ReleaseAsset, bool) {
	for _, flow := range flows {
		candidate, err := load(flow.Name, flow.Version)
		if err == nil && candidate.SourceVersion == asset.SourceVersion && candidate.Ref == asset.Ref {
			return candidate, true
		}
	}
	return platform.ReleaseAsset{}, false
}

type RetainedPut struct {
	Shapes    map[string]platform.EntityInfo
	Release   string
	Current   *records.Store
	Authorize func(platform.AuthzRequest) *kernel.Error
	Check     func(any) *kernel.Error
	Put       func(any) *kernel.Error
}

func (p RetainedPut) Run(c platform.Caller, old *records.Type, entity any) *kernel.Error {
	if old == nil {
		return p.Put(entity)
	}
	if _, retained := p.Shapes[old.Info().Type]; !retained {
		return p.Put(entity)
	}
	current := p.Current.Type(old.Info().Type)
	id := records.Header(records.Copy(old.Info().Go, entity)).ID
	var priorValue any
	if row := old.Rows().Get(id); row != nil {
		priorValue = row.Value.Interface()
	}
	prior := current.Rows().Get(id)
	var stored json.RawMessage
	if prior != nil {
		stored, _ = prior.Image()
	}
	result, input, fields, writeErr := PlanRetainedPut(p.Shapes[old.Info().Type], old.Info(), current.Info(), entity, priorValue, stored, prior != nil)
	if writeErr != nil {
		return conflictOf(writeErr)
	}
	question := platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "write", Type: current.Info().Type, Record: id, Fields: fields}
	if prior == nil {
		question.Kind = "create"
	}
	if refusal := p.Authorize(question); refusal != nil {
		return refusal
	}
	decoded, decodeErr := RetainedRecord(current.Info(), result, input)
	if decodeErr != nil {
		return conflictOf(decodeErr)
	}
	if refusal := p.Check(decoded); refusal != nil {
		return refusal
	}
	if refusal := p.Put(decoded); refusal != nil {
		return refusal
	}
	updated := *current.Rows().Get(id)
	updated.Projection = records.CloneProjection(updated.Projection)
	if source := records.ProjectionFor(updated.Projection, p.Release); source != nil {
		source.Value, _ = json.Marshal(input)
		if err := records.RefreshProjection(updated.Projection, updated.Value.Interface()); err != nil {
			return conflictOf(err)
		}
		current.SetRow(id, &updated)
	}
	return nil
}

func conflictOf(err error) *kernel.Error {
	if refusal, ok := err.(*kernel.Error); ok {
		return refusal
	}
	return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
}

type RetainedOwners struct {
	Apps    []platform.App
	Assets  []platform.ReleaseAsset
	Current func() ([]platform.ReleaseAsset, error)
	Native  func(owner platform.App, name string, version int) (platform.ReleaseAsset, error)
	Claim   func(i int, key string)
	Bind    func(bound platform.App)
}

func (r RetainedOwners) Select() ([]platform.App, map[string]platform.EntityInfo, error) {
	apps, shapes := slices.Clone(r.Apps), map[string]platform.EntityInfo{}
	for i, owner := range apps {
		selected := AssetsOf(r.Assets, owner.Manifest().ID)
		if len(selected) == 0 {
			continue
		}
		compiler, ok := owner.(platform.ReleaseExecutionOwner)
		if !ok {
			current, err := r.Current()
			if err != nil {
				return nil, nil, err
			}
			err = RetainedCompatible(selected, current, func(asset platform.ReleaseAsset) (platform.ReleaseAsset, bool) {
				return NativeFlowCandidate(owner.Manifest().Flows, asset, func(name string, version int) (platform.ReleaseAsset, error) {
					return r.Native(owner, name, version)
				})
			})
			if err != nil {
				return nil, nil, err
			}
			continue
		}
		bound, err := compiler.ForkReleaseExecution(selected)
		if err != nil || bound == nil || bound.AcceptedLedger() != owner.AcceptedLedger() {
			return nil, nil, fmt.Errorf("retained owner %s cannot select its original execution: %v", owner.Manifest().ID, err)
		}
		apps[i] = bound
		for _, action := range bound.Manifest().Actions.All() {
			r.Claim(i, "action:"+action.Schema)
		}
		found, err := RetainedShapes(bound.Manifest().ID, bound.Manifest().Entities, selected)
		if err != nil {
			return nil, nil, err
		}
		for _, info := range found {
			shapes[info.Type] = info
			r.Claim(i, info.Type)
		}
		r.Bind(bound)
	}
	return apps, shapes, nil
}

func ReadableRecord(raw json.RawMessage, current *records.Type, c platform.Caller) (json.RawMessage, *kernel.Error) {
	var info *platform.EntityInfo
	if current != nil {
		stored := current.Info()
		info = &stored
	}
	out, err := ReadableFields(raw, info, c)
	if err != nil {
		return out, conflictOf(err)
	}
	return out, nil
}
