package platform

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestCompositionSelectsProvidersAndRejectsInvalidDependencies(t *testing.T) {
	contract := CapabilityContract{ID: "test.compute", Version: "1", Semantics: "1"}
	provider := CapabilityModule{ID: "provider", Version: "1", Title: "Provider", Scope: "tenant", Replacement: "drain", Provides: []CapabilityContract{contract}}
	consumer := CapabilityModule{ID: "consumer", Version: "1", Title: "Consumer", Scope: "tenant", Replacement: "drain", Requires: []CapabilityContract{contract}}
	plan, err := ResolveComposition([]CapabilityModule{consumer, provider}, nil)
	if err != nil || !reflect.DeepEqual(plan.Order, []string{"provider", "consumer"}) {
		t.Fatalf("dependency order: %+v, %v", plan, err)
	}
	if _, err := ResolveComposition([]CapabilityModule{consumer}, nil); err == nil {
		t.Fatal("missing required provider was accepted")
	}
	reordered, err := ResolveComposition([]CapabilityModule{provider, consumer}, nil)
	if err != nil || !reflect.DeepEqual(reordered, plan) {
		t.Fatalf("registration order changed the composition: %+v, %v", reordered, err)
	}
	other := provider
	other.ID = "other"
	if _, err = ResolveComposition([]CapabilityModule{provider, other, consumer}, nil); err == nil {
		t.Fatal("ambiguous provider was selected by registration order")
	}
	plan, err = ResolveComposition([]CapabilityModule{provider, other, consumer}, map[string]string{contract.ID: "other"})
	if err != nil || plan.Bindings[0].Provider != "other" {
		t.Fatalf("explicit selection: %+v, %v", plan, err)
	}
	if !reflect.DeepEqual(plan.Order, []string{"other", "consumer"}) {
		t.Fatalf("unselected implementation would be activated: %+v", plan.Order)
	}
	consumer.Requires[0].Semantics = "2"
	if _, err = ResolveComposition([]CapabilityModule{consumer, provider}, nil); err == nil {
		t.Fatal("same contract name bypassed semantic compatibility")
	}
	consumer.Requires[0].Semantics = "1"
	consumer.Scope = "deployment"
	if _, err = ResolveComposition([]CapabilityModule{consumer, provider}, nil); err == nil {
		t.Fatal("shared provider retained a tenant resource")
	}
	consumer.Scope = "tenant"
	consumer.Provides = []CapabilityContract{{ID: "test.consumer", Version: "1", Semantics: "1"}}
	provider.Requires = consumer.Provides
	if _, err = ResolveComposition([]CapabilityModule{provider, consumer}, nil); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("cycle was not diagnosed: %v", err)
	}
}

func TestResourcesCloseRunsInversesInReverseAndRetriesFailures(t *testing.T) {
	var resources Resources
	registry := map[string]string{"other": "still active"}
	registry["owned"] = "active"
	if err := resources.Track("registration", func(context.Context) error { delete(registry, "owned"); return nil }); err != nil {
		t.Fatal(err)
	}
	fail := true
	var order []string
	if err := resources.Track("connection", func(context.Context) error {
		order = append(order, "connection")
		if fail {
			return errors.New("close failed")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := resources.Close(ctx); !errors.Is(err, context.Canceled) || registry["owned"] != "active" {
		t.Fatalf("canceled close ran an inverse: %v, %+v", err, registry)
	}
	if err := resources.Track("late", func(context.Context) error { return nil }); err == nil {
		t.Fatal("a closing scope accepted a registration")
	}
	var err error
	if err = resources.Close(context.Background()); err == nil || registry["owned"] != "active" {
		t.Fatalf("failed dependent cleanup removed its provider: %v", err)
	}
	fail = false
	if err = resources.Close(context.Background()); err != nil || len(registry) != 1 || registry["other"] != "still active" {
		t.Fatalf("cleanup changed a foreign registration: %+v, %v", registry, err)
	}
	if err = resources.Close(context.Background()); err != nil || len(order) != 2 {
		t.Fatalf("completed inverse ran again: %+v, %v", order, err)
	}
}
