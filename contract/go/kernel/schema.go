package kernel

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

type SchemaRegistry struct {
	known map[string]bool
}

func NewSchemaRegistry(known []*pb.SchemaRef) *SchemaRegistry {
	r := &SchemaRegistry{known: map[string]bool{}}
	for _, s := range known {
		r.known[schemaKey(s)] = true
	}
	return r
}

func (r *SchemaRegistry) Accepts(s *pb.SchemaRef) bool {
	return r.known[schemaKey(s)]
}

func (r *SchemaRegistry) Learn(refs ...*pb.SchemaRef) *Error {
	for _, s := range refs {
		if s.GetName() == "" || s.GetVersion() < 1 {
			return errorf(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
		}
	}
	for _, s := range refs {
		r.known[schemaKey(s)] = true
	}
	return nil
}
