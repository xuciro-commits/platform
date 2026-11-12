package trials

import (
	"encoding/json"
	"fmt"
	"slices"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
)

func (request *CandidateSimulationRequest) Check(staged *platform.ReleaseCandidate) *kernel.Error {
	if staged != nil {
		request.ObjectID, request.ProcessID, request.ApplicationID, request.Roots, request.CandidateID = "", "", "", nil, ""
		if request.FunctionID == "" {
			request.CandidateID = staged.ID
		}
	}
	roots := 0
	for _, root := range []string{request.ObjectID, request.ProcessID, request.FunctionID, request.ApplicationID, request.CandidateID} {
		if root != "" {
			roots++
		}
	}
	if len(request.Roots) > 0 {
		roots++
		seen := map[platform.AssetRef]bool{}
		for _, root := range request.Roots {
			if root.Check() != nil || root.App != build.ID || root.Kind != platform.AssetObject && root.Kind != platform.AssetFlow || seen[root] {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Test roots must be distinct objects or workflows")
			}
			seen[root] = true
		}
		if len(request.Roots) > 16 {
			return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		}
	}
	if roots != 1 || len(request.Model) > 256 || request.At.IsZero() || len(request.Steps) == 0 || len(request.Steps) > 20 || len(request.Actors) > 3 || len(request.Sample) > 64 || len(request.Actors) > 0 && request.As != "" {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose one saved application, object, process, function or candidate, a fixed time and 1–20 test steps")
	}
	return nil
}

func (request *CandidateSimulationRequest) Members(builder platform.Member, member func(string) (platform.Member, bool)) (platform.Member, []platform.Member, *kernel.Error) {
	m := builder
	if len(request.Actors) > 0 {
		request.As = request.Actors[0]
	}
	if request.As != "" {
		var ok bool
		m, ok = member(request.As)
		if !ok {
			return m, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "{member} is not a member here", request.As)
		}
	}
	members := []platform.Member{m}
	seenActors := map[string]bool{}
	for _, id := range request.Actors {
		if seenActors[id] {
			return m, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose distinct known members for role comparison")
		}
		seenActors[id] = true
		if id == "" {
			return m, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose known members for role comparison")
		}
		if slices.ContainsFunc(members, func(m platform.Member) bool { return m.ID == id }) {
			continue
		}
		actor, ok := member(id)
		if !ok {
			return m, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "{member} is not a member here", id)
		}
		members = append(members, actor)
	}
	for _, step := range request.Steps {
		if len(request.Actors) > 0 && step.As != "" {
			return m, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Role comparisons use the same member for every scenario step")
		}
		if step.Compute != nil && !step.Compute.Check() {
			return m, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Use a bounded compute fixture with a completed or failed outcome")
		}
		if step.Function != nil && !step.Function.Check() {
			return m, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Use a bounded function fixture with a ready or rejected outcome")
		}
		if step.AdvanceSeconds < 0 || step.AdvanceSeconds > 86400 {
			return m, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Advance each test step by 0–86400 fixed seconds")
		}
		if step.As != "" && !slices.ContainsFunc(members, func(m platform.Member) bool { return m.ID == step.As }) {
			actor, ok := member(step.As)
			if !ok {
				return m, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "{member} is not a member here", step.As)
			}
			members = append(members, actor)
		}
	}
	return m, members, nil
}

func (request *CandidateSimulationRequest) Root() (platform.AssetKind, string) {
	kind, id := platform.AssetObject, request.ObjectID
	if request.ProcessID != "" {
		kind, id = platform.AssetFlow, request.ProcessID
	}
	if request.FunctionID != "" {
		kind, id = platform.AssetFunction, request.FunctionID
	}
	return kind, id
}

func (request *CandidateSimulationRequest) SelectSample(candidate platform.ReleaseCandidate) error {
	for _, asset := range candidate.Assets {
		if asset.Ref.Kind == platform.AssetSampleSet && asset.Ref.Name == request.Sample {
			var sample platform.SampleSet
			if json.Unmarshal(asset.Body, &sample) != nil || sample.Check() != nil {
				return fmt.Errorf("Invalid application sample asset")
			}
			if request.Samples == nil {
				for _, group := range sample.Samples {
					request.Samples = append(request.Samples, SimulationSample{Type: group.Type, Records: group.Records})
				}
			}
			return nil
		}
	}
	return fmt.Errorf("Choose a sample asset in this application's draft closure")
}

func CandidateModules(candidate platform.ReleaseCandidate, modules map[string][]byte, read func(string) ([]byte, error)) *kernel.Error {
	for _, asset := range candidate.Assets {
		if asset.Ref.Kind != platform.AssetCompute {
			continue
		}
		var operation platform.Operation
		if json.Unmarshal(asset.Body, &operation) != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Candidate compute descriptor is invalid")
		}
		if operation.Binding.Kind == "wasm" {
			raw, err := read(operation.Binding.Module)
			if err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Candidate module bytes are unavailable")
			}
			modules[operation.Binding.Module] = raw
		}
	}
	return nil
}

func RunActors(request CandidateSimulationRequest, out CandidateSimulation, m platform.Member, members []platform.Member, run func(platform.Member) (CandidateSimulation, *kernel.Error)) (CandidateSimulation, *kernel.Error) {
	if len(request.Actors) == 0 {
		return out, nil
	}
	out.Actors = []SimulationActor{{Member: m.ID, Steps: out.Steps, TestID: out.TestID, Recovered: out.Recovered, Passed: out.Passed}}
	for _, id := range request.Actors[1:] {
		actor := members[slices.IndexFunc(members, func(m platform.Member) bool { return m.ID == id })]
		other, err := run(actor)
		if err != nil {
			return CandidateSimulation{}, err
		}
		out.Actors = append(out.Actors, SimulationActor{Member: actor.ID, Steps: other.Steps, TestID: other.TestID, Recovered: other.Recovered, Passed: other.Passed})
	}
	return out, nil
}
