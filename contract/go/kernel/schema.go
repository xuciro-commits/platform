package kernel

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

type SchemaRegistry struct {
	known    map[string]bool
	upgrades map[string]bool
}

func NewSchemaRegistry(known []*pb.SchemaRef, upgrades []*pb.UpgradeStep) *SchemaRegistry {
	r := &SchemaRegistry{known: map[string]bool{}, upgrades: map[string]bool{}}
	for _, s := range known {
		r.known[schemaKey(s)] = true
	}
	for _, u := range upgrades {
		r.upgrades[stepKey(u.GetName(), u.GetFromVersion())] = true
	}
	return r
}

func (r *SchemaRegistry) Accepts(s *pb.SchemaRef) bool {
	for v := s.GetVersion(); ; v++ {
		if r.known[stepKey(s.GetName(), v)] {
			return true
		}
		if !r.upgrades[stepKey(s.GetName(), v)] {
			return false
		}
	}
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

func (r *SchemaRegistry) AddUpgrade(u *pb.UpgradeStep) *Error {
	switch {
	case u.GetName() == "" || u.GetFromVersion() < 1:
		return errorf(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	case !r.known[stepKey(u.GetName(), u.GetFromVersion()+1)]:
		return errorf(pb.ErrorCode_ERROR_CODE_INVALID_REFERENCE)
	case r.upgrades[stepKey(u.GetName(), u.GetFromVersion())]:
		return errorf(pb.ErrorCode_ERROR_CODE_CONFLICT)
	}
	r.upgrades[stepKey(u.GetName(), u.GetFromVersion())] = true
	return nil
}

func (r *SchemaRegistry) Path(from *pb.SchemaRef, to uint32) ([]uint32, *Error) {
	if !r.known[stepKey(from.GetName(), to)] {
		return nil, errorf(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA)
	}
	if to < from.GetVersion() {
		return nil, errorf(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
	path := []uint32{from.GetVersion()}
	for v := from.GetVersion(); v < to; v++ {
		if !r.upgrades[stepKey(from.GetName(), v)] {
			return nil, errorf(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA)
		}
		path = append(path, v+1)
	}
	return path, nil
}

func (r *SchemaRegistry) Negotiate(name string, offered []uint32) (uint32, *Error) {
	var best uint32
	for _, v := range offered {
		if v > best && r.Accepts(&pb.SchemaRef{Name: name, Version: v}) {
			best = v
		}
	}
	if best == 0 {
		return 0, errorf(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA)
	}
	return best, nil
}

func (r *SchemaRegistry) Retire(s *pb.SchemaRef, stored []*pb.SchemaRef) *Error {
	if !r.known[schemaKey(s)] {
		return errorf(pb.ErrorCode_ERROR_CODE_NOT_FOUND)
	}
	delete(r.known, schemaKey(s))
	for _, p := range stored {
		if !r.Accepts(p) {
			r.known[schemaKey(s)] = true
			return errorf(pb.ErrorCode_ERROR_CODE_CONFLICT)
		}
	}
	return nil
}

func stepKey(name string, version uint32) string {
	return schemaKey(&pb.SchemaRef{Name: name, Version: version})
}
