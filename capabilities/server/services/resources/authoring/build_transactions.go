package build

import (
	"encoding/json"
	"fmt"
	"platformserver/kernel/protocol"
	"reflect"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/records"
	"platformserver/platform"
	ontology "platformserver/services/ontology"
)

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

func (b *Build) checkTransactions(object ontology.Object, engines ...platform.RuleEngine) error {
	return ontology.CheckTransactions(object, ontology.TransactionContext{
		App:         ID,
		BuilderRole: Builder,
		Lookup:      b.conditionLookup,
		ObjectInfo: func() (platform.EntityInfo, error) {
			return records.Describe(ID, Entity(object), func(reflect.Type) string { return "" })
		},
		Saved: func(typ string) bool {
			_, installed := b.installed[typ]
			_, joint := b.jointEntities[typ]
			return installed || joint
		},
		Sequences: func() []platform.Sequence {
			return ontology.TransactionSequences(b.installed)
		},
		Link:      b.transactionLink,
		Confirm:   b.data.CheckConfirmation,
		Dispatch:  b.data.CheckDispatch,
		Writeback: b.data.CheckTransactionWriteback,
		Call: func(call platform.TxnCall) error {
			if call.Ref.App == ID && call.Version > 0 {
				if _, selected := b.jointDeclarations[call.Ref]; selected {
					if _, version, valid := b.QueryDefinition(call.Ref.Name, call.Version); valid && version == call.Version {
						return nil
					}
				}
			}
			if b.transaction != nil {
				return b.transaction.CheckTxnCall(call)
			}
			return nil
		},
		Reads: func(spec platform.RuleReadSpec, where string, lookup func(string) (platform.EntityInfo, bool)) error {
			return b.checkRuleCollections(spec, where, lookup)
		},
	}, engines...)
}

func (b *Build) CanonicalObject(object ontology.Object) (ontology.Object, error) {
	var canonical ontology.Object
	if err := json.Unmarshal(platform.Raw(object), &canonical); err != nil {
		return object, err
	}
	if err := b.normalizeObjectRules(&canonical); err != nil {
		return object, err
	}
	return canonical, nil
}

func (b *Build) businessRuntime() protocol.TransactionExecutor {
	return b.host.TransactionRuntime(protocol.BusinessRuntimeContext{App: ID, Entities: b.installed, Ledger: b.ledger, Lookup: b.conditionLookup, Link: b.transactionLink, Dispatch: b.data.OpenDispatch, Confirm: b.data.OpenConfirmation,
		Prepare: func(c platform.Caller, r *pb.ChangeRecord, txn *platform.BusinessTxn, now time.Time) (protocol.TransactionProposal, *platform.Error) {
			return b.functions.PrepareProposal(c, r, txn, b.installed[txn.Subject], now)
		}, Effect: b.data.TransactionEffect})
}
