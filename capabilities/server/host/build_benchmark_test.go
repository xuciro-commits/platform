package host

import (
	build "platformserver/services/resources/authoring"
	"strconv"
	"testing"
)

func BenchmarkBuildSubmit(b *testing.B) {
	t := newInvariantTenant(b, "bench")
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		id := "folder" + strconv.Itoa(i)
		t.accept(b, build.FolderType+".create", build.FolderType, id, map[string]any{"name": id, "project": t.project})
	}
}
