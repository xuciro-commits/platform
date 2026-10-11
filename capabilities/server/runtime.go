package platformserver

import (
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// runtime is what the tenant does for its apps' callers (platform.Runtime):
// the only way from an app into the host.
type runtime struct{ t *Tenant }

var _ platform.Runtime = runtime{}

// Decide and Attempt explicitly retain the legacy caller's direct semantics,
// including historical input replay and entries not yet migrated to results.
// They offer no savepoint or atomic-result guarantee. Accepted entry points
// supply stagedDecision instead; nested calls never switch back to runtime.
func (runtime) Decide(c platform.Caller, app platform.App, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	if !c.Replaying {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A new decision requires a private runtime")
	}
	return app.Submit(c, s, at)
}

func (runtime) Attempt(decide func() (*pb.ChangeRecord, *kernel.Error)) (*pb.ChangeRecord, *kernel.Error) {
	return decide()
}

func (r runtime) ProbeDecision(c platform.Caller, decide func(platform.Caller) *kernel.Error) *kernel.Error {
	return r.t.newStagedDecision().runProbe(c, decide)
}

func (r runtime) Publish(c platform.Caller, record *pb.ChangeRecord) {
	r.t.publish(platform.Event{App: c.App, Record: record})
}

func (r runtime) Probe(c platform.Caller, protocol, action, id string, payload []byte, now time.Time) *kernel.Error {
	return r.t.probe(c, protocol, action, id, payload, now)
}

func (r runtime) Request(c platform.Caller, rec *pb.ChangeRecord, q platform.Request) {
	if !c.Replaying {
		unsupportedStagedEffect()
	}
	if q.Protocol == "" {
		r.t.askModel(c, rec, q)
	}
	// Historical protocol answers are already separate journal entries.
}

func (r runtime) Query(c platform.Caller, protocol, read string, provider ...string) ([]platform.ProviderResult, *kernel.Error) {
	return r.t.query(c, protocol, read, provider...)
}

func (r runtime) Notify(c platform.Caller, n platform.Notification, now time.Time, to []platform.Recipient) []string {
	return r.t.notify(c, n, now, to)
}

func (r runtime) Setting(c platform.Caller, name string) string { return r.t.setting(c, name) }

func (r runtime) Emit(c platform.Caller, kind, key, entity string, data any, now time.Time) (int, *kernel.Error) {
	return r.t.emitFor(c, kind, key, entity, data, now)
}

func (r runtime) Units(c platform.Caller, structure string, now time.Time) []string {
	if r.t.directory == nil {
		return nil
	}
	return r.t.directory.Units("member:"+c.ID, structure, c.Today(now))
}

func (r runtime) Element(c platform.Caller, id string, now time.Time) (platform.ElementInfo, bool) {
	if r.t.directory == nil {
		return platform.ElementInfo{}, false
	}
	return r.t.directory.Element(id, now.UTC().Format(time.DateOnly))
}

func (r runtime) Related(c platform.Caller, element, stereotype string, outgoing bool, now time.Time) []string {
	if r.t.directory == nil {
		return nil
	}
	return r.t.directory.Related(element, stereotype, outgoing, now.UTC().Format(time.DateOnly))
}

func (r runtime) Links(c platform.Caller, entity string) []string {
	if r.t.linker == nil {
		return nil
	}
	return r.t.linker.Links(c, entity)
}

func (r runtime) Link(c platform.Caller, from, to *pb.EntityRef, key string, now time.Time) *kernel.Error {
	if r.t.linker == nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	return r.t.linker.Link(c, from, to, key, now)
}

// Deliver accepts one batch from the calling connector (K8).
func (r runtime) Deliver(c platform.Caller, dataClass, from, to string, now time.Time) *kernel.Error {
	r.t.opsMu.Lock()
	defer r.t.opsMu.Unlock()
	return r.t.connectors.kernel.Deliver(c.Tenant, c.ID, dataClass, from, to, now)
}

func (r runtime) Assign(c platform.Caller, rec *pb.ChangeRecord, a platform.Assignment) *kernel.Error {
	if r.t.tasks == nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	return r.t.tasks.Assign(c, rec, a)
}

func (runtime) Probing() bool { return false }

// closeTask cancels a task a platform app no longer waits for, when the tenant runs the work app.
func (t *Tenant) closeTask(c platform.Caller, r *pb.ChangeRecord, id string) {
	if t.tasks != nil {
		t.tasks.Close(c, r, id)
	}
}

func (r runtime) Readable(c platform.Caller, ref string) bool {
	return r.t.Readable(c.Member, ref, time.Now())
}

func (r runtime) BoundProvider(c platform.Caller, protocol string) (string, *kernel.Error) {
	return r.t.boundProvider(c, protocol)
}

// The live runtime serves historical replay and reads; new execution requests
// require a private decision so their intents are accepted with its records.
func (r runtime) Seen(c platform.Caller, keys ...string)          { r.t.notices.markKeysRead(c.App, keys) }
func (runtime) InputRefused(*kernel.Error, []platform.FieldIssue) {}
func (runtime) RequestApproval(platform.Caller, string, *pb.Submission, time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Approval requests require a private decision")
}
func (runtime) RequestOperation(platform.Caller, *pb.ChangeRecord, platform.OperationRequest) (platform.OperationCall, *kernel.Error) {
	return platform.OperationCall{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Operation requests require a private decision")
}
func (runtime) CancelOperation(platform.Caller, *pb.ChangeRecord, string) *kernel.Error {
	return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Operation cancellation requires a private decision")
}
func (runtime) RequestCompilation(platform.Caller, *pb.ChangeRecord, string, string, platform.CodeBuildRequest) (platform.OperationCall, *kernel.Error) {
	return platform.OperationCall{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Compilation requests require a private decision")
}
func (runtime) RequestFunction(platform.Caller, *pb.ChangeRecord, platform.FunctionRequest) (platform.FunctionCall, *kernel.Error) {
	return platform.FunctionCall{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Function requests require a private decision")
}
