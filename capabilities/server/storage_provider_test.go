package platformserver

import (
	"context"
	"strings"
	"testing"
)

func TestStorageProviderRejectsInsufficientDeliveryGuaranteesBeforeOpening(t *testing.T) {
	opened := false
	provider := FileProvider(t.TempDir())
	provider.Open = func(context.Context) (Journals, error) { opened = true; return nil, nil }
	d := Deployment{Profile: "delivery", Storage: &provider}
	if _, err := d.openJournals(context.Background()); err == nil || opened || !strings.Contains(err.Error(), "atomic current state") {
		t.Fatalf("insufficient provider reached activation: %v", err)
	}
	provider.AtomicCurrentState, provider.FixedReadViews = true, true
	provider.Open = FileProvider(t.TempDir()).Open
	if _, err := d.openJournals(context.Background()); err == nil || !strings.Contains(err.Error(), "lacks its declared") {
		t.Fatal("metadata substituted for actual storage interfaces", err)
	}
	d.Profile = "lightweight"
	provider = FileProvider(t.TempDir())
	store, err := d.openJournals(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
}
