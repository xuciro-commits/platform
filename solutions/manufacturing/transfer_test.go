package manufacturing

import (
	"strings"
	"testing"
	"time"

	"erp"

	"platformserver"
	"platformserver/platform"
)

// ADR-0028 11e: products imported into the ERP from CSV — previewed, then
// imported; the same file twice creates them once — and exported back.
func TestImportExport(t *testing.T) {
	var journal []platformserver.Entry
	build := func() *platformserver.Tenant {
		tn, err := NewTenant(tenant, erp.New(tenant), seats...)
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := build()
	tn.Record = func(e platformserver.Entry) { journal = append(journal, e) }
	now := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	sup, _ := tn.Member("sup-1")
	file := []byte("id,name,kind,unit,cost\nP-901,Hex bolt,material,pcs,0.30 CNY\nP-902,Washer,material,pcs,0.05 CNY\nP-903,Bad,metal,pcs,1\n")
	outcomes := func(rows []platformserver.ImportRow) string {
		var out []string
		for _, r := range rows {
			out = append(out, r.ID+" "+strings.TrimPrefix(r.Action, erp.ProductType+".")+" "+r.Outcome)
		}
		return strings.Join(out, ", ")
	}
	count := func() int {
		page, _ := tn.Records(sup, erp.ProductType, platform.Query{Domain: []byte(`[["id","like","P-90"]]`)}, now)
		return page.Total
	}
	preview, err := tn.Import(sup, erp.ProductType, file, true, now)
	if err != nil || outcomes(preview) != "P-901 create ok, P-902 create ok, P-903 create ERROR_CODE_INVALID_ARGUMENT" || count() != 0 {
		t.Fatalf("preview: %v %s, %d products", err, outcomes(preview), count())
	}
	rows, _ := tn.Import(sup, erp.ProductType, file, false, now)
	if outcomes(rows) != "P-901 create ok, P-902 create ok, P-903 create ERROR_CODE_INVALID_ARGUMENT" || count() != 2 {
		t.Fatalf("import: %s, %d products", outcomes(rows), count())
	}
	revision := func() uint32 {
		v, _ := tn.RecordOf(sup, erp.ProductType, "P-901", now)
		return v.Record.(erp.Product).Revision
	}
	again, _ := tn.Import(sup, erp.ProductType, file, false, now) // the same file again: its rows answer as they did, nothing new is decided
	if count() != 2 || outcomes(again) != outcomes(rows) || revision() != 1 {
		t.Fatalf("again: %s, %d products", outcomes(again), count())
	}
	// Another file edits what exists.
	edits, _ := tn.Import(sup, erp.ProductType, []byte("id,name\nP-901,Hex bolt M6\n"), false, now)
	if outcomes(edits) != "P-901 edit ok" {
		t.Fatalf("edit: %s", outcomes(edits))
	}
	csv, _ := tn.Export(sup, erp.ProductType, platform.Query{Domain: []byte(`[["id","like","P-90"]]`), Sort: []string{"id"}}, now)
	if !strings.Contains(string(csv), "P-901,Hex bolt M6,material,pcs,0.30 CNY") || !strings.HasPrefix(string(csv), "id,name,kind,unit,cost") {
		t.Fatalf("export:\n%s", csv)
	}
	platformserver.CheckReplay(t, tn, journal, build)
}
