package protocol

import (
	"slices"
	"strconv"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/values"
	"platformserver/platform"
)

type Ref struct{ Type, ID string }

func RefOf(r *pb.EntityRef) Ref { return Ref{r.GetType(), r.GetId()} }

func (r Ref) key() string { return strconv.Quote(r.Type) + r.ID }

type Identity struct {
	entities  values.Index[Ref]
	redirects values.Index[RedirectState]
}

func NewIdentity(entities []*pb.EntityRef) *Identity {
	id := &Identity{}
	for _, e := range entities {
		ref := RefOf(e)
		id.entities.Set(ref.key(), ref)
	}
	return id
}

func (id *Identity) Create(r *pb.EntityRef) *platform.Error {
	ref := RefOf(r)
	if ref.Type == "" || ref.ID == "" {
		return errorf(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
	if id.entities.Has(ref.key()) {
		return errorf(pb.ErrorCode_ERROR_CODE_CONFLICT)
	}
	id.entities.Set(ref.key(), ref)
	return nil
}

func (id *Identity) AddRedirect(r *pb.Redirect) *platform.Error {
	from, to := RefOf(r.GetFrom()), make([]Ref, 0, len(r.GetTo()))
	for _, t := range r.GetTo() {
		to = append(to, RefOf(t))
	}
	switch {
	case r.GetKind() == pb.RedirectKind_REDIRECT_KIND_MERGE && len(to) != 1,
		r.GetKind() == pb.RedirectKind_REDIRECT_KIND_SPLIT && len(to) < 2,
		r.GetKind() == pb.RedirectKind_REDIRECT_KIND_UNSPECIFIED:
		return errorf(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	case !id.entities.Has(from.key()):
		return errorf(pb.ErrorCode_ERROR_CODE_NOT_FOUND)
	case slices.ContainsFunc(to, func(t Ref) bool { return !id.entities.Has(t.key()) }):
		return errorf(pb.ErrorCode_ERROR_CODE_INVALID_REFERENCE)
	case id.redirects.Get(from.key()).To != nil:
		return errorf(pb.ErrorCode_ERROR_CODE_CONFLICT)
	case slices.ContainsFunc(to, func(t Ref) bool { return id.reaches(from, t) }):
		return errorf(pb.ErrorCode_ERROR_CODE_REDIRECT_CYCLE)
	}
	id.redirects.Set(from.key(), RedirectState{From: from, To: to})
	return nil
}

func (id *Identity) Resolve(r *pb.EntityRef) ([]Ref, *platform.Error) {
	ref := RefOf(r)
	if !id.entities.Has(ref.key()) {
		return nil, errorf(pb.ErrorCode_ERROR_CODE_NOT_FOUND)
	}
	var terminals []Ref
	var walk func(Ref)
	walk = func(current Ref) {
		if targets := id.redirects.Get(current.key()).To; targets != nil {
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
	return slices.ContainsFunc(id.redirects.Get(start.key()).To, func(t Ref) bool { return id.reaches(goal, t) })
}
