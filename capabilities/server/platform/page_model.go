package platform

import (
	"encoding/json"
	"fmt"
	"strings"
)

func modelRef(typ string) AssetRef {
	app, _, _ := strings.Cut(typ, ".")
	return AssetRef{App: app, Kind: AssetObject, Name: typ}
}

func (b PageWidgetSpec) IsZero() bool { return b.Format == "" && len(b.Body) == 0 }

func (p Page) CheckEnvelope() error {
	if len(p.DataBindings) > 128 || len(p.ActionTriggers) > 256 || len(p.ScopeVisibility.Scopes) > 512 || len(p.WidgetSpec.Body) > 2<<20 || !json.Valid(p.WidgetSpec.Body) {
		return fmt.Errorf("page safety declaration exceeds its bounded format")
	}
	return nil
}
