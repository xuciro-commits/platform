package releases

import (
	"platformserver/platform"
	"platformserver/services/resources/authoring"
	"slices"
)

func ReferencedDraftClosure(owner build.ConstructionDrafts, kind platform.AssetKind, id, mine string) (ReleaseDraftClosure, error) {
	drafts, err := owner.ReferencedDrafts(kind, id, mine)
	if err != nil {
		return ReleaseDraftClosure{}, err
	}
	if drafts == nil {
		drafts = []build.JointDraftRef{}
	}
	return ReleaseDraftClosure{Drafts: drafts}, nil
}

func WorkingDrafts(owner build.ConstructionDrafts, memberID string) (ReleaseWorkingState, error) {
	changes, err := owner.WorkingDrafts(memberID)
	if err != nil {
		return ReleaseWorkingState{}, err
	}
	state := ReleaseWorkingState{Drafts: []ReleaseWorkingDraft{}}
	for _, change := range changes {
		state.Drafts = append(state.Drafts, ReleaseWorkingDraft{Kind: change.Ref.Kind, ID: change.ID, Type: change.Type, Title: change.Title})
	}
	return state, nil
}

func HideInvisibleDownstream(reply *ReleasePreview, definitions []platform.Definition) {
	visible := map[platform.AssetRef]bool{}
	for _, definition := range definitions {
		visible[definition.Ref] = true
	}
	for i := range reply.DraftReviews {
		reply.DraftReviews[i].Downstream = slices.DeleteFunc(reply.DraftReviews[i].Downstream, func(resource ReleaseDraftResource) bool {
			return !visible[resource.Ref]
		})
	}
}
