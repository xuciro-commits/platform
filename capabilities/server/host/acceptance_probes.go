package host

import (
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
	forked, ok := d.Decision.Fork(d.tenant.App)
	if !ok {
		unsupportedStagedEffect()
	}
	fork.Decision = forked
	fork.operationInputs = maps.Clone(d.operationInputs)
	for hash, pending := range fork.operationInputs {
		fork.operationInputs[hash] = slices.Clone(pending)
	}
	fork.requests = slices.Clone(d.requests)
	for id, app := range fork.PrivateApps {
		if !d.Shared(id, app) {
			(hostView{t: d.tenant, app: app, decision: &fork}).bindOwner(app)
		}
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
