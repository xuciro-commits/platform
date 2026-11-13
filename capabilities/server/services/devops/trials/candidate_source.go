package trials

import (
	"fmt"
	"slices"

	"platformserver/platform"
	"platformserver/services/resources/authoring"
)

type StagedCandidate struct {
	Candidate platform.ReleaseCandidate
	Drafts    []build.JointDraftRef
}

type CandidateSource struct {
	Drafts    func() (build.ConstructionDrafts, bool)
	Candidate func(id string) (platform.ReleaseCandidate, error)
	Preview   func(drafts []build.JointDraftRef) (diagnostic string, candidate platform.ReleaseCandidate, err error)
	Function  func(id string) string
}

func (r CandidateSimulationRequest) Select(source CandidateSource, staged *StagedCandidate) (platform.ReleaseCandidate, string, error) {
	kind, id := r.Root()
	drafts := []build.JointDraftRef{{Kind: kind, ID: id}}
	var candidate platform.ReleaseCandidate
	var err error
	diagnostic := ""
	if staged != nil && r.FunctionID == "" {
		candidate = staged.Candidate
	} else if r.CandidateID != "" {
		candidate, err = source.Candidate(r.CandidateID)
	} else {
		if len(r.Roots) > 0 {
			owner, ok := source.Drafts()
			if !ok {
				err = fmt.Errorf("Construction draft owner is unavailable")
			} else {
				drafts, err = owner.RootDrafts(r.Roots)
			}
		}
		if r.ApplicationID != "" {
			owner, ok := source.Drafts()
			if !ok {
				err = fmt.Errorf("Construction draft owner is unavailable")
			} else {
				drafts, err = owner.ApplicationDrafts(r.ApplicationID)
			}
		}
		if staged != nil {
			if root := (build.JointDraftRef{Kind: kind, ID: id}); !slices.Contains(staged.Drafts, root) {
				drafts = append(slices.Clone(staged.Drafts), root)
			} else {
				drafts = staged.Drafts
			}
		}
		if err == nil {
			diagnostic, candidate, err = source.Preview(drafts)
		}
	}
	if r.Sample != "" && err == nil {
		err = r.SelectSample(candidate)
	}
	functionName := ""
	if r.FunctionID != "" {
		functionName = source.Function(r.FunctionID)
	}
	if err == nil && diagnostic != "" {
		err = fmt.Errorf("%s", diagnostic)
	}
	return candidate, functionName, err
}
