package mes

import "testing"

// The plant reads in Chinese (ADR-0023 6a): a title or description without a
// translation fails here.
func TestChinese(t *testing.T) {
	_, tn := plantTenant(t)
	if missing := tn.Untranslated("mes", "zh-CN"); len(missing) > 0 {
		t.Errorf("mes lacks Chinese for %q", missing)
	}
}
