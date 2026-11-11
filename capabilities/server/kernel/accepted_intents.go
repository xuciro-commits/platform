package kernel

import (
	"fmt"
	"strings"

	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
)

func CheckIntents(tenant string, intents []platform.Effect, known func(app string) bool, queued func(id string) bool) error {
	ids := map[string]bool{}
	for _, x := range intents {
		if err := platform.CheckApplicationBindings(x.Applications); err != nil {
			return err
		}
		kind, ok := strings.CutPrefix(x.Event, x.App+"/")
		id := fmt.Sprintf("%s:%s:%s:%s:%s", tenant, x.App, kind, x.Key, x.Endpoint)
		if kind == "model" && x.Endpoint == acceptedformat.ModelEndpoint {
			id = fmt.Sprintf("%s:%s:model:%s", tenant, x.App, x.Key)
		}
		if !ok || kind == "" || x.App == "" || !known(x.App) || x.Key == "" || x.Endpoint == "" ||
			x.ID != id ||
			ids[x.ID] || x.State != "pending" && x.State != "held" || x.At.IsZero() || !x.Due.Equal(x.At) ||
			x.Attempts != 0 || !x.Last.IsZero() || x.Error != "" || x.Digest != "" ||
			x.State == "held" && x.Agent == "" || queued(x.ID) {
			return fmt.Errorf("accepted result has an invalid or duplicate outbound intent")
		}
		ids[x.ID] = true
	}
	return nil
}
