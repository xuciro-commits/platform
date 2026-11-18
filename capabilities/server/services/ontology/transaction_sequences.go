package ontology

import (
	"maps"
	"slices"

	"platformserver/platform"
)

func TransactionSequences(installed map[string]platform.RecordType) []platform.Sequence {
	byName := map[string]platform.Sequence{}
	for _, typ := range slices.Sorted(maps.Keys(installed)) {
		if txn := installed[typ].CreateTxn; txn != nil {
			WalkBusinessTxn(txn, func(step platform.TxnStep) {
				if step.Sequence != nil {
					byName[step.Sequence.Name] = *step.Sequence
				}
			})
		}
		if life := installed[typ].Lifecycle; life != nil {
			for _, action := range life.Actions {
				for _, rule := range action.Rules {
					if rule.Transaction == nil || rule.Transaction.Spec == nil {
						continue
					}
					WalkBusinessTxn(rule.Transaction.Spec, func(step platform.TxnStep) {
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
