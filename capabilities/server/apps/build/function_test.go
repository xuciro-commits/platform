package build

import (
	"encoding/json"
	"slices"
	"testing"

	"platformserver/platform"
)

func TestFunctionImageRetainsOnlyValidPublishedVersions(t *testing.T) {
	v1 := Function{Record: platform.Record{ID: "F"}, Name: "advice", Title: "Advice", Description: "Review a record",
		Object: "build.intake", Fields: []string{"note"}, Instructions: "Only use the provided note",
		Output:        []platform.Field{{Name: "summary", Type: "string", Required: true, Description: "A factual summary"}},
		MaxInputBytes: 1024, MaxOutputBytes: 256, MaxTokens: 64, Roles: []string{User}, State: "published", Version: 1}
	v2 := v1
	v2.Version, v2.Instructions = 2, "A changed prompt"
	family := v2
	family.Name, family.Object, family.Instructions = "unfinished", "build.missing", "Invalid current draft"
	family.Published, family.Versions = published(v2), []string{published(v1), published(v2)}
	image := func(f Function) []byte { raw, _ := json.Marshal(f); return raw }
	if latest, err := functionImage(image(family)); err != nil || latest.Name != v2.Name || latest.Instructions != v2.Instructions {
		t.Fatalf("draft replaced the installed version: %+v %v", latest, err)
	}
	for _, tc := range []struct {
		name string
		edit func(*Function)
	}{
		{"missing version", func(f *Function) { f.Versions = f.Versions[:1] }},
		{"wrong pointer", func(f *Function) { f.Published = f.Versions[0] }},
		{"changed source", func(f *Function) {
			bad := v2
			bad.Object = "build.other"
			f.Versions[1], f.Published = published(bad), published(bad)
		}},
		{"invalid budget", func(f *Function) {
			bad := v2
			bad.MaxTokens = 0
			f.Versions[1], f.Published = published(bad), published(bad)
		}},
		{"skipped ordinal", func(f *Function) {
			bad := v2
			bad.Version = 3
			f.Versions[1], f.Published = published(bad), published(bad)
		}},
		{"recursive publication", func(f *Function) {
			bad := v2
			bad.Versions = []string{published(v1)}
			raw, _ := json.Marshal(bad)
			f.Versions[1], f.Published = string(raw), string(raw)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := family
			f.Versions = slices.Clone(family.Versions)
			tc.edit(&f)
			if _, err := functionImage(image(f)); err == nil {
				t.Fatal("accepted an invalid saved version family")
			}
		})
	}
}
