package platformserver

import (
	"fmt"
	"slices"
	"strings"

	"platformserver/platform"
)

func (t *Tenant) validateAcceptedIntents(intents []platform.Effect) error {
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	ids := map[string]bool{}
	for _, x := range intents {
		kind, ok := strings.CutPrefix(x.Event, x.App+"/")
		id := fmt.Sprintf("%s:%s:%s:%s:%s", t.ID, x.App, kind, x.Key, x.Endpoint)
		if kind == "model" && x.Endpoint == modelEndpoint {
			id = fmt.Sprintf("%s:%s:model:%s", t.ID, x.App, x.Key)
		}
		if !ok || kind == "" || x.App == "" || t.app(x.App) == nil || x.Key == "" || x.Endpoint == "" ||
			x.ID != id ||
			ids[x.ID] || x.State != "pending" && x.State != "held" || x.At.IsZero() || !x.Due.Equal(x.At) ||
			x.Attempts != 0 || !x.Last.IsZero() || x.Error != "" || x.Digest != "" ||
			x.State == "held" && x.Agent == "" || slices.ContainsFunc(t.outbound, func(e *effect) bool { return e.ID == x.ID }) {
			return fmt.Errorf("accepted result has an invalid or duplicate outbound intent")
		}
		ids[x.ID] = true
	}
	return nil
}

func (t *Tenant) installAcceptedIntents(intents []platform.Effect) {
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	for _, planned := range intents {
		t.outbound = append(t.outbound, &effect{Effect: planned, span: t.current()})
	}
	t.trimEffects()
}
