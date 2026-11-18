package actions

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

func Admission(stopped, suspended bool, member string, delegated *platform.Error) *platform.Error {
	if stopped {
		return StoppedForMaintenance()
	}
	if suspended {
		return AgentSuspended(member)
	}
	return delegated
}

func StoppedForMaintenance() *platform.Error {
	return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Business access is stopped for maintenance")
}

func AgentSuspended(member string) *platform.Error {
	return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The agent {agent} is suspended", member)
}
