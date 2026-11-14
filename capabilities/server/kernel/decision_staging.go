package kernel

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"time"

	"google.golang.org/protobuf/proto"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/kernel/records"
	"platformserver/platform"
)

func (d *Decision) MarkOperationCancelled(id string) {
	if d.OperationCancels == nil {
		d.OperationCancels = map[string]bool{}
	}
	d.OperationCancels[id] = true
}

func (d *Decision) NextRequest(key string) int {
	if d.RequestCounts == nil {
		d.RequestCounts = map[string]int{}
	}
	n := d.RequestCounts[key]
	d.RequestCounts[key]++
	return n
}

func (d *Decision) AdoptRefusal(probe *Decision, err *kernel.Error) {
	if fact, ok := probe.BusinessRefusals[err]; ok {
		d.BusinessRefused(err, fact)
	} else if issues := probe.InputIssues[err]; len(issues) > 0 {
		d.InputRefused(err, issues)
	}
}

func (d *Decision) CheckAttempt(r *pb.ChangeRecord, events int) *kernel.Error {
	if r != nil && len(d.Events) > events && slices.ContainsFunc(d.Events[events:], func(event platform.Event) bool {
		return event.App == r.GetSubmission().GetAuthority() && event.Record.GetChangeId() == r.GetChangeId()
	}) {
		return d.CheckFinalConstraints()
	}
	return nil
}

func Attempt[D any](d *D, private func() *D, attach func(), decide func() (*pb.ChangeRecord, *kernel.Error), check func(*pb.ChangeRecord) *kernel.Error) (r *pb.ChangeRecord, err *kernel.Error) {
	saved := *d
	*d = *private()
	attach()
	defer func() {
		if p := recover(); p != nil {
			*d = saved
			attach()
			panic(p)
		}
	}()
	r, err = decide()
	if err == nil {
		err = check(r)
	}
	if err != nil {
		*d = saved
		attach()
	}
	return r, err
}

func (d *Decision) RunProbe(rt platform.Runtime, c platform.Caller, decide func(platform.Caller) *kernel.Error, unsupported func(any) bool) (err *kernel.Error) {
	defer func() {
		if failure := recover(); failure != nil {
			if !unsupported(failure) {
				panic(failure)
			}
			err = platform.RefuseOutsideAcceptedBoundary()
		}
	}()
	d.ProbeOnly = true
	err = decide(platform.NewCaller(rt, c.Member, c.App, c.Replaying, c.Automation))
	if err == nil {
		err = d.Failure
	}
	if err == nil {
		err = d.CheckFinalConstraints()
	}
	return err
}

func (d *Decision) PrivateApp(app platform.App, bind func(platform.App)) (platform.App, bool) {
	id := app.Manifest().ID
	if existing := d.PrivateApps[id]; existing != nil {
		return existing, true
	}
	var fork platform.App
	var before json.RawMessage
	var err error
	owner, ownsState := app.(platform.AcceptedStateApp)
	if ownsState {
		before, err = owner.AcceptedState()
	}
	if err == nil {
		fork, err = app.Fork(app.AcceptedLedger())
	}
	if err != nil || fork == nil || fork.Manifest().ID != id || fork.AcceptedLedger() != app.AcceptedLedger() {
		return nil, false
	}
	if d.PrivateApps == nil {
		d.PrivateApps = map[string]platform.App{}
	}
	d.PrivateApps[id] = fork
	bind(fork)
	if ownsState {
		copy, ok := fork.(platform.AcceptedStateApp)
		if !ok {
			return nil, false
		}
		if d.States == nil {
			d.States = map[string]platform.AcceptedStateApp{}
			d.StateBases = map[string]json.RawMessage{}
		}
		d.States[id], d.StateBases[id] = copy, slices.Clone(before)
	}
	return fork, true
}

type ObserverHooks struct {
	Apps    func() []platform.App
	Private func(platform.App) platform.App
	Caller  func(id string) platform.Caller
	Events  func(platform.Event) []string
}

func (d *Decision) StageObservers(h ObserverHooks) (*kernel.Error, bool) {
	if d.Observed {
		return d.Failure, true
	}
	count := len(d.Events)
	for _, e := range slices.Clone(d.Events) {
		for _, app := range h.Apps() {
			_, observer := app.(platform.Observer)
			if !observer {
				continue
			}
			owner, ok := h.Private(app).(platform.AcceptedObserver)
			if !ok {
				return nil, false
			}
			id := app.Manifest().ID
			raw, err := owner.PlanObserved(h.Caller(id), e, h.Events(e))
			if err != nil || owner.ValidateObserved(raw) != nil {
				return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}, true
			}
			d.Observations = append(d.Observations, acceptedformat.Observation{App: id, Body: raw})
		}
	}
	if len(d.Events) != count {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}, true
	}
	d.Observed = true
	return d.Failure, true
}

func (d *Decision) AllocateNumber(c platform.Caller, tenant string, owner func(string) platform.App, rec *pb.ChangeRecord, name string, date time.Time) (number string, err *kernel.Error) {
	defer func() {
		if err != nil {
			d.FailDecision(err)
		}
	}()
	if rec.GetChangeId() == "" || rec.GetSubmission().GetAuthority() != c.App ||
		rec.GetSubmission().GetTenantId() != tenant {
		return "", &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	app := owner(c.App)
	if app == nil {
		return "", &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	i := slices.IndexFunc(app.Manifest().Sequences, func(s platform.Sequence) bool { return s.Name == name })
	if i < 0 {
		return "", &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	s, year := app.Manifest().Sequences[i], 0
	if s.Yearly {
		year = date.Year()
	}
	key := fmt.Sprintf("%s/%s/%d", c.App, name, year)
	if _, taken := d.Allocated[key]; !taken {
		d.SequenceBases[key] = d.Sequences[key]
	}
	if _, started := d.Sequences[key]; !started && s.SeedRecord != "" {
		waterline, problem := d.SequenceWaterline(c.App, s, date.Year())
		if problem != nil {
			return "", problem
		}
		d.Sequences[key] = waterline
	}
	d.Sequences[key]++
	d.Allocated[key] = d.Sequences[key]
	return s.Format(date.Year(), d.Sequences[key]), nil
}

func (d *Decision) PublishRecord(app platform.App, c platform.Caller, record *pb.ChangeRecord, installLink func(schema string, raw []byte) error) bool {
	if tracker, ok := app.(interface {
		TrackResource(platform.Caller, *pb.ChangeRecord, time.Time)
	}); ok {
		tracker.TrackResource(c, record, d.At)
	}
	changed := slices.Clone(d.Changed[c.App+"/"+record.GetChangeId()])
	d.Events = append(d.Events, platform.Event{App: c.App, Record: record, Changed: changed})
	schema := record.GetSubmission().GetSchema().GetName()
	if publisher, ok := app.(platform.AcceptedPublisher); ok &&
		slices.Contains(publisher.AcceptedPublicationSchemas(), schema) {
		ref := record.GetSubmission().GetTarget()
		var raw []byte
		var err error
		func() {
			d.Records.Lock()
			defer d.Records.Unlock()
			et := d.Records.Type(ref.GetType())
			if et != nil && et.Rows().Get(ref.GetId()) != nil {
				raw, err = json.Marshal(et.Rows().Get(ref.GetId()).Value.Interface())
			}
		}()
		if len(raw) == 0 || err != nil || publisher.ValidateAcceptedPublication(schema, raw) != nil {
			return false
		}
		if err := installLink(schema, raw); err != nil {
			d.Failure = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, records.LinkCardinalityFailure)
			return true
		}
		if d.Publications == nil {
			d.Publications = map[string]*acceptedformat.Publication{}
		}
		d.Publications[c.App+"/"+record.GetChangeId()] = &acceptedformat.Publication{Schema: schema, Image: raw}
	}
	return true
}

func (d *Decision) DecideApp(c platform.Caller, app platform.App, s *pb.Submission, at time.Time, definition func() (*platform.DecisionDefinition, error)) (*pb.ChangeRecord, *kernel.Error, bool) {
	if app.AcceptedLedger() == nil {
		return nil, nil, false
	}
	id := app.Manifest().ID
	if d.Receiving[app.AcceptedLedger()] {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The decision cannot re-enter its held owner"), true
	}
	if d.Active[id] || len(d.Active) >= acceptedformat.MaxHops {
		return nil, nil, false
	}
	d.Active[id] = true
	defer func() { delete(d.Active, id) }()
	if d.At.IsZero() {
		d.At = at
	}
	c.Roles = maps.Clone(c.Roles)
	def, definitionErr := definition()
	if definitionErr != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The execution definition and dependencies are unavailable"), true
	}
	previous := d.CurrentSubmission
	d.CurrentSubmission = proto.Clone(s).(*pb.Submission)
	defer func() { d.CurrentSubmission = previous }()
	r, err := app.Submit(c, proto.Clone(s).(*pb.Submission), at)
	if err != nil && d.Failure == nil {
		d.Failure = err
	}
	if d.Failure != nil {
		return nil, d.Failure, true
	}
	if r != nil && def != nil {
		if d.ObservationInput != nil {
			def.Observation = d.ObservationInput
		}
		def.Contract.Family = r.GetSubmission().GetSchema().GetVersion()
		if d.Definitions == nil {
			d.Definitions = map[string]*platform.DecisionDefinition{}
		}
		d.Definitions[id+"/"+r.GetChangeId()] = def
	}
	return r, nil, true
}

func (d *Decision) KeepChanges(receipt *pb.ChangeRecord) {
	for i, event := range d.Events {
		if event.Record == receipt {
			d.Events[i].Changed = slices.Clone(d.Changed[event.App+"/"+receipt.GetChangeId()])
		}
	}
}
