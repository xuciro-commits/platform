package security

import "platformserver/platform"

func ProjectApprovals(owner platform.App, project string) int {
	if access, ok := owner.(interface {
		SnapshotState() platform.AuthorizationState
	}); ok {
		if config := access.SnapshotState().Projects[project]; config != nil {
			return max(1, config.Approvals)
		}
	}
	return 1
}

func CompartmentExists(owner platform.App, id string) bool {
	for _, marking := range owner.(interface{ MarkingViews() []platform.Marking }).MarkingViews() {
		if marking.ID == id && !marking.Classification && !marking.Archived {
			return true
		}
	}
	return false
}
