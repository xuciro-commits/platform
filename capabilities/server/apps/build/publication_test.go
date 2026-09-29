package build

import (
	"encoding/json"
	"strings"
	"testing"

	"platformserver/platform"
)

func TestPublishedImageKeepsOnlyTheCurrentDefinition(t *testing.T) {
	for _, initial := range []struct {
		name string
		next func(string) string
	}{
		{"object", func(previous string) string {
			return published(Object{Name: "visit", Published: previous})
		}},
		{"page", func(previous string) string {
			return published(Page{Name: "visits", Published: previous})
		}},
		{"application", func(previous string) string {
			return published(Application{Name: "frontdesk", Published: previous})
		}},
		{"function", func(previous string) string {
			return published(Function{Name: "advice", Published: previous, Versions: []string{previous}})
		}},
		{"process", func(previous string) string {
			return published(Process{Name: "review", Published: previous, Versions: []string{previous}})
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
	v1 := Process{Record: platform.Record{ID: "P"}, Name: "review", Object: "build.visit", Version: 1}
	v2 := v1
	v2.Version = 2
	family := Process{Record: v1.Record, Name: "unfinished", Object: "build.missing", State: "published", Version: 2, Published: published(v2), Versions: []string{published(v1), published(v2)}}
	image := func(p Process) []byte { raw, _ := json.Marshal(p); return raw }
	if latest, err := processImage(image(family)); err != nil || latest.Name != "review" || latest.Version != 2 {
		t.Fatalf("draft replaced the saved family: %+v %v", latest, err)
	}
	for _, corrupt := range []struct {
		name string
		edit func(*Process)
	}{
		{"missing version", func(p *Process) { p.Versions = p.Versions[:1] }},
		{"wrong published pointer", func(p *Process) { p.Published = p.Versions[0] }},
		{"changed identity", func(p *Process) {
			bad := v2
			bad.Name = "other"
			p.Versions[1] = published(bad)
			p.Published = p.Versions[1]
		}},
		{"skipped version", func(p *Process) {
			bad := v2
			bad.Version = 3
			p.Versions[1] = published(bad)
			p.Published = p.Versions[1]
		}},
		{"recursive image", func(p *Process) {
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
