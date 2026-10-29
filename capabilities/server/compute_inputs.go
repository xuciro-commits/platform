package platformserver

import "platformserver/platform"

type preparedOperationInput struct {
	ref         platform.OperationInputArtifact
	requestHash string
}
