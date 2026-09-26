package hospitality

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"csm"

	"platformserver"
	"platformserver/apps/files"
)

// ADR-0028: a guest's screenshot attached to a CSM ticket; the desk downloads
// it, a salesperson without a role in customer service does not.
func TestFilesOnTickets(t *testing.T) {
	w := newWorld(t, hotelProvider)
	w.expect(w.submit("desk", csm.ID, csm.SchemaOpen, csm.TicketType, "T-9", "t9", map[string]string{"subject": "Broken lamp", "customer": "anna@acme.test"}), "ok")
	shot, _, err := w.tenant.Upload(w.members["desk"], "lamp.png", "image/png", strings.NewReader("png bytes"), t0)
	if err != nil {
		t.Fatal(err)
	}
	w.expect(w.submit("desk", files.ID, files.SchemaAttach, files.FileType, "SHOT-1", "f1",
		map[string]any{"hash": shot.Hash, "name": shot.Name, "contentType": shot.ContentType, "size": shot.Size, "target": csm.TicketType + "/T-9"}), "ok")
	get := func(who string) string {
		r := httptest.NewRecorder()
		w.tenant.Download(r, w.members[who], "SHOT-1", t0)
		return fmt.Sprint(r.Code)
	}
	w.expect(get("desk")+" "+get("manager")+" "+get("sales-only"), "200 200 404")
	platformserver.CheckReplay(t, w.tenant, w.journal, func() *platformserver.Tenant { return newWorld(t, hotelProvider).tenant })
}
