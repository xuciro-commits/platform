package kernel

import (
	"encoding/json"
	"platformserver/kernel/protocol"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

func (r *ConnectorRoster) Image() (json.RawMessage, error) {
	descriptors, _ := r.State()
	return protocol.Protos(descriptors)
}

func (r *ConnectorRoster) InstallImage(raw json.RawMessage) error {
	descriptors, err := protocol.Unprotos[*pb.ConnectorDescriptor](raw)
	if err != nil {
		return err
	}
	return r.Install(descriptors)
}
