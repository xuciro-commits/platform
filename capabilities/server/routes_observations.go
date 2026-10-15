package platformserver

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

func (h *Host) routesObservations(rt *routes) {
	rt.handle(Route{Pattern: "GET /v1/observations", Summary: "Declared external observation sources available through original member and model permissions", Answer: []ObservationInfo{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		answer, refusal := t.Observations(m)
		if refusal != nil {
			Reply(w, nil, refusal)
			return
		}
		WriteJSON(w, http.StatusOK, t.i18n.Translate(answer, t.Language(m, r)))
	})
	rt.handle(Route{Pattern: "POST /v1/observations/{app}/{name}/read", Summary: "Read a current protocol point or bounded external window without storing points in tenant state", Body: ObservationRead{}, Answer: platform.ObservationResult{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var request ObservationRead
		if !readObservationRequest(w, r, &request) {
			return
		}
		answer, refusal := t.ReadObservation(r.Context(), m, platform.AssetRef{App: r.PathValue("app"), Kind: platform.AssetObservation, Name: r.PathValue("name")}, request, h.Now())
		if refusal != nil {
			Reply(w, nil, t.i18n.said(refusal, t.Language(m, r)))
			return
		}
		WriteJSON(w, http.StatusOK, answer)
	})
	rt.handle(Route{Pattern: "POST /v1/observations/{app}/{name}/evaluate", Summary: "Evaluate one external summary with the shared rule language and submit only compact results through the original business action", Body: ObservationEvaluate{}, Answer: ObservationEvaluation{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var request ObservationEvaluate
		if !readObservationRequest(w, r, &request) {
			return
		}
		answer, refusal := t.EvaluateObservation(r.Context(), m, platform.AssetRef{App: r.PathValue("app"), Kind: platform.AssetObservation, Name: r.PathValue("name")}, request, h.Now())
		if refusal != nil {
			Reply(w, nil, t.i18n.said(refusal, t.Language(m, r)))
			return
		}
		WriteJSON(w, http.StatusOK, answer)
	})
}

func readObservationRequest(w http.ResponseWriter, r *http.Request, value any) bool {
	raw, err := io.ReadAll(io.LimitReader(r.Body, (16<<10)+1))
	if err != nil || len(raw) > 16<<10 {
		Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Observation request exceeds its bounded shape"))
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil || decoder.Decode(new(any)) != io.EOF {
		Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Observation request has an invalid shape"))
		return false
	}
	return true
}
