package platformserver_test

import (
	"fmt"
	"testing"

	"platformserver"
	"platformserver/composition"
	"platformserver/platform"
)

func benchTenant(b *testing.B) (*platformserver.Tenant, platform.Member) {
	b.Helper()
	seats := []platformserver.Seat{{Subjects: []string{"dev"}, Member: platform.Member{ID: "builder", Roles: map[string]string{
		"platform": "admin", "build": "builder", "core": "steward", "ai": "admin", "work": "admin", "agent": "admin",
	}}}}
	t, err := composition.NewTenant("bench", seats...)
	if err != nil {
		b.Fatal(err)
	}
	m, ok := t.Member("builder")
	if !ok {
		b.Fatal("no member")
	}
	return t, m
}

func BenchmarkCatalog(b *testing.B) {
	t, m := benchTenant(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := t.Catalog(m); len(got) == 0 {
			b.Fatal("empty")
		}
	}
}

func BenchmarkEntities(b *testing.B) {
	t, m := benchTenant(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := t.Entities(m); len(got) == 0 {
			b.Fatal("empty")
		}
	}
}

func BenchmarkDefinitions(b *testing.B) {
	t, m := benchTenant(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := t.Definitions(m); len(got) == 0 {
			b.Fatal("empty")
		}
	}
}

func BenchmarkTranslateCatalog(b *testing.B) {
	t, m := benchTenant(b)
	catalog := t.Catalog(m)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = fmt.Sprint(len(catalog))
	}
}
