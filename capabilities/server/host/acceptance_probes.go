package host

import (
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/proto"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
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
	if err == nil && r != nil && len(d.events) > len(saved.events) && slices.ContainsFunc(d.events[len(saved.events):], func(event platform.Event) bool {
		return event.App == r.GetSubmission().GetAuthority() && event.Record.GetChangeId() == r.GetChangeId()
	}) {
		err = d.checkFinalConstraints()
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
	fork.businessFacts = slices.Clone(d.businessFacts)
	for i := range fork.businessFacts {
		fork.businessFacts[i] = acceptedformat.CloneBusinessFact(fork.businessFacts[i])
	}
	fork.businessRefusals = maps.Clone(d.businessRefusals)
	for key, fact := range fork.businessRefusals {
		fork.businessRefusals[key] = acceptedformat.CloneBusinessFact(fact)
	}
	fork.callbackWrites = maps.Clone(d.callbackWrites)
	if d.callbackOrigin != nil {
		origin := *d.callbackOrigin
		origin.Output = slices.Clone(origin.Output)
		fork.callbackOrigin = &origin
	}
	fork.operationCancels = maps.Clone(d.operationCancels)
	fork.operationAnswers = maps.Clone(d.operationAnswers)
	for id, answer := range fork.operationAnswers {
		answer.Output = slices.Clone(answer.Output)
		if answer.Ref != nil {
			ref := *answer.Ref
			answer.Ref = &ref
		}
		fork.operationAnswers[id] = answer
	}
	fork.operationInputs = maps.Clone(d.operationInputs)
	for hash, pending := range fork.operationInputs {
		fork.operationInputs[hash] = slices.Clone(pending)
	}
	fork.inputIssues = maps.Clone(d.inputIssues)
	fork.constraintWrites = maps.Clone(d.constraintWrites)
	for refusal, issues := range fork.inputIssues {
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
		fork.inputIssues[refusal] = issues
	}
	fork.records = d.records.ForkRecords()
	fork.records.InheritDecision(d.records)
	fork.logs = map[*platform.Ledger]platform.ChangeLog{}
	for ledger, log := range d.logs {
		suspended := d.receiving[ledger]
		for id := range d.active {
			owner := d.tenant.app(id)
			if owner != nil && owner.AcceptedLedger() == ledger {
				suspended = true
				break
			}
		}
		if suspended {
			fork.logs[ledger] = log
		} else {
			fork.logs[ledger] = log.ForkLog()
		}
	}
	fork.events, fork.notices = slices.Clone(d.events), slices.Clone(d.notices)
	fork.definitions = maps.Clone(d.definitions)
	fork.sequences, fork.allocated, fork.sequenceBases = maps.Clone(d.sequences), maps.Clone(d.allocated), maps.Clone(d.sequenceBases)
	fork.writers, fork.changed = cloneLists(d.writers), cloneLists(d.changed)
	fork.noticeEffects, fork.publications = slices.Clone(d.noticeEffects), maps.Clone(d.publications)
	fork.intents = slices.Clone(d.intents)
	fork.iterations = slices.Clone(d.iterations)
	fork.authorizations = slices.Clone(d.authorizations)
	fork.observations = slices.Clone(d.observations)
	fork.active = maps.Clone(d.active)
	fork.receiving = maps.Clone(d.receiving)
	fork.requests, fork.requestCounts = slices.Clone(d.requests), maps.Clone(d.requestCounts)
	descriptors, marks := d.connectors.State()
	for i, descriptor := range descriptors {
		descriptors[i] = proto.Clone(descriptor).(*pb.ConnectorDescriptor)
	}
	fork.connectors = d.tenant.services.Kernel.Connectors()
	fork.connectors.Restore(descriptors, marks)
	fork.deliveries = slices.Clone(d.deliveries)
	fork.privateApps = map[string]platform.App{}
	fork.states, fork.stateBases = map[string]platform.AcceptedStateApp{}, map[string]json.RawMessage{}
	for id, base := range d.stateBases {
		fork.stateBases[id] = slices.Clone(base)
	}
	for id, owner := range d.states {
		if d.active[id] || d.receiving[owner.AcceptedLedger()] {
			fork.states[id], fork.privateApps[id] = owner, owner
			continue
		}
		app, err := owner.ForkAcceptedState()
		copy, ok := app.(platform.AcceptedStateApp)
		if err != nil || !ok {
			unsupportedStagedEffect()
		}
		fork.states[id], fork.privateApps[id] = copy, app
	}
	for id, app := range d.privateApps {
		if d.active[id] || d.receiving[app.AcceptedLedger()] {
			fork.privateApps[id] = app
			continue
		}
		if fork.privateApps[id] != nil {
			continue
		}
		owner, ok := app.(platform.DecisionForkApp)
		if !ok {
			unsupportedStagedEffect()
		}
		copy, err := owner.ForkDecision()
		if err != nil || copy == nil || copy.Manifest().ID != id || copy.AcceptedLedger() != app.AcceptedLedger() {
			unsupportedStagedEffect()
		}
		fork.privateApps[id] = copy
	}
	for id, app := range fork.privateApps {
		if d.active[id] || d.receiving[app.AcceptedLedger()] {
			continue
		}
		(hostView{t: d.tenant, app: app, decision: &fork}).bindOwner(app)
	}
	fork.forkControlDecision(d.control)
	for ledger := range fork.logs {
		fork.bindPrivateFacts(ledger)
	}
	return &fork
}

func (d *stagedDecision) ProbeDecision(c platform.Caller, decide func(platform.Caller) *kernel.Error) *kernel.Error {
	probe := d.privateFork()
	err := probe.runProbe(c, decide)
	if err != nil {
		if fact, ok := probe.businessRefusals[err]; ok {
			d.BusinessRefused(err, fact)
		} else if issues := probe.inputIssues[err]; len(issues) > 0 {
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
	d.probing = true
	err = decide(platform.NewCaller(d, c.Member, c.App, c.Replaying, c.Automation))
	if err == nil {
		err = d.failure
	}
	if err == nil {
		err = d.checkFinalConstraints()
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
	if d.observed {
		return d.failure
	}
	count := len(d.events)
	for _, e := range slices.Clone(d.events) {
		for _, app := range d.tenant.liveApps() {
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
			raw, err := owner.PlanObserved(c, e, d.tenant.protocolEvents(e))
			if err != nil || owner.ValidateObserved(raw) != nil {
				return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
			}
			d.observations = append(d.observations, acceptedformat.Observation{App: id, Body: raw})
		}
	}
	if len(d.events) != count {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	d.observed = true
	return d.failure
}

func (t *Tenant) validateAcceptedObservations(saved []acceptedformat.Observation) error {
	for _, item := range saved {
		owner, ok := t.app(item.App).(platform.AcceptedObserver)
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
		if err := t.app(item.App).(platform.AcceptedObserver).ApplyObserved(item.Body); err != nil {
			return err
		}
	}
	return nil
}
