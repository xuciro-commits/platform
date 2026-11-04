package objects

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func authorizationRefusal(decision platform.AuthzDecision) *kernel.Error {
	if decision.Allow {
		return nil
	}
	layer := "verb"
	if len(decision.Layers) > 0 {
		layer = decision.Layers[len(decision.Layers)-1].Name
	}
	return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Authorization denied at {layer}: {reason}", layer, decision.Reason)
}

func AuthorizationRefusal(decision platform.AuthzDecision) *kernel.Error {
	return authorizationRefusal(decision)
}
