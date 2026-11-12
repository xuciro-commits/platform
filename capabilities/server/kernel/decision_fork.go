package kernel

import (
	"encoding/json"
	"maps"
	"slices"

	"platformkernel/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
)

func (d Decision) Fork(owner func(string) platform.App) (Decision, bool) {
	fork := d
	fork.BusinessFacts = slices.Clone(d.BusinessFacts)
	for i := range fork.BusinessFacts {
		fork.BusinessFacts[i] = acceptedformat.CloneBusinessFact(fork.BusinessFacts[i])
	}
	fork.BusinessRefusals = maps.Clone(d.BusinessRefusals)
	for key, fact := range fork.BusinessRefusals {
		fork.BusinessRefusals[key] = acceptedformat.CloneBusinessFact(fact)
	}
	fork.CallbackWrites = maps.Clone(d.CallbackWrites)
	if d.CallbackOrigin != nil {
		origin := *d.CallbackOrigin
		origin.Output = slices.Clone(origin.Output)
		fork.CallbackOrigin = &origin
	}
	fork.OperationCancels = maps.Clone(d.OperationCancels)
	fork.OperationAnswers = maps.Clone(d.OperationAnswers)
	for id, answer := range fork.OperationAnswers {
		answer.Output = slices.Clone(answer.Output)
		if answer.Ref != nil {
			ref := *answer.Ref
			answer.Ref = &ref
		}
		fork.OperationAnswers[id] = answer
	}
	fork.InputIssues = maps.Clone(d.InputIssues)
	fork.ConstraintWrites = maps.Clone(d.ConstraintWrites)
	for refusal, issues := range fork.InputIssues {
		issues = slices.Clone(issues)
		for i := range issues {
			issues[i].Translations = maps.Clone(issues[i].Translations)
			issues[i].Values = maps.Clone(issues[i].Values)
			for name, raw := range issues[i].Values {
				issues[i].Values[name] = slices.Clone(raw)
			}
			issues[i].Path = slices.Clone(issues[i].Path)
			issues[i].RelatedPaths = slices.Clone(issues[i].RelatedPaths)
			for j := range issues[i].RelatedPaths {
				issues[i].RelatedPaths[j] = slices.Clone(issues[i].RelatedPaths[j])
			}
		}
		fork.InputIssues[refusal] = issues
	}
	fork.Records = d.Records.ForkRecords()
	fork.Records.InheritDecision(d.Records)
	fork.Logs = map[*platform.Ledger]*kernel.ChangeLog{}
	for ledger, log := range d.Logs {
		suspended := d.Receiving[ledger]
		for id := range d.Active {
			if app := owner(id); app != nil && app.AcceptedLedger() == ledger {
				suspended = true
				break
			}
		}
		if suspended {
			fork.Logs[ledger] = log
		} else {
			fork.Logs[ledger] = log.Fork()
		}
	}
	fork.Events, fork.Notices = slices.Clone(d.Events), d.Notices.Clone()
	fork.Definitions = maps.Clone(d.Definitions)
	fork.Sequences, fork.Allocated, fork.SequenceBases = maps.Clone(d.Sequences), maps.Clone(d.Allocated), maps.Clone(d.SequenceBases)
	fork.Writers, fork.Changed = cloneLists(d.Writers), cloneLists(d.Changed)
	fork.Publications = maps.Clone(d.Publications)
	fork.Intents = slices.Clone(d.Intents)
	fork.Iterations = slices.Clone(d.Iterations)
	fork.Authorizations = slices.Clone(d.Authorizations)
	fork.Observations = slices.Clone(d.Observations)
	fork.Active = maps.Clone(d.Active)
	fork.Receiving = maps.Clone(d.Receiving)
	fork.RequestCounts = maps.Clone(d.RequestCounts)
	fork.Connectors = CloneConnectors(d.Connectors)
	fork.PrivateApps = map[string]platform.App{}
	fork.States, fork.StateBases = map[string]platform.AcceptedStateApp{}, map[string]json.RawMessage{}
	for id, base := range d.StateBases {
		fork.StateBases[id] = slices.Clone(base)
	}
	for id, state := range d.States {
		if d.Shared(id, state) {
			fork.States[id], fork.PrivateApps[id] = state, state
			continue
		}
		app, err := state.Fork(state.AcceptedLedger())
		copy, ok := app.(platform.AcceptedStateApp)
		if err != nil || !ok {
			return Decision{}, false
		}
		fork.States[id], fork.PrivateApps[id] = copy, app
	}
	for id, app := range d.PrivateApps {
		if d.Shared(id, app) {
			fork.PrivateApps[id] = app
			continue
		}
		if fork.PrivateApps[id] != nil {
			continue
		}
		copy, err := app.Fork(app.AcceptedLedger())
		if err != nil || copy == nil || copy.Manifest().ID != id || copy.AcceptedLedger() != app.AcceptedLedger() {
			return Decision{}, false
		}
		fork.PrivateApps[id] = copy
	}
	return fork, true
}

func (d *Decision) Shared(id string, app platform.App) bool {
	return d.Active[id] || d.Receiving[app.AcceptedLedger()]
}

func cloneLists(src map[string][]string) map[string][]string {
	dst := make(map[string][]string, len(src))
	for key, list := range src {
		dst[key] = slices.Clone(list)
	}
	return dst
}
