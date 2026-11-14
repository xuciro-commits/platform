package kernel

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/protocol"

	"google.golang.org/protobuf/proto"
)

func CloneConnectors(c *protocol.Connectors) *protocol.Connectors {
	descriptors, marks := c.State()
	for i, descriptor := range descriptors {
		descriptors[i] = proto.Clone(descriptor).(*pb.ConnectorDescriptor)
	}
	out := protocol.NewConnectors()
	out.Restore(descriptors, marks)
	return out
}
