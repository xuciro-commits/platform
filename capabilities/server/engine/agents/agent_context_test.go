package agents

import (
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type sourceTrackingApp struct {
	platform.App
	caller platform.Caller
	submit func(*pb.Submission) (*pb.ChangeRecord, *kernel.Error)
}

func (*sourceTrackingApp) Manifest() platform.Manifest {
	return platform.Manifest{ID: "probe", Actions: platform.NewCatalog(platform.Action{Schema: "probe.write", Target: "probe.item", Title: "Write", Roles: []string{"writer"}, Payload: []platform.Field{}})}
}
func (a *sourceTrackingApp) Submit(c platform.Caller, s *pb.Submission, _ time.Time) (*pb.ChangeRecord, *kernel.Error) {
	a.caller = c
	return a.submit(s)
}

type sourceTrackingHost struct {
	platform.AgentHost
	app     platform.App
	records map[string]any
}

func (h *sourceTrackingHost) App(string) platform.App     { return h.app }
func (*sourceTrackingHost) OwnerOf(string) (string, bool) { return "probe", true }
func (h *sourceTrackingHost) Record(ref string) (any, bool) {
	value, ok := h.records[ref]
	return value, ok
}
func (*sourceTrackingHost) ScopeAgentRun(string) func() { return func() {} }

func TestLegacyAgentActionSourcesTrackOnlyRealRowsBeforeAndAfterReplay(t *testing.T) {
	for _, test := range []struct {
		name, target, created   string
		existing, refused, want bool
	}{
		{name: "logical target", target: "logical/path", created: "canonical"},
		{name: "new row", target: "new", created: "new", want: true},
		{name: "existing row", target: "existing", existing: true, want: true},
		{name: "missing refused row", target: "missing", refused: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := &sourceTrackingHost{records: map[string]any{}}
			if test.existing {
				h.records["probe.item/"+test.target] = struct{}{}
			}
			h.app = &sourceTrackingApp{submit: func(s *pb.Submission) (*pb.ChangeRecord, *kernel.Error) {
				if s.GetTarget().GetId() != test.target {
					t.Fatal("the logical target was changed")
				}
				if test.refused {
					return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Missing row")
				}
				if test.created != "" {
					h.records["probe.item/"+test.created] = struct{}{}
				}
				return &pb.ChangeRecord{ChangeId: "accepted"}, nil
			}}
			agents := New("tenant")
			agents.AttachAgent(h)
			run := platform.AgentRunRecord{Record: platform.Record{ID: "run"}, Agent: "probe.assistant"}
			definition := &platform.CompiledAgent{App: "probe", Agent: platform.Agent{Budget: platform.Budget{Actions: 1}}}
			step := platform.RunStep{CallID: "call"}
			args := map[string]any{"target": test.target, "rationale": "Write the selected target", "inputs": map[string]any{}}
			outcome, _ := agents.use(platform.NewCaller(nil, platform.Member{}, platform.AgentApp, true, false), definition, &run, platform.CompiledAgentTool{Kind: "action", Target: "probe.item", Schema: "probe.write"}, args, platform.Raw(args), &step, time.Now())
			if !test.refused && !strings.HasPrefix(outcome, "done:") {
				t.Fatalf("action failed: %s", outcome)
			}
			if caller := h.app.(*sourceTrackingApp).caller; caller.AgentRun() != run.ID || !caller.Agent {
				t.Fatalf("action lost its original agent invocation: %+v", caller)
			}
			if test.want {
				if len(step.Sources) != 1 || step.Sources[0] != "probe.item/"+test.target {
					t.Fatalf("sources=%v", step.Sources)
				}
			} else if len(step.Sources) != 0 {
				t.Fatalf("invented record dependency: %v", step.Sources)
			}
		})
	}
}
