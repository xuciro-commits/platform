package transport

import (
	"net/http"

	"google.golang.org/protobuf/encoding/protojson"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func Answering[M, T, R any](f func(*http.Request, M, T) (R, *kernel.Error)) func(http.ResponseWriter, *http.Request, M, T) {
	return func(w http.ResponseWriter, r *http.Request, m M, t T) {
		out, refusal := f(r, m, t)
		Answer(w, out, refusal)
	}
}

func AnsweringOr[M, T, R any](failure int, f func(*http.Request, M, T) (R, error)) func(http.ResponseWriter, *http.Request, M, T) {
	return func(w http.ResponseWriter, r *http.Request, m M, t T) {
		out, err := f(r, m, t)
		AnswerOr(w, failure, out, err)
	}
}

func Bounded[M, T, B, R any](limit int64, message string, f func(*http.Request, M, T, B) (R, *kernel.Error)) func(http.ResponseWriter, *http.Request, M, T) {
	return func(w http.ResponseWriter, r *http.Request, m M, t T) {
		var body B
		if DecodeBodyLimit(r, &body, limit) != nil {
			Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, message))
			return
		}
		out, refusal := f(r, m, t, body)
		Answer(w, out, refusal)
	}
}

func DecodeSubmission(body []byte, correctionOf string) (*pb.Submission, *kernel.Error) {
	sub := &pb.Submission{}
	if protojson.Unmarshal(body, sub) != nil {
		return sub, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if correctionOf != "" {
		correlation := "correction:" + correctionOf
		if sub.CorrelationId != "" && sub.CorrelationId != correlation {
			return sub, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The correction source differs from the submitted identity")
		}
		sub.CorrelationId = correlation
	}
	return sub, nil
}

func SignInOf(issuer, client string, development bool, identities func() []platform.LoginIdentity, mint func(string) string) SignIn {
	out := SignIn{}
	if issuer != "" {
		out.Issuer, out.Client = issuer, client
	} else if development {
		out.Identities = append([]platform.LoginIdentity{}, identities()...)
		if mint != nil {
			for i := range out.Identities {
				out.Identities[i].Token = mint(out.Identities[i].Token)
			}
		}
	}
	return out
}
