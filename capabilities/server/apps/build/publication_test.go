package build

import "platformserver/internal/construction"

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

func TestPublishRetainedQueryKeepsHistoryAndInstallerOutcome(t *testing.T) {
	q := Query{Name: "lookup", Title: "Lookup", Object: "build.receipt", State: "published", Version: 1}
	previous := published(q)
	q.State, q.Published, q.Versions = "draft", previous, []string{previous}
	calls := 0
	var installed Query
	var failure error
	install := func(_ platform.Caller, current Query) error {
		calls++
		installed = current
		return failure
	}
	if err := publishRetainedDefinition(platform.Caller{}, &q, "query limit", install); err != nil {
		t.Fatal(err)
	}
	if q.Version != 2 || q.State != "published" || len(q.Versions) != 2 || q.Versions[0] != previous || q.Versions[1] != q.Published || calls != 1 || installed.Version != 2 || installed.State != q.State || installed.Published != q.Published {
		t.Fatal("publication lost history or did not install the new current once", q, installed, calls)
	}
	var image Query
	if err := json.Unmarshal([]byte(q.Published), &image); err != nil {
		t.Fatal(err)
	}
	if image.Version != 2 || image.State != "published" || image.Name != q.Name || image.Object != q.Object || image.Published != "" || len(image.Versions) != 0 {
		t.Fatal("published bytes lost the current definition or recursively retained history", image)
	}
	full := q
	full.Version = 64
	if err := publishRetainedDefinition(platform.Caller{}, &full, "query limit", install); err == nil || err.Code != pb.ErrorCode_ERROR_CODE_CONFLICT || err.Message != "query limit" || calls != 1 {
		t.Fatal("full version family was installed or lost its refusal", err, calls)
	}
	failure = errors.New("query installer failed")
	if err := publishRetainedDefinition(platform.Caller{}, &q, "query limit", install); err == nil || err.Code != pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT || err.Message != failure.Error() || calls != 2 || installed.Version != 3 {
		t.Fatal("installer failure was lost", err, calls, installed)
	}
}

func TestPublishedImageKeepsOnlyTheCurrentDefinition(t *testing.T) {
	for _, initial := range []struct {
		name string
		next func(string) string
	}{
		{"object", func(previous string) string {
			return published(construction.Object{Name: "visit", Published: previous})
		}},
		{"page", func(previous string) string {
			return published(Page{Name: "visits", Published: previous})
		}},
		{"application", func(previous string) string {
			return published(Application{Name: "frontdesk", Published: previous})
		}},
		{"function", func(previous string) string {
			return published(construction.Function{Name: "advice", Published: previous, Versions: []string{previous}})
		}},
		{"process", func(previous string) string {
			return published(construction.Process{Name: "review", Published: previous, Versions: []string{previous}})
		}},
	} {
		t.Run(initial.name, func(t *testing.T) {
			var previous string
			for range 20 {
				previous = initial.next(previous)
				if strings.Contains(previous, `"published"`) || strings.Contains(previous, `"versions"`) || len(previous) > 256 {
					t.Fatalf("published image recursively retained old versions (%d bytes)", len(previous))
				}
			}
		})
	}
}

func TestProcessImageValidatesSavedFamilyIndependentlyOfDraft(t *testing.T) {
	v1 := construction.Process{Record: platform.Record{ID: "P"}, Name: "review", Object: "build.visit", Version: 1}
	v2 := v1
	v2.Version = 2
	family := construction.Process{Record: v1.Record, Name: "unfinished", Object: "build.missing", State: "published", Version: 2, Published: published(v2), Versions: []string{published(v1), published(v2)}}
	image := func(p construction.Process) []byte { raw, _ := json.Marshal(p); return raw }
	if latest, err := processImage(image(family)); err != nil || latest.Name != "review" || latest.Version != 2 {
		t.Fatalf("draft replaced the saved family: %+v %v", latest, err)
	}
	for _, corrupt := range []struct {
		name string
		edit func(*construction.Process)
	}{
		{"missing version", func(p *construction.Process) { p.Versions = p.Versions[:1] }},
		{"wrong published pointer", func(p *construction.Process) { p.Published = p.Versions[0] }},
		{"changed identity", func(p *construction.Process) {
			bad := v2
			bad.Name = "other"
			p.Versions[1] = published(bad)
			p.Published = p.Versions[1]
		}},
		{"skipped version", func(p *construction.Process) {
			bad := v2
			bad.Version = 3
			p.Versions[1] = published(bad)
			p.Published = p.Versions[1]
		}},
		{"recursive image", func(p *construction.Process) {
			bad := v2
			bad.Versions = []string{published(v1)}
			raw, _ := json.Marshal(bad)
			p.Versions[1] = string(raw)
			p.Published = string(raw)
		}},
	} {
		t.Run(corrupt.name, func(t *testing.T) {
			p := family
			p.Versions = append([]string(nil), family.Versions...)
			corrupt.edit(&p)
			if _, err := processImage(image(p)); err == nil {
				t.Fatal("accepted corrupt saved versions")
			}
		})
	}
}
