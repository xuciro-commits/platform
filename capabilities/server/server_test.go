package platformserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

type who struct{ Tenant string }

func (w who) TenantID() string    { return w.Tenant }
func (w who) PrincipalID() string { return "p-" + w.Tenant }

func TestKernelEndpoints(t *testing.T) {
	s := New(map[string]string{"t-1": "tenant one"}, Tokens(map[string]who{"good": {"t-1"}, "orphan": {"t-9"}}))
	s.Kernel(func(_ who, _ string, sub *pb.Submission, _ time.Time) (*pb.ChangeRecord, *kernel.Error) {
		if sub.GetIdempotencyKey() == "conflict" {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
		return &pb.ChangeRecord{ChangeId: "chg-1", Submission: sub}, nil
	}, func(string) []*pb.AuthorityDeclaration {
		return []*pb.AuthorityDeclaration{{TenantId: "t-1", Epoch: 1}}
	})
	call := func(method, path, token, body string) (int, string) {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec.Code, strings.TrimSpace(rec.Body.String())
	}
	for _, c := range []struct {
		method, path, token, body string
		status                    int
		contains                  string
	}{
		{"POST", "/v1/submissions", "good", `{"idempotencyKey":"k"}`, 200, `"changeId":"chg-1"`},
		{"POST", "/v1/submissions", "good", `{"idempotencyKey":"conflict"}`, 409, `ERROR_CODE_CONFLICT`},
		{"POST", "/v1/submissions", "good", `{"unknownField":1}`, 400, `ERROR_CODE_INVALID_ARGUMENT`},
		{"POST", "/v1/submissions", "nobody", `{}`, 401, ``},
		{"GET", "/v1/me", "orphan", ``, 401, ``},
		{"GET", "/v1/declarations", "good", ``, 200, `"epoch":1`},
		{"GET", "/v1/me", "good", ``, 200, `"principalId":"p-t-1","profile":{"Tenant":"t-1"},"tenantId":"t-1"`},
		{"OPTIONS", "/v1/submissions", "", ``, http.StatusNoContent, ``},
	} {
		status, body := call(c.method, c.path, c.token, c.body)
		if status != c.status || !strings.Contains(body, c.contains) {
			t.Errorf("%s %s as %q: %d %s", c.method, c.path, c.token, status, body)
		}
	}
}
