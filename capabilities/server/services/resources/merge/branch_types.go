package branches

import (
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
)
