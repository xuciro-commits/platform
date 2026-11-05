package host

import (
	"strings"
	"testing"

	"platformserver/kernel/systemscope"
	"platformserver/platform"
	build "platformserver/services/resources/authoring"
)

func TestInvariantSystemReleaseRecordsAreReadOnly(t *testing.T) {
	live := newInvariantTenant(t, systemscope.Tenant)
	drafts := []build.JointDraftRef{{Kind: platform.AssetObject, ID: "OBJ1"}}
	if _, err := live.SaveReleaseCandidates(live.member(t), drafts, "system-candidate", "system-save", invariantClock); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("system release save: %v", err)
	}
	if live.releases.Raw("system-candidate") != nil {
		t.Fatal("a refused system candidate was retained")
	}
	if _, err := live.ActivateRelease(live.member(t), "system-candidate", "system-activate", invariantClock); err == nil || !strings.Contains(err.Error(), "retained review") {
		t.Fatalf("system release activation: %v", err)
	}
	if live.releases.Active() != "" {
		t.Fatalf("a refused system activation became active: %s", live.releases.Active())
	}
}
