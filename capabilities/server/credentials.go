package platformserver

import transport "platformserver/kernel/transport"

import (
	"encoding/json"
	"net/http"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	controlowner "platformserver/kernel/control"
	"platformserver/platform"
)

func (t *Tenant) resolveCredential(name, consumer, address string) ([]byte, string, error) {
	owner, _ := t.app(controlowner.PlatformApp).(platform.Credentials)
	return controlowner.ResolveCredential(t.ID, owner, t.secret, name, consumer, address)
}
func (t *Tenant) credentialValue(name, consumer, address string) ([]byte, bool) {
	value, _, err := t.resolveCredential(name, consumer, address)
	return value, err == nil
}
func (t *Tenant) credentialHeader(name, consumer, address string) ([]byte, bool) {
	value, kind, err := t.resolveCredential(name, consumer, address)
	if err == nil && kind == "bearer" {
		value = []byte("Bearer " + string(value))
	}
	return value, err == nil
}

func (h *Host) routesCredentials(rt *routes) {
	rt.authorized(transport.Route{Pattern: "POST /v1/credentials/seal", Summary: "Encrypt a transient credential before ordinary submission", Body: platform.CredentialSealRequest{}, Answer: platform.CredentialWrite{}}, controlowner.PlatformApp, []string{controlowner.Admin}, func(w http.ResponseWriter, r *http.Request, _ platform.Member, t *Tenant) {
		var request platform.CredentialSealRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&request) != nil {
			Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A valid credential and nonempty single-line value are required"))
			return
		}
		answer, err := controlowner.SealCredential(t.ID, t.app(controlowner.PlatformApp).(platform.Credentials), request)
		if err != nil {
			Reply(w, nil, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		WriteJSON(w, http.StatusOK, answer)
	})
}
