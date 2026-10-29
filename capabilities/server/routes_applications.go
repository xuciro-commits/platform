package platformserver

import transport "platformserver/kernel/transport"

import (
	"encoding/json"
	"io"
	"net/http"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

func (h *Host) routesApplications(rt *routes) {
	rt.metadata(transport.Route{Pattern: "GET /v1/applications", Summary: "Every application this member may open, with the pages each offers", Answer: []platform.ApplicationView{}},
		func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
			WriteJSON(w, http.StatusOK, t.i18n.Translate(t.Applications(m), t.Language(m, r)))
		})
	rt.metadata(transport.Route{Pattern: "POST /v1/page-view", Summary: "One page of an application resolved for the caller: its object, fields and the actions they may take", Body: PageRequest{}, Answer: platform.PageView{}},
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

type PageRequest struct {
	App   string `json:"app"`
	Owner string `json:"owner,omitempty"`
	Page  string `json:"page"`
}
