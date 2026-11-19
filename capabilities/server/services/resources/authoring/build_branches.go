package authoring

import (
	"platformserver/platform"
	branchowner "platformserver/services/resources/merge/owner"
)

type Branch = branchowner.Branch
type BranchCheckHeader = branchowner.BranchCheckHeader
type BranchChange = branchowner.BranchChange
type BranchReview = branchowner.BranchReview
type BranchProposal = branchowner.BranchProposal
type BranchMerge = branchowner.BranchMerge

const (
	BranchType = branchowner.BranchType

	BranchOpen     = branchowner.BranchOpen
	BranchProposed = branchowner.BranchProposed
	BranchMerged   = branchowner.BranchMerged
	BranchClosed   = branchowner.BranchClosed

	BranchMaxChanges  = branchowner.BranchMaxChanges
	BranchMaxSpec     = branchowner.BranchMaxSpec
	BranchMaxBytes    = branchowner.BranchMaxBytes
	BranchMaxReviews  = branchowner.BranchMaxReviews
	BranchMaxChecks   = branchowner.BranchMaxChecks
	BranchMaxReviewer = branchowner.BranchMaxReviewer
)

func BranchDraftType(typ string) bool {
	_, found := resourceDescriptorOfType(typ)
	return found
}

func BranchResource(typ, name string) (platform.AssetRef, error) {
	return draftAssetRef(typ, name)
}

func (b *Build) branchEntity() platform.RecordType {
	return b.branches.Entity()
}

func (b *Build) BranchReviewRequirement(c platform.Caller, branch Branch) ([]string, int, error) {
	return b.branches.ReviewRequirement(c, branch)
}
