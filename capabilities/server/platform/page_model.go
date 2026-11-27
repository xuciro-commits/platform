package platform

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
)

func modelRef(typ string) AssetRef {
	return AssetRef{App: OwnerOfType(typ), Kind: AssetObject, Name: typ}
}

var (
	hostedApps       sync.Map
	hostedGeneration atomic.Uint64
)

func HostApp(id string) {
	if _, loaded := hostedApps.LoadOrStore(id, true); !loaded {
		hostedGeneration.Add(1)
	}
}

func HostedGeneration() uint64 { return hostedGeneration.Load() }

func OwnerOfType(typ string) string {
	app, _, _ := strings.Cut(typ, ".")
	if _, hosted := hostedApps.Load(app); hosted || builtinManifests[app] != "" {
		return app
	}
	return "build"
}

func (b PageWidgetSpec) IsZero() bool { return b.Format == "" && len(b.Body) == 0 }

func (p Page) CheckEnvelope() error {
	if len(p.DataBindings) > 128 || len(p.ActionTriggers) > 256 || len(p.ScopeVisibility.Scopes) > 512 || len(p.WidgetSpec.Body) > 2<<20 || !json.Valid(p.WidgetSpec.Body) {
		return fmt.Errorf("page safety declaration exceeds its bounded format")
	}
	return nil
}
