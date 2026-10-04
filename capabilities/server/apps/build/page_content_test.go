package build

import (
	"fmt"
	"platformserver/platform"
	"testing"
)

func TestPagePublicationHistoryIsBoundedDistinctAndNonrecursive(t *testing.T) {
	p := Page{Record: platform.Record{ID: "page"}, Name: "child", Object: "build.note", Sections: []Section{{ID: "text", Widget: "text", Text: "first", ConfigVersion: 1}}}
	if err := retainPagePublication(&p); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := retainPagePublication(&p); err != nil {
			t.Fatal(err)
		}
	}
	if len(p.Versions) != 1 {
		t.Fatal("equal descriptor copies grew history")
	}
	// Names are part of content identity. A renamed current page keeps its
	// prior owner snapshots without treating them as the new named page.
	rename := p
	rename.Name = "renamed"
	if err := retainPagePublication(&rename); err != nil || len(rename.Versions) != 2 {
		t.Fatal("existing owner rename was blocked by history", err)
	}
	for i := 1; i < 64; i++ {
		p.Sections[0].Text = fmt.Sprint(i)
		if err := retainPagePublication(&p); err != nil {
			t.Fatal(err)
		}
	}
	if len(p.Versions) != 64 {
		t.Fatal("missing distinct contents")
	}
	for _, raw := range p.Versions {
		saved, ok := wasPublished[Page](raw)
		if !ok || saved.Published != "" || len(saved.Versions) != 0 {
			t.Fatal("recursive publication snapshot")
		}
	}
	p.Sections[0].Text = "overflow"
	old := p.Published
	if retainPagePublication(&p) == nil || len(p.Versions) != 64 || p.Published != old {
		t.Fatal("history budget mutated original publication")
	}
}
