package build

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	dataowner "platformserver/services/data"
	ontology "platformserver/services/ontology"
)

func (b *Build) transactionSequences() []platform.Sequence {
	byName := map[string]platform.Sequence{}
	for _, typ := range sortedTypes(b.installed) {
		if txn := b.installed[typ].CreateTxn; txn != nil {
			platform.WalkBusinessTxn(txn, func(step platform.TxnStep) {
				if step.Sequence != nil {
					byName[step.Sequence.Name] = *step.Sequence
				}
			})
		}
		if life := b.installed[typ].Lifecycle; life != nil {
			for _, transition := range life.Transitions {
				if transition.Txn != nil {
					platform.WalkBusinessTxn(transition.Txn, func(step platform.TxnStep) {
						if step.Sequence != nil {
							byName[step.Sequence.Name] = *step.Sequence
						}
					})
				}
			}
		}
	}
	names := slices.Sorted(maps.Keys(byName))
	out := make([]platform.Sequence, 0, len(names))
	for _, name := range names {
		out = append(out, byName[name])
	}
	return out
}

func txnUsesLegacyRows(steps []platform.TxnStep) bool {
	needed := false
	platform.WalkTxnSteps(steps, func(step platform.TxnStep) {
		sets := step.Changes
		if step.Post != nil {
			sets = append(slices.Clone(sets), step.Post.Match...)
		}
		if step.Effect != nil {
			sets = append(slices.Clone(sets), step.Effect.Payload...)
		}
		if step.Call != nil {
			sets = append(slices.Clone(sets), step.Call.Inputs...)
		}
		for _, set := range sets {
			needed = needed || strings.HasPrefix(set.From, "row.")
		}
	})
	return needed
}

func (b *Build) normalizeTransaction(object Object, action *Action, lookup func(string) (platform.EntityInfo, bool)) error {
	var lower func([]platform.TxnStep, platform.RuleReadSpec) error
	lower = func(steps []platform.TxnStep, scope platform.RuleReadSpec) error {
		scope.Inputs = maps.Clone(scope.Inputs)
		for i := range steps {
			step := &steps[i]
			if step.Call != nil && step.Call.View == "" {
				step.Call.View = "snapshot"
			}
			if step.Post != nil {
				for j, match := range step.Post.Match {
					if match.Expr != nil && match.From == "" {
						continue
					}
					info, ok := lookup(step.Target.Type)
					if !ok {
						return fmt.Errorf("Transaction balance object is unavailable")
					}
					field, ok := info.Field(match.Field)
					if !ok {
						return fmt.Errorf("Post match field is unavailable")
					}
					expr, err := platform.AssignmentExpr(field.Type, match, scope)
					if err != nil {
						return err
					}
					step.Post.Match[j] = platform.TxnSet{Field: match.Field, Expr: &expr}
				}
			}
			for _, parameters := range []*[]platform.TxnSet{func() *[]platform.TxnSet {
				if step.Effect != nil {
					return &step.Effect.Payload
				}
				return nil
			}(), func() *[]platform.TxnSet {
				if step.Call != nil {
					return &step.Call.Inputs
				}
				return nil
			}()} {
				if parameters != nil {
					for j, parameter := range *parameters {
						expr, err := platform.DispatchParameter(parameter, scope)
						if err != nil {
							return err
						}
						(*parameters)[j] = platform.TxnSet{Field: parameter.Field, Expr: &expr}
					}
				}
			}
			for j := range step.Refusals {
				if record := step.Refusals[j].Record; record != nil {
					if err := lower([]platform.TxnStep{*record}, scope); err != nil {
						return err
					}
				}
			}
			if step.ForEach != nil {
				if step.ForEach.Range != nil {
					if err := lower(step.ForEach.Steps, platform.WithTxnRange(scope, step.ForEach.Range.From.Kind)); err != nil {
						return err
					}
					continue
				}
				if !txnUsesLegacyRows(step.ForEach.Steps) {
					if err := lower(step.ForEach.Steps, scope); err != nil {
						return err
					}
					continue
				}
				source, err := platform.TxnIterationSource(ID, TypeOf(object.Name), step.ForEach.Of, lookup)
				if err != nil {
					return err
				}
				index := slices.IndexFunc(source.Sets, func(set platform.RecordSet) bool { return set.Name == step.ForEach.Set })
				if index < 0 {
					return fmt.Errorf("iteration set %s is undeclared", step.ForEach.Set)
				}
				info, ok := lookup(source.Sets[index].Object)
				if !ok {
					return fmt.Errorf("iteration object is unavailable")
				}
				if err := lower(step.ForEach.Steps, platform.WithTxnRow(scope, info)); err != nil {
					return err
				}
				continue
			}
			if len(step.Changes) > 0 {
				if !strings.HasPrefix(step.Target.Type, ID+".") {
					return fmt.Errorf("transaction cannot change another application")
				}
				for i := range step.Changes {
					assignment := &step.Changes[i]
					if assignment.Expr != nil && assignment.From == "" {
						continue
					}
					info, ok := lookup(step.Target.Type)
					if !ok {
						return fmt.Errorf("transaction object is unavailable")
					}
					field, ok := info.Field(assignment.Field)
					if !ok {
						return fmt.Errorf("transaction field is unavailable")
					}
					expr, err := platform.AssignmentExpr(field.Type, *assignment, scope)
					if err != nil {
						return err
					}
					*assignment = platform.TxnSet{Field: assignment.Field, Expr: &expr}
				}
			}
			platform.TxnResult(&scope, *step)
		}
		return nil
	}
	scope := ruleSpec(object, *action, action.Transaction.Reads)
	for i := range action.Transaction.Refusals {
		if record := action.Transaction.Refusals[i].Record; record != nil {
			if err := lower([]platform.TxnStep{*record}, scope); err != nil {
				return err
			}
		}
	}
	if dispatch := action.Transaction.Dispatch; dispatch != nil {
		for i, parameter := range dispatch.Payload {
			expr, err := platform.DispatchParameter(parameter, scope)
			if err != nil {
				return err
			}
			dispatch.Payload[i] = platform.TxnSet{Field: parameter.Field, Expr: &expr}
		}
	}
	return lower(action.Transaction.Steps, scope)
}

func (b *Build) transactionLink(binding platform.AssetBinding) (platform.LinkType, error) {
	ordinal, ok := strings.CutPrefix(binding.SourceVersion, b.Manifest().Version+".link-")
	version, err := strconv.Atoi(ordinal)
	if !ok || err != nil || version < 1 || strconv.Itoa(version) != ordinal || binding.Ref.App != ID || binding.Ref.Kind != platform.AssetLinkType {
		return platform.LinkType{}, fmt.Errorf("transaction needs its owner's retained relationship")
	}
	if asset, selected := b.jointDeclarations[binding.Ref]; selected && asset.SourceVersion == binding.SourceVersion {
		var link platform.LinkType
		if b.definitionAssetVersion(asset, platform.AssetLinkType, "link") != version || json.Unmarshal(asset.Body, &link) != nil || link.Name != binding.Ref.Name || link.Check() != nil || asset.ContractVersion != link.Contract() {
			return platform.LinkType{}, fmt.Errorf("transaction relationship candidate is invalid")
		}
		return link, nil
	}
	if declaration, retained, found := linkKind.definition(b, binding.Ref.Name, version); found && retained == version {
		return declaration, nil
	}
	return platform.LinkType{}, fmt.Errorf("transaction relationship %s version %s is unavailable", binding.Ref.Name, binding.SourceVersion)
}

func (b *Build) checkTransactions(object Object, engines ...platform.RuleEngine) error {
	return ontology.CheckTransactions(object, ontology.TransactionContext{
		App:         ID,
		BuilderRole: Builder,
		Lookup:      b.conditionLookup,
		ObjectInfo: func() (platform.EntityInfo, error) {
			return platform.Describe(ID, Entity(object), func(reflect.Type) string { return "" })
		},
		Saved: func(typ string) bool {
			_, installed := b.installed[typ]
			_, joint := b.jointEntities[typ]
			return installed || joint
		},
		Sequences: b.transactionSequences,
		Link:      b.transactionLink,
		Confirm:   b.checkConfirmation,
		Dispatch:  b.checkDispatch,
		Writeback: func(step platform.TxnStep, object string) error {
			if b.host == nil {
				return nil
			}
			record, found := b.host.Record(dataowner.WritebackType + "/" + step.Effect.Writeback)
			w, valid := record.(dataowner.Writeback)
			if !found || !valid {
				return fmt.Errorf("Transaction writeback is unavailable")
			}
			w, err := b.data.ExecutionWriteback(w)
			if err != nil {
				return err
			}
			if w.State != "published" || w.On != "transaction" || w.Object != object || len(w.Result) > 0 {
				return fmt.Errorf("Effect steps require a published transaction-only writeback without answer mutations")
			}
			return nil
		},
		Call: func(call platform.TxnCall) error {
			if call.Ref.App == ID && call.Version > 0 {
				if _, selected := b.jointDeclarations[call.Ref]; selected {
					if _, version, valid := b.QueryDefinition(call.Ref.Name, call.Version); valid && version == call.Version {
						return nil
					}
				}
			}
			if b.host != nil {
				owner, ok := b.host.(platform.TransactionHost)
				if !ok {
					return fmt.Errorf("The transaction query owner is unavailable")
				}
				return owner.CheckTxnCall(call)
			}
			return nil
		},
		Reads: func(spec platform.RuleReadSpec, where string, lookup func(string) (platform.EntityInfo, bool)) error {
			return b.checkRuleCollections(spec, where, lookup)
		},
	}, engines...)
}

func (b *Build) CanonicalObject(object Object) (Object, error) {
	var canonical Object
	if err := json.Unmarshal(platform.Raw(object), &canonical); err != nil {
		return object, err
	}
	if err := b.normalizeObjectRules(&canonical); err != nil {
		return object, err
	}
	return canonical, nil
}

func (b *Build) businessRuntime() platform.TransactionExecutor {
	return b.host.TransactionRuntime(platform.BusinessRuntimeContext{App: ID, Entities: b.installed, Ledger: b.ledger, Lookup: b.conditionLookup, Link: b.transactionLink, Dispatch: b.openDispatch, Confirm: b.openConfirmation,
		Prepare: func(c platform.Caller, r *pb.ChangeRecord, txn *platform.BusinessTxn, now time.Time) (platform.TransactionProposal, *kernel.Error) {
			p, err := b.prepareProposal(c, r, txn, now)
			if err != nil {
				return platform.TransactionProposal{}, err
			}
			return platform.TransactionProposal{Apply: func(entity platform.Entity, info platform.EntityInfo, patch map[string]any, sources []string) (platform.Entity, map[string]any) {
				return proposalEntity(entity, info, patch, p, sources)
			}, Finish: func(c platform.Caller, r *pb.ChangeRecord, now time.Time) *kernel.Error {
				return b.finishProposal(c, r, p, now)
			}}, nil
		}, Effect: func(c platform.Caller, id string) (platform.TransactionEffectDefinition, *kernel.Error) {
			w, found := platform.Get[dataowner.Writeback](c, id)
			if !found {
				return platform.TransactionEffectDefinition{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Transaction writeback is unavailable")
			}
			w, err := b.data.ExecutionWriteback(w)
			if err != nil {
				return platform.TransactionEffectDefinition{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
			}
			conn, found := platform.Get[dataowner.Connection](c, w.Connection)
			if !found {
				return platform.TransactionEffectDefinition{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Transaction effect connection is unavailable")
			}
			conn, err = b.data.ExecutionConnection(conn)
			if err != nil {
				return platform.TransactionEffectDefinition{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
			}
			return platform.TransactionEffectDefinition{Request: func(id string, payload json.RawMessage) (json.RawMessage, *kernel.Error) {
				request, err := w.Request(conn, id, payload)
				if err != nil {
					return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
				}
				return platform.Raw(request), nil
			}}, nil
		}})
}
