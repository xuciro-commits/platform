package kernel

import (
	"slices"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

type Ref struct{ Type, ID string }

func RefOf(r *pb.EntityRef) Ref { return Ref{r.GetType(), r.GetId()} }

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

func (id *Identity) Create(r *pb.EntityRef) *Error {
	ref := RefOf(r)
	if ref.Type == "" || ref.ID == "" {
		return errorf(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
	if id.entities[ref] {
		return errorf(pb.ErrorCode_ERROR_CODE_CONFLICT)
	}
	id.entities[ref] = true
	return nil
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
		return errorf(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	case !id.entities[from]:
		return errorf(pb.ErrorCode_ERROR_CODE_NOT_FOUND)
	case slices.ContainsFunc(to, func(t Ref) bool { return !id.entities[t] }):
		return errorf(pb.ErrorCode_ERROR_CODE_INVALID_REFERENCE)
	case id.redirects[from] != nil:
		return errorf(pb.ErrorCode_ERROR_CODE_CONFLICT)
	case slices.ContainsFunc(to, func(t Ref) bool { return id.reaches(from, t) }):
		return errorf(pb.ErrorCode_ERROR_CODE_REDIRECT_CYCLE)
	}
	id.redirects[from] = to
	return nil
}

func (id *Identity) Resolve(r *pb.EntityRef) ([]Ref, *Error) {
	ref := RefOf(r)
	if !id.entities[ref] {
		return nil, errorf(pb.ErrorCode_ERROR_CODE_NOT_FOUND)
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
