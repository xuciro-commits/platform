package kernel

import (
	"encoding/json"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

func (r *ConnectorRoster) Image() (json.RawMessage, error) {
	descriptors, _ := r.State()
	return platform.Protos(descriptors)
}

func (r *ConnectorRoster) InstallImage(raw json.RawMessage) error {
	descriptors, err := platform.Unprotos[*pb.ConnectorDescriptor](raw)
	if err != nil {
		return err
	}
	return r.Install(descriptors)
}
