package kernel

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	contract "platformkernel/kernel"

	"google.golang.org/protobuf/proto"
)

func CloneConnectors(c *contract.Connectors) *contract.Connectors {
	descriptors, marks := c.State()
	for i, descriptor := range descriptors {
		descriptors[i] = proto.Clone(descriptor).(*pb.ConnectorDescriptor)
	}
	out := contract.NewConnectors()
	out.Restore(descriptors, marks)
	return out
}
