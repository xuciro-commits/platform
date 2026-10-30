package platform

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"slices"
	"strconv"
	"strings"
)

type ReleaseAsset struct {
	Ref             AssetRef        `json:"ref"`
	ContractVersion int             `json:"contractVersion"`
	SourceVersion   string          `json:"sourceVersion"`
	Requires        []AssetRef      `json:"requires"`
	Provides        []Capability    `json:"provides,omitempty"`
	Body            json.RawMessage `json:"body"`
}

type FlowReleaseDescriptor struct {
	Native       bool            `json:"native,omitempty"`
	Name         string          `json:"name"`
	Subject      AssetRef        `json:"subject"`
	Actions      []AssetRef      `json:"actions"`
	Definition   json.RawMessage `json:"definition"`
	Functions    []AssetBinding  `json:"functions,omitempty"`
	Operations   []AssetBinding  `json:"operations,omitempty"`
	Queries      []AssetBinding  `json:"queries,omitempty"`
	Dependencies []AssetRef      `json:"dependencies,omitempty"`
}

type AssetBinding struct {
	Ref           AssetRef `json:"ref"`
	SourceVersion string   `json:"sourceVersion"`
}

func PageReleaseAsset(app, sourceVersion string, page Page) (ReleaseAsset, error) {
	if err := page.CheckSafety(); err != nil {
		return ReleaseAsset{}, err
	}
	body, err := json.Marshal(page)
	if err != nil {
		return ReleaseAsset{}, err
	}
	return WithDependencies(ReleaseAsset{Ref: AssetRef{App: app, Kind: AssetPage, Name: page.Name},
		ContractVersion: 1, SourceVersion: sourceVersion, Body: body})
}

func ApplicationReleaseAsset(app, sourceVersion string, application Application) (ReleaseAsset, error) {
	if err := application.CheckVariables(); err != nil {
		return ReleaseAsset{}, err
	}
	if err := application.CheckResources(); err != nil {
		return ReleaseAsset{}, err
	}
	body, err := json.Marshal(application)
	if err != nil {
		return ReleaseAsset{}, err
	}
	return WithDependencies(ReleaseAsset{Ref: AssetRef{App: app, Kind: AssetApp, Name: application.Name},
		ContractVersion: 1, SourceVersion: sourceVersion, Body: body})
}

type ReleaseCandidate struct {
	ID     string
	Bytes  []byte
	Assets []ReleaseAsset
}

func (c ReleaseCandidate) Closure(roots []AssetRef) (ReleaseCandidate, error) {
	checked, err := ReadCandidate(c.ID, c.Bytes)
	if err != nil {
		return ReleaseCandidate{}, err
	}
	return candidate(roots, checked.Assets, true)
}

type releaseBytes struct {
	Format int            `json:"format"`
	Assets []ReleaseAsset `json:"assets"`
}

func Candidate(roots []AssetRef, available []ReleaseAsset) (ReleaseCandidate, error) {
	return candidate(roots, available, false)
}

func candidate(roots []AssetRef, available []ReleaseAsset, historical bool) (ReleaseCandidate, error) {
	var empty ReleaseCandidate
	if len(roots) == 0 {
		return empty, fmt.Errorf("a release needs at least one root")
	}
	available = slices.Clone(available)
	lookup := make(map[AssetRef]ReleaseAsset, len(available))
	for i, asset := range available {
		if !historical && asset.Provides == nil {
			description, err := DescribeAsset(asset)
			if err != nil {
				return empty, err
			}
			asset.Provides = description.Provides
			available[i] = asset
		}
		if err := asset.Ref.Check(); err != nil {
			return empty, err
		}
		if _, found := lookup[asset.Ref]; found {
			return empty, fmt.Errorf("asset %s is declared twice", asset.Ref)
		}
		lookup[asset.Ref] = asset
	}
	graph, err := NewReleaseDependencyGraph(available)
	if err != nil {
		return empty, err
	}
	refs, err := graph.Closure(roots)
	if err != nil {
		return empty, err
	}
	var closed []ReleaseAsset
	replacedViews := map[string]AssetRef{}
	for _, ref := range refs {
		asset := lookup[ref]
		if asset.SourceVersion == "" || asset.ContractVersion <= 0 {
			return empty, fmt.Errorf("release asset %s has no pinned source or contract", ref)
		}
		body, err := canonicalBody(asset.Body)
		if err != nil {
			return empty, fmt.Errorf("release asset %s: %w", ref, err)
		}
		if err := checkAssetDescription(asset); err != nil {
			return empty, err
		}
		if ref.Kind == AssetObservation {
			var observation ObservationDefinition
			if err := json.Unmarshal(body, &observation); err != nil {
				return empty, err
			}
			if err := CheckObservationDefinition(observation, func(typ string) (EntityInfo, bool) {
				target, exists := lookup[observation.Object]
				if typ != observation.Object.Name || !exists {
					return EntityInfo{}, false
				}
				info, err := queryObjectDescriptor(target.Body)
				return info, err == nil
			}); err != nil {
				return empty, fmt.Errorf("observation %s: %w", ref, err)
			}
			if observation.Decision != nil {
				actionAsset, exists := lookup[observation.Decision.Action]
				action, err := releaseActionDescriptor(actionAsset.Body)
				if !exists || err != nil {
					return empty, fmt.Errorf("observation %s action is unavailable", ref)
				}
				if err := CheckObservationAction(observation, action); err != nil {
					return empty, fmt.Errorf("observation %s: %w", ref, err)
				}
			}
		}
		if ref.Kind == AssetAlert {
			var alert AlertRule
			if json.Unmarshal(body, &alert) != nil || alert.Check() != nil {
				return empty, fmt.Errorf("invalid alert rule")
			}
			target, exists := lookup[alert.Object]
			info, err := queryObjectDescriptor(target.Body)
			if !exists || err != nil {
				return empty, fmt.Errorf("alert %s object is unavailable", ref)
			}
			if _, exists := info.Field(alert.Field); !exists {
				return empty, fmt.Errorf("alert %s field %s is unavailable", ref, alert.Field)
			}
		}
		if ref.Kind == AssetLinkType {
			var l LinkType
			if json.Unmarshal(body, &l) != nil || asset.ContractVersion != l.Contract() {
				return empty, fmt.Errorf("link asset has an incompatible cardinality contract")
			}
		}
		if ref.Kind == AssetObject {
			info, err := queryObjectDescriptor(body)
			if err != nil {
				return empty, err
			}
			if source := info.ExtensionSource; source != nil {
				base, exists := lookup[source.Base.Ref]
				shape, err := queryObjectDescriptor(base.Body)
				if !exists || err != nil || !slices.Contains(asset.Requires, source.Base.Ref) {
					return empty, fmt.Errorf("extension %s needs its base object in the frozen closure", ref)
				}
				if err := source.Check(info.Extends); err != nil {
					return empty, err
				}
				if shape.App != source.Base.Ref.App {
					return empty, fmt.Errorf("extension base authority differs from its frozen closure")
				}
			}
			for _, f := range info.Fields {
				if f.Property == nil {
					continue
				}
				source, ok := lookup[f.Property.Ref]
				var p PropertyType
				if !ok || source.SourceVersion != f.Property.SourceVersion || json.Unmarshal(source.Body, &p) != nil || p.CheckField(f) != nil {
					return empty, fmt.Errorf("frozen object field has an unavailable or incompatible property")
				}
			}
			for _, f := range info.Fields {
				if f.ValueType == nil {
					continue
				}
				source, ok := lookup[f.ValueType.Ref]
				var v ValueType
				if !ok || source.SourceVersion != f.ValueType.SourceVersion || json.Unmarshal(source.Body, &v) != nil || v.CheckField(f) != nil {
					return empty, fmt.Errorf("frozen object field has an unavailable or incompatible value type")
				}
			}
		}
		if ref.Kind == AssetEvaluation {
			evaluation, err := ReadEvaluationSet(body)
			if err != nil {
				return empty, err
			}
			function, ok := lookup[evaluation.Function.Ref]
			var definition AIFunction
			if !ok || function.SourceVersion != evaluation.Function.SourceVersion || json.Unmarshal(function.Body, &definition) != nil || definition.Check() != nil {
				return empty, fmt.Errorf("evaluation set needs its pinned function")
			}
			for _, test := range evaluation.Policy.Cases {
				if !definition.EvaluationInput(test.Input) || definition.ValidateOutput(test.Expected) != nil {
					return empty, fmt.Errorf("evaluation cases differ from the pinned function")
				}
			}
		}
		if ref.Kind == AssetApp {
			var application Application
			if err := json.Unmarshal(body, &application); err != nil {
				return empty, err
			}
			if err := application.CheckVariables(); err != nil {
				return empty, err
			}
			if err := application.CheckExtension(); err != nil {
				return empty, err
			}
			if application.Extends != nil {
				entry := application.Entry
				if entry == "" {
					entry = application.Extends.App
				}
				for _, replacement := range application.Replacements {
					key := entry + "/" + replacement.View
					if previous, exists := replacedViews[key]; exists {
						return empty, fmt.Errorf("application view %s/%s is replaced by both %s and %s", entry, replacement.View, previous, ref)
					}
					replacedViews[key] = ref
				}
			}
			contracts := make([]ReleaseAsset, 0, len(lookup))
			for _, asset := range lookup {
				contracts = append(contracts, asset)
			}
			if err := checkApplicationProfileClosure(ref, contracts); err != nil {
				return empty, err
			}
			if err := checkFrozenQueries(application.QueryPage(), lookup); err != nil {
				return empty, err
			}
			for _, name := range application.Pages {
				pageRef := AssetRef{App: ref.App, Kind: AssetPage, Name: name}
				target, ok := lookup[pageRef]
				var page Page
				if !ok || json.Unmarshal(target.Body, &page) != nil {
					return empty, fmt.Errorf("application page %s is unavailable", pageRef)
				}
				if err := application.CheckPageVariables(page); err != nil {
					return empty, err
				}
			}
		}
		if ref.Kind == AssetFlow || ref.Kind == AssetPage {
			var bindings []AssetBinding
			if ref.Kind == AssetFlow {
				var flow FlowReleaseDescriptor
				if err := json.Unmarshal(body, &flow); err != nil {
					return empty, err
				}
				bindings = append(slices.Clone(flow.Functions), flow.Operations...)
				bindings = append(bindings, flow.Queries...)
			} else {
				var page Page
				if err := json.Unmarshal(body, &page); err != nil {
					return empty, err
				}
				if page.Document != nil {
					for _, event := range page.Document.Events {
						for _, effect := range event.Effects {
							if effect.Kind == "action" && effect.Action != nil {
								asset, ok := lookup[effect.Action.Ref]
								action, err := releaseActionDescriptor(asset.Body)
								if !ok || err != nil || action.New == (effect.Action.RecordVariable != "") {
									return empty, fmt.Errorf("page action effect %s is unavailable", effect.Action.Ref)
								}
								for parameter := range effect.Action.Inputs {
									if !slices.ContainsFunc(action.Payload, func(f Field) bool { return f.Name == parameter }) {
										return empty, fmt.Errorf("page action effect %s has no parameter %s", effect.Action.Ref, parameter)
									}
								}
							}
						}
						if n := event.Navigation(); n != nil {
							target, ok := lookup[n.Page]
							var targetPage Page
							if !ok || json.Unmarshal(target.Body, &targetPage) != nil {
								return empty, fmt.Errorf("page navigation target %s is unavailable", n.Page)
							}
							if err := CheckPageNavigation(page, targetPage, *n); err != nil {
								return empty, err
							}
						}
					}
				}
				if err := CheckPageEmbeddingGraph(ref, lookup); err != nil {
					return empty, err
				}
				if err := checkFrozenQueries(page, lookup); err != nil {
					return empty, err
				}

				for _, c := range page.ComputeResources() {
					bindings = append(bindings, c.Operation)
				}
				safe, err := page.Safety()
				if err != nil {
					return empty, err
				}
				for _, trigger := range safe.ActionTriggers {
					if trigger.Function != nil {
						bindings = append(bindings, *trigger.Function)
					}
					if trigger.Operation != nil {
						bindings = append(bindings, *trigger.Operation)
					}
				}

			}
			seen := map[AssetRef]bool{}
			for _, binding := range bindings {
				dependency, ok := lookup[binding.Ref]
				if !ok || seen[binding.Ref] && ref.Kind == AssetFlow || binding.Ref.Kind != AssetFunction && binding.Ref.Kind != AssetCompute && !(ref.Kind == AssetFlow && binding.Ref.Kind == AssetQuery) || binding.SourceVersion == "" || dependency.SourceVersion != binding.SourceVersion {
					return empty, fmt.Errorf("%s needs exact function dependency %s at %s", ref, binding.Ref, binding.SourceVersion)
				}
				seen[binding.Ref] = true
			}
		}
		if asset.Provides != nil {
			if err := graph.CheckExecutionBindings(asset, lookup); err != nil {
				return empty, err
			}
			if err := graph.CheckReadBindings(asset); err != nil {
				return empty, err
			}
		}
		asset.Requires = graph.Requires(ref)
		if asset.Requires == nil {
			asset.Requires = []AssetRef{}
		}
		asset.Body = body
		closed = append(closed, asset)
	}
	slices.SortFunc(closed, func(a, b ReleaseAsset) int { return compareRef(a.Ref, b.Ref) })
	raw, err := json.Marshal(releaseBytes{Format: 1, Assets: closed})
	if err != nil {
		return empty, fmt.Errorf("encode release candidate: %w", err)
	}
	hash := sha256.Sum256(raw)
	return ReleaseCandidate{ID: "sha256-v1:" + hex.EncodeToString(hash[:]), Bytes: raw, Assets: closed}, nil
}

func ReadCandidate(id string, raw []byte) (ReleaseCandidate, error) {
	var wire releaseBytes
	if len(raw) == 0 || len(raw) > 16<<20 {
		return ReleaseCandidate{}, fmt.Errorf("release candidate is empty or exceeds 16 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return ReleaseCandidate{}, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF || wire.Format != 1 {
		return ReleaseCandidate{}, fmt.Errorf("invalid release format or trailing data")
	}
	roots := make([]AssetRef, len(wire.Assets))
	for i, asset := range wire.Assets {
		roots[i] = asset.Ref
	}
	checked, err := candidate(roots, wire.Assets, true)
	if err != nil {
		return ReleaseCandidate{}, err
	}
	if checked.ID != id || !bytes.Equal(checked.Bytes, raw) {
		return ReleaseCandidate{}, fmt.Errorf("release candidate identity or canonical bytes differ")
	}
	return checked, nil
}

func canonicalBody(raw []byte) ([]byte, error) {
	if len(raw) == 0 || len(raw) > 1<<20 {
		return nil, fmt.Errorf("descriptor is empty or exceeds 1 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	value, err := readJSONValue(decoder)
	if err != nil {
		return nil, fmt.Errorf("invalid descriptor JSON: %w", err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("descriptor JSON has trailing content")
	}
	if _, ok := value.(map[string]any); !ok {
		return nil, fmt.Errorf("descriptor must be a JSON object")
	}
	return json.Marshal(value)
}

func readJSONValue(d *json.Decoder) (any, error) {
	token, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch token {
	case json.Delim('{'):
		object := map[string]any{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return nil, err
			}
			name := key.(string)
			if _, exists := object[name]; exists {
				return nil, fmt.Errorf("duplicate JSON key %q", name)
			}
			object[name], err = readJSONValue(d)
			if err != nil {
				return nil, err
			}
		}
		_, err := d.Token()
		return object, err
	case json.Delim('['):
		array := []any{}
		for d.More() {
			item, err := readJSONValue(d)
			if err != nil {
				return nil, err
			}
			array = append(array, item)
		}
		_, err := d.Token()
		return array, err
	default:
		if number, ok := token.(json.Number); ok {
			return canonicalNumber(number)
		}
		return token, nil
	}
}

func canonicalNumber(number json.Number) (json.Number, error) {
	text := string(number)
	if len(text) > 4096 {
		return "", fmt.Errorf("descriptor number exceeds canonical range")
	}
	if i := strings.IndexAny(text, "eE"); i >= 0 {
		exponent, err := strconv.Atoi(text[i+1:])
		if err != nil || exponent < -1024 || exponent > 1024 {
			return "", fmt.Errorf("descriptor exponent exceeds canonical range")
		}
	}
	value, ok := new(big.Rat).SetString(text)
	if !ok {
		return "", fmt.Errorf("invalid descriptor number %q", text)
	}
	if value.IsInt() {
		return json.Number(value.Num().String()), nil
	}
	denominator := new(big.Int).Set(value.Denom())
	two, five := big.NewInt(2), big.NewInt(5)
	quotient, remainder := new(big.Int), new(big.Int)
	precision := 0
	for _, factor := range []*big.Int{two, five} {
		count := 0
		for {
			quotient.QuoRem(denominator, factor, remainder)
			if remainder.Sign() != 0 {
				break
			}
			denominator.Set(quotient)
			count++
		}
		if count > precision {
			precision = count
		}
	}
	if denominator.Cmp(big.NewInt(1)) != 0 || precision > 1024 {
		return "", fmt.Errorf("descriptor number exceeds canonical range")
	}
	decimal := strings.TrimRight(value.FloatString(precision), "0")
	return json.Number(strings.TrimSuffix(decimal, ".")), nil
}

func compareRef(a, b AssetRef) int {
	if a.App != b.App {
		return compareString(a.App, b.App)
	}
	if a.Kind != b.Kind {
		return compareString(string(a.Kind), string(b.Kind))
	}
	return compareString(a.Name, b.Name)
}

func compareString(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func CandidateDiff(before, after ReleaseCandidate) (added, removed, changed []AssetRef, err error) {
	before, err = ReadCandidate(before.ID, before.Bytes)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("previous candidate: %w", err)
	}
	after, err = ReadCandidate(after.ID, after.Bytes)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("next candidate: %w", err)
	}
	prior := map[AssetRef][]byte{}
	for _, asset := range before.Assets {
		raw, _ := json.Marshal(asset)
		prior[asset.Ref] = raw
	}
	for _, asset := range after.Assets {
		raw, _ := json.Marshal(asset)
		old, found := prior[asset.Ref]
		if !found {
			added = append(added, asset.Ref)
		} else if !bytes.Equal(old, raw) {
			changed = append(changed, asset.Ref)
		}
		delete(prior, asset.Ref)
	}
	for ref := range prior {
		removed = append(removed, ref)
	}
	slices.SortFunc(added, compareRef)
	slices.SortFunc(removed, compareRef)
	slices.SortFunc(changed, compareRef)
	return added, removed, changed, nil
}

func checkFrozenQueries(original Page, assets map[AssetRef]ReleaseAsset) error {
	page, err := original.Safety()
	if err != nil {
		return err
	}
	lookup := func(typ string) (EntityInfo, bool) {
		asset, ok := assets[ModelObject(typ)]
		if !ok {
			for ref, value := range assets {
				if ref.Kind == AssetObject && ref.Name == typ {
					asset = value
					ok = true
					break
				}
			}
		}
		info, err := queryObjectDescriptor(asset.Body)
		return ConstraintEntity(info), ok && err == nil
	}
	criteria := func(ref AssetRef) (*RuleCriteria, bool) {
		asset, ok := assets[ref]
		action, err := releaseActionDescriptor(asset.Body)
		if !ok || err != nil {
			return nil, false
		}
		reads := RuleReadSpec{Object: action.Target, Inputs: map[string]ExprType{}}
		mapped := map[string]bool{}
		for _, binding := range page.DataBindings {
			for _, model := range binding.ModelBindings() {
				if model.Options != nil && model.Options.Rule != nil && model.Options.Rule.Action == ref {
					for name := range model.Options.Rule.Inputs {
						mapped[name] = true
					}
				}
			}
		}
		for _, field := range action.Payload {
			if mapped[field.Name] {
				reads.Inputs[field.Name] = ExprType{Kind: RuleFieldKind(field.Type), Object: field.Ref}
			}
		}
		return &RuleCriteria{Reads: reads}, true
	}
	for _, binding := range page.DataBindings {
		if err := page.CheckDataBinding(binding, lookup, criteria); err != nil {
			return err
		}
		for _, model := range binding.ModelBindings() {
			sets := []*ModelRecordSet{model.Set}
			if model.Options != nil {
				sets = append(sets, model.Options.Set)
			}
			for _, set := range sets {
				if set == nil || set.Query == nil {
					continue
				}
				asset, ok := assets[set.Query.Ref]
				var query NamedQuery
				if !ok || asset.SourceVersion != set.Query.SourceVersion || json.Unmarshal(asset.Body, &query) != nil || query.Object != model.Type {
					return fmt.Errorf("binding %s frozen model source differs", binding.ID)
				}
			}
		}
		for _, resource := range binding.Resources {
			if resource.Asset == nil {
				continue
			}
			asset, ok := assets[resource.Asset.Ref]
			if !ok || resource.Asset.SourceVersion != "" && asset.SourceVersion != resource.Asset.SourceVersion {
				return fmt.Errorf("binding %s exact resource is unavailable", binding.ID)
			}
		}
	}
	for _, trigger := range page.ActionTriggers {
		object := page.Object.Name
		for _, binding := range page.DataBindings {
			if binding.ID == trigger.Binding {
				object = binding.ObjectRef(page).Name
			}
		}
		for _, ref := range trigger.Actions {
			asset, ok := assets[ref]
			action, err := releaseActionDescriptor(asset.Body)
			if !ok || err != nil || StaticControlAction(action) || action.Target != object {
				return fmt.Errorf("page action owner or target is unavailable")
			}
		}
		if trigger.InlineEdit != nil {
			asset, ok := assets[trigger.InlineEdit.Action]
			action, err := releaseActionDescriptor(asset.Body)
			info, found := lookup(object)
			if !ok || !found || err != nil || (Section{InlineEdit: trigger.InlineEdit}).CheckTableEdit(info, action) != nil {
				return fmt.Errorf("page edit fields differ from original action")
			}
		}
		if len(trigger.Defaults) > 0 {
			if len(trigger.Actions) != 1 {
				return fmt.Errorf("page parameters need one original action")
			}
			action, err := releaseActionDescriptor(assets[trigger.Actions[0]].Body)
			if err != nil {
				return err
			}
			info, _ := lookup(object)
			for _, mapping := range trigger.Defaults {
				field, ok := info.Field(mapping.Field)
				index := slices.IndexFunc(action.Payload, func(f Field) bool { return f.Name == mapping.Parameter })
				if !ok || index < 0 || !ActionParameterCompatible(field, action.Payload[index]) {
					return fmt.Errorf("page action parameter differs from original readable field")
				}
			}
		}
		if trigger.Function != nil {
			asset, ok := assets[trigger.Function.Ref]
			var function AIFunction
			if !ok || asset.SourceVersion != trigger.Function.SourceVersion || json.Unmarshal(asset.Body, &function) != nil || function.Object != object {
				return fmt.Errorf("page function original source is unavailable")
			}
		}
		if trigger.Operation != nil {
			asset, ok := assets[trigger.Operation.Ref]
			var operation Operation
			if !ok || asset.SourceVersion != trigger.Operation.SourceVersion || json.Unmarshal(asset.Body, &operation) != nil || operation.Check() != nil {
				return fmt.Errorf("page operation original source is unavailable")
			}
		}
	}
	if err := page.InputSchema.CheckVariables(); err != nil {
		return err
	}
	if err := page.InputSchema.CheckQuerySets(); err != nil {
		return err
	}
	for id, compute := range page.ComputeResources() {
		asset, ok := assets[compute.Operation.Ref]
		var op Operation
		object, found := lookup(page.RecordResourceObject(compute.RecordVariable).Name)
		if !ok || !found || asset.SourceVersion != compute.Operation.SourceVersion || json.Unmarshal(asset.Body, &op) != nil || page.CheckComputeResource(compute, object, op, page.InputSchema.Variables[id].Type, lookup) != nil {
			return fmt.Errorf("frozen compute resource contract is unavailable")
		}
	}
	for _, v := range page.InputSchema.Variables {
		if v.Source == nil {
			continue
		}
		if v.Mode == "property" && v.Source.Object != nil {
			info, ok := lookup(v.Source.Object.Name)
			if !ok || page.CheckPropertySchema(v, info) != nil {
				return fmt.Errorf("frozen property schema is unavailable")
			}
		}
		if v.Scope == "application" && v.Mode == "resource" && v.Type == "filter" && v.Source.Object != nil {
			info, ok := lookup(v.Source.Object.Name)
			if !ok || CheckFilterSchema(*v.Source, info) != nil {
				return fmt.Errorf("frozen application filter schema is unavailable")
			}
		}
	}
	namedSources := map[AssetBinding]NamedQuery{}
	for id, plan := range page.InputSchema.Queries {
		if plan.Interface != "" {
			named, err := frozenPageInterfaceQuery(plan, assets)
			if err != nil {
				return err
			}
			if err := page.CheckInterfaceQuery(plan, named); err != nil {
				return err
			}
			continue
		}
		asset, ok := assets[plan.Object]
		object, err := queryObjectDescriptor(asset.Body)
		if !ok || err != nil {
			return fmt.Errorf("page query %s object is unavailable", id)
		}
		var named *Definition
		if plan.Query != nil {
			asset, ok := assets[plan.Query.Ref]
			if !ok || asset.SourceVersion != plan.Query.SourceVersion {
				return fmt.Errorf("page query %s exact source is unavailable", id)
			}
			if plan.Query.Ref.Kind == AssetLinkType {
				var link LinkType
				if json.Unmarshal(asset.Body, &link) != nil {
					return fmt.Errorf("frozen link source is invalid")
				}
				parentAsset, pok := assets[link.Parent]
				childAsset, cok := assets[link.Child]
				parent, pe := queryObjectDescriptor(parentAsset.Body)
				child, ce := queryObjectDescriptor(childAsset.Body)
				parent.App = link.Parent.App
				child.App = link.Child.App
				if !pok || !cok || pe != nil || ce != nil || link.CheckSchema(parent, child) != nil {
					return fmt.Errorf("frozen link endpoints are unavailable")
				}
				named = &Definition{Ref: asset.Ref, Version: asset.SourceVersion, LinkType: &link}
			} else {
				var query NamedQuery
				if json.Unmarshal(asset.Body, &query) != nil {
					return fmt.Errorf("frozen named query is invalid")
				}
				named = &Definition{Ref: asset.Ref, Version: asset.SourceVersion, Query: &query}
				namedSources[*plan.Query] = query
			}
		}
		for _, v := range page.InputSchema.Variables {
			if v.Source != nil && v.Source.Query == id {
				if err := page.InputSchema.CheckAggregateScalar(v, object); err != nil {
					return err
				}
			}
		}
		if err := page.CheckBoundQuery(id, named); err != nil {
			return err
		}
		if err := page.CheckQuerySchema(plan, object, named); err != nil {
			return err
		}
	}
	return page.CheckQuerySetConditions(namedSources)
}

func queryObjectDescriptor(body []byte) (EntityInfo, error) {
	var shape struct {
		Type            string                       `json:"type"`
		Implements      []string                     `json:"implements"`
		Extends         string                       `json:"extends"`
		ExtensionSource *ExtensionSource             `json:"extensionSource"`
		Entity          *EntityInfo                  `json:"entity"`
		Fields          []map[string]json.RawMessage `json:"fields"`
		Lifecycle       *LifecycleInfo               `json:"lifecycle"`
		States          []State                      `json:"states"`
		Actions         []struct {
			Transaction *BusinessTxn `json:"transaction"`
			Name        string       `json:"name"`
			Title       string       `json:"title"`
			From        []string     `json:"from"`
			To          string       `json:"to"`
			ToInput     string       `json:"toInput"`
			Inputs      []struct {
				Name     string `json:"name"`
				Type     string `json:"type"`
				Required bool   `json:"required"`
				Choices  string `json:"choices"`
			} `json:"inputs"`
		} `json:"actions"`
	}
	if err := json.Unmarshal(body, &shape); err != nil {
		return EntityInfo{}, err
	}
	if shape.Entity != nil {
		if shape.Entity.Type != shape.Type {
			return EntityInfo{}, fmt.Errorf("frozen native object identity differs")
		}
		return *shape.Entity, nil
	}
	var fields []FieldInfo
	for _, raw := range shape.Fields {
		if choicesRaw, ok := raw["choices"]; ok {
			var source string
			if json.Unmarshal(choicesRaw, &source) == nil {
				options := []string{}
				for _, part := range strings.Split(source, ",") {
					if value := strings.TrimSpace(part); value != "" {
						options = append(options, value)
					}
				}
				raw["choices"], _ = json.Marshal(options)
			}
		}
		if whenRaw, ok := raw["when"]; ok {
			var source string
			if json.Unmarshal(whenRaw, &source) == nil {
				condition, err := ParseWhen(source)
				if err != nil {
					return EntityInfo{}, err
				}
				if condition == nil {
					delete(raw, "when")
				} else {
					raw["when"], _ = json.Marshal(condition)
				}
			}
		}
		encoded, err := json.Marshal(raw)
		if err != nil {
			return EntityInfo{}, err
		}
		var field FieldInfo
		if err := json.Unmarshal(encoded, &field); err != nil {
			return EntityInfo{}, err
		}
		fields = append(fields, field)
	}
	owner, _, _ := strings.Cut(shape.Type, ".")
	if shape.Lifecycle == nil && len(shape.States) > 0 {
		l := LifecycleInfo{Field: "state", Initial: shape.States[0].Name, States: shape.States}
		choices := []string{}
		for _, s := range shape.States {
			choices = append(choices, s.Name)
		}
		fields = append(fields, FieldInfo{Name: "state", Title: "State", Type: "choice", ReadOnly: true, Choices: choices})
		for _, a := range shape.Actions {
			inputs := make([]Field, 0, len(a.Inputs))
			for _, input := range a.Inputs {
				kind := input.Type
				if kind == "choice" {
					kind = "string"
				}
				var options []string
				for _, option := range strings.Split(input.Choices, ",") {
					if value := strings.TrimSpace(option); value != "" {
						options = append(options, value)
					}
				}
				inputs = append(inputs, Field{Name: input.Name, Type: kind, Required: input.Required, Choices: options})
			}
			if to, input := transactionDestination(shape.Type, "state", a.Transaction, inputs); len(to) > 0 {
				if slices.ContainsFunc(to, func(state string) bool { return !slices.Contains(choices, state) }) {
					return EntityInfo{}, fmt.Errorf("frozen object transaction destination differs")
				}
				l.Transitions = append(l.Transitions, TransitionInfo{Name: a.Name, Schema: shape.Type + "." + a.Name, Title: a.Title, From: a.From, To: to, ToInput: input})
				continue
			}
			to := []string{}
			if a.To != "" {
				to = append(to, a.To)
			}
			if a.ToInput != "" {
				to = nil
				valid := false
				for _, input := range a.Inputs {
					if input.Name == a.ToInput && input.Type == "choice" && input.Required {
						valid = true
						for _, raw := range strings.Split(input.Choices, ",") {
							state := strings.TrimSpace(raw)
							if state != "" {
								to = append(to, state)
							}
						}
					}
				}
				if !valid || a.To != "" || len(to) == 0 || slices.ContainsFunc(to, func(state string) bool { return !slices.Contains(choices, state) }) {
					return EntityInfo{}, fmt.Errorf("frozen object destination input differs")
				}
			} else if a.To == "" {
				to = slices.Clone(a.From)
			}
			l.Transitions = append(l.Transitions, TransitionInfo{Name: a.Name, Schema: shape.Type + "." + a.Name, Title: a.Title, From: a.From, To: to, ToInput: a.ToInput})
		}
		shape.Lifecycle = &l
	}
	return EntityInfo{App: owner, Type: shape.Type, Standard: []string{shape.Type + ".create", shape.Type + ".edit", shape.Type + ".archive"}, Fields: fields, Lifecycle: shape.Lifecycle, Implements: shape.Implements, Extends: shape.Extends, ExtensionSource: shape.ExtensionSource}, nil
}

func releaseActionDescriptor(body json.RawMessage) (Action, error) {
	var flat Action
	if err := json.Unmarshal(body, &flat); err != nil {
		return Action{}, err
	}
	var envelope struct {
		Action *Action `json:"action"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return Action{}, err
	}
	if envelope.Action != nil {
		if envelope.Action.Schema != flat.Schema || envelope.Action.Target != flat.Target {
			return Action{}, fmt.Errorf("frozen action declaration identity differs")
		}
		return *envelope.Action, nil
	}
	return flat, nil
}
