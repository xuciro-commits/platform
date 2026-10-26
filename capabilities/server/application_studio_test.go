package platformserver

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"platformkernel/kernel"
	"platformserver/platform"
)

type studioReadOwner struct{ *notes }

func (a studioReadOwner) Read(platform.Caller, string) (any, *kernel.Error) { return a.id, nil }

func TestApplicationStudioScopedReadsKeepOriginalOwnerAuthorization(t *testing.T) {
	tn := composeTenant(t, "studio-reads", []Seat{seatOf("both", "a:writer", "b:writer"), seatOf("onlya", "a:writer")}, studioReadOwner{newNotes("studio-reads", "a")}, studioReadOwner{newNotes("studio-reads", "b")})
	handler := NewHost(Tokens(map[string]string{"both": "both", "onlya": "onlya"}), tn).Handler()
	for _, tc := range []struct {
		app, member, answer string
		status              int
	}{{"a", "both", `"a"`, http.StatusOK}, {"b", "both", `"b"`, http.StatusOK}, {"b", "onlya", "", http.StatusForbidden}, {"missing", "both", "", http.StatusNotFound}} {
		request := httptest.NewRequest(http.MethodGet, "/v1/apps/"+tc.app+"/reads/"+tc.app+"-notes", nil)
		request.Header.Set("Authorization", "Bearer "+tc.member)
		reply := httptest.NewRecorder()
		handler.ServeHTTP(reply, request)
		if reply.Code != tc.status || tc.answer != "" && strings.TrimSpace(reply.Body.String()) != tc.answer {
			t.Fatalf("%s as %s: %d %s", tc.app, tc.member, reply.Code, reply.Body.String())
		}
	}
	if _, err := tn.ReadFrom(memberOf(t, tn, "both"), "a", "b-notes"); err == nil {
		t.Fatal("a scoped read selected a different app's read")
	}
}

type studioEmptyOwner struct{ *notes }

func (a studioEmptyOwner) Manifest() platform.Manifest {
	return platform.Manifest{ID: a.id, Title: "Empty native application", Version: "1", Actions: platform.NewCatalog(), Roles: []string{"reader"}}
}

func TestApplicationStudioRetainsEmptyNativeApplicationIdentity(t *testing.T) {
	owner := studioEmptyOwner{&notes{id: "empty", ledger: platform.NewLedger("empty-studio", "empty", platform.NewCatalog()), texts: map[string]string{}}}
	tn := composeTenant(t, "empty-studio", []Seat{seatOf("reader", "empty:reader"), seatOf("other", "platform:admin")}, owner)
	ref := platform.AssetRef{App: "empty", Kind: platform.AssetApp, Name: "empty"}
	definitions := tn.Definitions(memberOf(t, tn, "reader"))
	i := slices.IndexFunc(definitions, func(definition platform.Definition) bool { return definition.Ref == ref })
	if i < 0 || definitions[i].Application == nil || len(definitions[i].Application.Pages) != 0 || len(definitions[i].Application.Resources) != 0 {
		t.Fatal("an empty native application lost its identity")
	}
	if slices.ContainsFunc(tn.Definitions(memberOf(t, tn, "other")), func(definition platform.Definition) bool { return definition.Ref == ref }) {
		t.Fatal("empty application discovery granted another member structure access")
	}
	if candidate, err := tn.ReleaseCandidate([]platform.AssetRef{ref}); err != nil || len(candidate.Assets) != 1 || candidate.Assets[0].Ref != ref {
		t.Fatalf("empty application cannot be an original release prerequisite: %+v %v", candidate, err)
	}
}
