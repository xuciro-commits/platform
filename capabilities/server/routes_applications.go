package platformserver

import (
	"encoding/json"
	"io"
	"net/http"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

// The application runtime: one call opens any application, code or model.
func (h *Host) routesApplications(rt *routes) {
	rt.metadata(Route{Pattern: "GET /v1/applications", Summary: "Every application this member may open, with the pages each offers", Answer: []ApplicationView{}},
		func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
			WriteJSON(w, http.StatusOK, t.i18n.Translate(t.Applications(m), t.Language(m, r)))
		})
	rt.metadata(Route{Pattern: "POST /v1/page-view", Summary: "One page of an application resolved for the caller: its object, fields and the actions they may take", Body: PageRequest{}, Answer: PageView{}},
		func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
			var body PageRequest
			decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&body) != nil || decoder.Decode(new(any)) != io.EOF || body.App == "" || body.Page == "" {
				Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A page view names one application and one page"))
				return
			}
			view, refusal := t.PageView(m, body.App, body.Owner, body.Page)
			if refusal != nil {
				Reply(w, nil, refusal)
				return
			}
			WriteJSON(w, http.StatusOK, t.i18n.Translate(view, t.Language(m, r)))
		})
}

// PageRequest names the page to open: the application, the registry owner when
// two applications share a name, and the page.
type PageRequest struct {
	App   string `json:"app"`
	Owner string `json:"owner,omitempty"`
	Page  string `json:"page"`
}
