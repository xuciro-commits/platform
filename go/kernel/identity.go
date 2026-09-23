// Package kernel is the Go reference implementation of the kernel contract.
// The contract itself is Contract/proto, Contract/spec and Contract/vectors.
package kernel

import (
	"slices"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

// Ref is a comparable entity reference.
type Ref struct{ Type, ID string }

func RefOf(r *pb.EntityRef) Ref { return Ref{r.GetType(), r.GetId()} }

// Identity implements K1 (Contract/spec/K1-identity.md).
type Identity struct {
	entities  map[Ref]bool
	redirects map[Ref][]Ref
}

func NewIdentity(entities []*pb.EntityRef) *Identity {
	id := &Identity{entities: map[Ref]bool{}, redirects: map[Ref][]Ref{}}
	for _, e := range entities {
		id.entities[RefOf(e)] = true
	}
	return id
}

func (id *Identity) AddRedirect(r *pb.Redirect) *Error {
	from, to := RefOf(r.GetFrom()), make([]Ref, 0, len(r.GetTo()))
	for _, t := range r.GetTo() {
		to = append(to, RefOf(t))
	}
	switch {
	case r.GetKind() == pb.RedirectKind_REDIRECT_KIND_MERGE && len(to) != 1,
		r.GetKind() == pb.RedirectKind_REDIRECT_KIND_SPLIT && len(to) < 2,
		r.GetKind() == pb.RedirectKind_REDIRECT_KIND_UNSPECIFIED:
		return errorf(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT) // I3
	case !id.entities[from]:
		return errorf(pb.ErrorCode_ERROR_CODE_NOT_FOUND) // I4
	case slices.ContainsFunc(to, func(t Ref) bool { return !id.entities[t] }):
		return errorf(pb.ErrorCode_ERROR_CODE_INVALID_REFERENCE) // I5
	case id.redirects[from] != nil:
		return errorf(pb.ErrorCode_ERROR_CODE_CONFLICT) // I6
	case slices.ContainsFunc(to, func(t Ref) bool { return id.reaches(from, t) }):
		return errorf(pb.ErrorCode_ERROR_CODE_REDIRECT_CYCLE) // I7
	}
	id.redirects[from] = to
	return nil
}

// Resolve returns one terminal reference, or several when a split leaves a choice (I8).
func (id *Identity) Resolve(r *pb.EntityRef) ([]Ref, *Error) {
	ref := RefOf(r)
	if !id.entities[ref] {
		return nil, errorf(pb.ErrorCode_ERROR_CODE_NOT_FOUND) // I2
	}
	var terminals []Ref
	var walk func(Ref)
	walk = func(current Ref) {
		if targets := id.redirects[current]; targets != nil {
			for _, t := range targets {
				walk(t)
			}
		} else if !slices.Contains(terminals, current) {
			terminals = append(terminals, current)
		}
	}
	walk(ref)
	return terminals, nil
}

func (id *Identity) reaches(goal, start Ref) bool {
	if start == goal {
		return true
	}
	return slices.ContainsFunc(id.redirects[start], func(t Ref) bool { return id.reaches(goal, t) })
}
