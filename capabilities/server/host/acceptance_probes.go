package host

import (
	"encoding/json"
	"fmt"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	runtimekernel "platformserver/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
	"slices"
)

func (d *stagedDecision) Attempt(decide func() (*pb.ChangeRecord, *kernel.Error)) (r *pb.ChangeRecord, err *kernel.Error) {
	saved := *d
	*d = *d.privateFork()
	d.attachPrivateHosts()
	defer func() {
		if p := recover(); p != nil {
			*d = saved
			d.attachPrivateHosts()
			panic(p)
		}
	}()
	r, err = decide()
	if err == nil && r != nil && len(d.Events) > len(saved.Events) && slices.ContainsFunc(d.Events[len(saved.Events):], func(event platform.Event) bool {
		return event.App == r.GetSubmission().GetAuthority() && event.Record.GetChangeId() == r.GetChangeId()
	}) {
		err = d.CheckFinalConstraints()
	}
	if err != nil {
		*d = saved
		d.attachPrivateHosts()
	}
	return r, err
}

func (d *stagedDecision) privateFork() *stagedDecision {
	fork := *d
	fork.definitionPlans = nil
	fork.BusinessFacts = slices.Clone(d.BusinessFacts)
	for i := range fork.BusinessFacts {
		fork.BusinessFacts[i] = acceptedformat.CloneBusinessFact(fork.BusinessFacts[i])
	}
	fork.BusinessRefusals = maps.Clone(d.BusinessRefusals)
	for key, fact := range fork.BusinessRefusals {
		fork.BusinessRefusals[key] = acceptedformat.CloneBusinessFact(fact)
	}
	fork.callbackWrites = maps.Clone(d.callbackWrites)
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
	fork.operationInputs = maps.Clone(d.operationInputs)
	for hash, pending := range fork.operationInputs {
		fork.operationInputs[hash] = slices.Clone(pending)
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
			owner := d.tenant.App(id)
			if owner != nil && owner.AcceptedLedger() == ledger {
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
	fork.requests, fork.RequestCounts = slices.Clone(d.requests), maps.Clone(d.RequestCounts)
	fork.Connectors = runtimekernel.CloneConnectors(d.Connectors)
	fork.PrivateApps = map[string]platform.App{}
	fork.States, fork.StateBases = map[string]platform.AcceptedStateApp{}, map[string]json.RawMessage{}
	for id, base := range d.StateBases {
		fork.StateBases[id] = slices.Clone(base)
	}
	for id, owner := range d.States {
		if d.Active[id] || d.Receiving[owner.AcceptedLedger()] {
			fork.States[id], fork.PrivateApps[id] = owner, owner
			continue
		}
		app, err := owner.Fork(owner.AcceptedLedger())
		copy, ok := app.(platform.AcceptedStateApp)
		if err != nil || !ok {
			unsupportedStagedEffect()
		}
		fork.States[id], fork.PrivateApps[id] = copy, app
	}
	for id, app := range d.PrivateApps {
		if d.Active[id] || d.Receiving[app.AcceptedLedger()] {
			fork.PrivateApps[id] = app
			continue
		}
		if fork.PrivateApps[id] != nil {
			continue
		}
		copy, err := app.Fork(app.AcceptedLedger())
		if err != nil || copy == nil || copy.Manifest().ID != id || copy.AcceptedLedger() != app.AcceptedLedger() {
			unsupportedStagedEffect()
		}
		fork.PrivateApps[id] = copy
	}
	for id, app := range fork.PrivateApps {
		if d.Active[id] || d.Receiving[app.AcceptedLedger()] {
			continue
		}
		(hostView{t: d.tenant, app: app, decision: &fork}).bindOwner(app)
	}
	fork.forkControlDecision(d.control)
	return &fork
}

func (d *stagedDecision) ProbeDecision(c platform.Caller, decide func(platform.Caller) *kernel.Error) *kernel.Error {
	probe := d.privateFork()
	err := probe.runProbe(c, decide)
	if err != nil {
		if fact, ok := probe.BusinessRefusals[err]; ok {
			d.BusinessRefused(err, fact)
		} else if issues := probe.InputIssues[err]; len(issues) > 0 {
			d.InputRefused(err, issues)
		}
	}
	return err
}

func (d *stagedDecision) runProbe(c platform.Caller, decide func(platform.Caller) *kernel.Error) (err *kernel.Error) {
	defer func() {
		if failure := recover(); failure != nil {
			if _, ok := failure.(stagedEffectPanic); !ok {
				panic(failure)
			}
			err = platform.RefuseOutsideAcceptedBoundary()
		}
	}()
	d.ProbeOnly = true
	err = decide(platform.NewCaller(d, c.Member, c.App, c.Replaying, c.Automation))
	if err == nil {
		err = d.Failure
	}
	if err == nil {
		err = d.CheckFinalConstraints()
	}
	return err
}

func cloneLists(src map[string][]string) map[string][]string {
	dst := make(map[string][]string, len(src))
	for key, list := range src {
		dst[key] = slices.Clone(list)
	}
	return dst
}

func (d *stagedDecision) stageObservers() *kernel.Error {
	if d.Observed {
		return d.Failure
	}
	count := len(d.Events)
	for _, e := range slices.Clone(d.Events) {
		for _, app := range d.tenant.LiveApps() {
			_, observer := app.(platform.Observer)
			if !observer {
				continue
			}
			owner, ok := d.stateApp(app).(platform.AcceptedObserver)
			if !ok {
				unsupportedStagedEffect()
			}
			id := app.Manifest().ID
			c := platform.NewCaller(d, platform.Member{ID: "app:" + id, Tenant: d.tenant.ID}, id, false, true)
			raw, err := owner.PlanObserved(c, e, d.tenant.router().Events(e))
			if err != nil || owner.ValidateObserved(raw) != nil {
				return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
			}
			d.Observations = append(d.Observations, acceptedformat.Observation{App: id, Body: raw})
		}
	}
	if len(d.Events) != count {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	d.Observed = true
	return d.Failure
}

func (t *Tenant) validateAcceptedObservations(saved []acceptedformat.Observation) error {
	for _, item := range saved {
		owner, ok := t.App(item.App).(platform.AcceptedObserver)
		if !ok || len(item.Body) == 0 {
			return fmt.Errorf("accepted observation needs missing projection %s", item.App)
		}
		if err := owner.ValidateObserved(item.Body); err != nil {
			return err
		}
	}
	return nil
}

func (t *Tenant) applyAcceptedObservations(saved []acceptedformat.Observation) error {
	for _, item := range saved {
		if err := t.App(item.App).(platform.AcceptedObserver).ApplyObserved(item.Body); err != nil {
			return err
		}
	}
	return nil
}
