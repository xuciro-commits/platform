package publication

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"platformserver/platform"
)

func TestDescribeCacheAnswersSameContentOnceAndCopies(t *testing.T) {
	cache := NewDescribeCache()
	asset := platform.ReleaseAsset{Ref: platform.AssetRef{App: "crm", Kind: platform.AssetQuery, Name: "open"}, SourceVersion: "1", Body: json.RawMessage(`{"name":"open"}`)}
	calls := 0
	fresh := func() (platform.Dependencies, error) {
		calls++
		return platform.Dependencies{Ref: asset.Ref, Requires: []platform.AssetRef{{App: "crm", Kind: platform.AssetObject, Name: "crm.lead"}}, Provides: []platform.Capability{{Kind: "query", Name: "open", Roles: []string{"salesrep"}}}}, nil
	}
	first, err := cache.describe(asset, false, fresh)
	if err != nil {
		t.Fatal(err)
	}
	first.Requires[0].Name = "changed"
	first.Provides[0].Roles[0] = "changed"
	second, _ := cache.describe(asset, false, fresh)
	if calls != 1 {
		t.Fatalf("same content described %d times, want once", calls)
	}
	if second.Requires[0].Name != "crm.lead" || second.Provides[0].Roles[0] != "salesrep" {
		t.Fatalf("a caller's change reached the cached answer: %+v", second)
	}

	edited := asset
	edited.Body = json.RawMessage(`{"name":"open","by":"owner"}`)
	cache.describe(edited, false, fresh)
	cache.describe(asset, true, fresh)
	moved := asset
	moved.Requires = []platform.AssetRef{{App: "crm", Kind: platform.AssetObject, Name: "crm.deal"}}
	cache.describe(moved, false, fresh)
	if calls != 4 {
		t.Fatalf("body, historical and requires must each miss the cache: %d calls", calls)
	}

	platform.HostApp("describe-cache-test-app")
	cache.describe(asset, false, fresh)
	if calls != 5 {
		t.Fatalf("a newly hosted app must miss the cache: %d calls", calls)
	}
}

func TestDescribeCacheKeepsRefusals(t *testing.T) {
	cache := NewDescribeCache()
	asset := platform.ReleaseAsset{Ref: platform.AssetRef{App: "crm", Kind: platform.AssetQuery, Name: "bad"}, Body: json.RawMessage(`{}`)}
	calls := 0
	fresh := func() (platform.Dependencies, error) { calls++; return platform.Dependencies{}, errors.New("refused") }
	for range 2 {
		if _, err := cache.describe(asset, false, fresh); err == nil || err.Error() != "refused" {
			t.Fatalf("refusal not kept: %v", err)
		}
	}
	if calls != 1 {
		t.Fatalf("refusal described %d times, want once", calls)
	}
}

func TestDescribeWithAndWithoutCacheAgree(t *testing.T) {
	asset := platform.ReleaseAsset{Ref: platform.AssetRef{App: "crm", Kind: platform.AssetLinkType, Name: "lead-deal"}, Body: json.RawMessage(`{"name":"lead-deal"}`)}
	plain, plainErr := Resolver{}.describeAsset(asset, false)
	cached := Resolver{Describes: NewDescribeCache()}
	for range 2 {
		got, err := cached.describeAsset(asset, false)
		if (err == nil) != (plainErr == nil) || !slices.Equal(got.Requires, plain.Requires) {
			t.Fatalf("cached answer %+v %v differs from %+v %v", got, err, plain, plainErr)
		}
	}
}
