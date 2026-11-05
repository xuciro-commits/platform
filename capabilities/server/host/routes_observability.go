package host

import (
	"context"
	"errors"
	"net/http"

	"platformserver/kernel/transport"
)

func (h *Host) routesObservability(rt *routes) {
	rt.public(transport.Route{Pattern: "GET /readyz", Summary: "Whether this process can serve tenant reads from its journals and operational store"}, func(w http.ResponseWriter, r *http.Request) {
		for _, t := range h.currentTenants() {
			if err := t.storageReady(r.Context()); err != nil {
				WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
				return
			}
		}
		WriteJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
}

func (t *Tenant) storageReady(ctx context.Context) error {
	if t.store() == nil {
		return errors.New("operational store is unavailable")
	}
	if t.Store == nil {
		return nil
	}
	journal, ok := t.Store.(Journals)
	if !ok {
		return errors.New("tenant journal is unavailable")
	}
	if _, err := journal.Entries(ctx, t.ID, journal.Position(t.ID)); err != nil {
		return err
	}
	return nil
}
