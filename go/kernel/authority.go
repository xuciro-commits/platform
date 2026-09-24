package kernel

import (
	"slices"

	"google.golang.org/protobuf/proto"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

// Authorities implements K5 (Contract/spec/K5-authority.md): declarations, the
// receiver's check, and the outbox of one edge.
type Authorities struct {
	edge    string
	current map[[2]string]*pb.AuthorityDeclaration // (tenant, data class)
	outbox  map[[2]string]*outboxEntry             // (tenant, idempotency key)
}

type outboxEntry struct {
	submission *pb.Submission
	state      pb.SubmissionState
}

// transitions lists, per event, the allowed from → to states (A5).
var transitions = map[string]map[pb.SubmissionState]pb.SubmissionState{
	"send":     {pb.SubmissionState_SUBMISSION_STATE_PENDING: pb.SubmissionState_SUBMISSION_STATE_SENDING},
	"confirm":  {pb.SubmissionState_SUBMISSION_STATE_SENDING: pb.SubmissionState_SUBMISSION_STATE_CONFIRMED},
	"conflict": {pb.SubmissionState_SUBMISSION_STATE_SENDING: pb.SubmissionState_SUBMISSION_STATE_CONFLICT},
	"reject":   {pb.SubmissionState_SUBMISSION_STATE_SENDING: pb.SubmissionState_SUBMISSION_STATE_REJECTED},
	"timeout":  {pb.SubmissionState_SUBMISSION_STATE_SENDING: pb.SubmissionState_SUBMISSION_STATE_UNKNOWN},
	"retry":    {pb.SubmissionState_SUBMISSION_STATE_UNKNOWN: pb.SubmissionState_SUBMISSION_STATE_SENDING},
}

func NewAuthorities(edge string) *Authorities {
	return &Authorities{edge: edge, current: map[[2]string]*pb.AuthorityDeclaration{}, outbox: map[[2]string]*outboxEntry{}}
}

func (a *Authorities) Declare(d *pb.AuthorityDeclaration) *Error {
	if slices.Contains([]string{d.GetTenantId(), d.GetDataClass(), d.GetAuthorityId()}, "") ||
		d.GetKind() == pb.AuthorityKind_AUTHORITY_KIND_UNSPECIFIED {
		return errorf(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT) // A1
	}
	key := [2]string{d.GetTenantId(), d.GetDataClass()}
	if d.GetEpoch() != a.current[key].GetEpoch()+1 {
		return errorf(pb.ErrorCode_ERROR_CODE_CONFLICT) // A2
	}
	a.current[key] = d
	return nil
}

func (a *Authorities) declaration(s *pb.Submission) (*pb.AuthorityDeclaration, *Error) {
	d := a.current[[2]string{s.GetTenantId(), s.GetTarget().GetType()}]
	if d == nil {
		return nil, errorf(pb.ErrorCode_ERROR_CODE_NOT_FOUND)
	}
	return d, nil
}

// Authorize is the receiver's check before K4 accepts a submission (A3).
func (a *Authorities) Authorize(s *pb.Submission) *Error {
	d, err := a.declaration(s)
	if err == nil && s.GetAuthority() != d.GetAuthorityId() {
		err = errorf(pb.ErrorCode_ERROR_CODE_NOT_AUTHORITY)
	}
	return err
}

// Enqueue puts a submission in this edge's outbox (A4, A6).
func (a *Authorities) Enqueue(s *pb.Submission) (pb.SubmissionState, *Error) {
	d, err := a.declaration(s)
	if err != nil {
		return 0, err
	}
	key := [2]string{s.GetTenantId(), s.GetIdempotencyKey()}
	if existing := a.outbox[key]; existing != nil {
		if !proto.Equal(existing.submission, s) {
			return 0, errorf(pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT)
		}
		return existing.state, nil
	}
	state := pb.SubmissionState_SUBMISSION_STATE_PENDING
	if d.GetAuthorityId() == a.edge {
		state = pb.SubmissionState_SUBMISSION_STATE_CONFIRMED
	}
	a.outbox[key] = &outboxEntry{submission: s, state: state}
	return state, nil
}

func (a *Authorities) Transition(tenant, idempotencyKey, event string) (pb.SubmissionState, *Error) {
	entry := a.outbox[[2]string{tenant, idempotencyKey}]
	if entry == nil {
		return 0, errorf(pb.ErrorCode_ERROR_CODE_NOT_FOUND)
	}
	next, ok := transitions[event][entry.state]
	if !ok {
		return 0, errorf(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT) // A5
	}
	entry.state = next
	return next, nil
}
