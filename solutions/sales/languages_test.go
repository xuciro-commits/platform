package sales

import "testing"

// Every app the sales solution runs reads in Chinese (ADR-0023 6a): a title or
// description without a translation fails here.
func TestChinese(t *testing.T) {
	tn, err := NewTenant("t", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, app := range []string{"crm", "hotel", "hr", "helpdesk", "memstay"} {
		if missing := tn.Untranslated(app, "zh-CN"); len(missing) > 0 {
			t.Errorf("%s lacks Chinese for %q", app, missing)
		}
	}
}
