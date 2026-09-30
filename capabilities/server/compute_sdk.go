package platformserver

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"platformserver/platform"
)

type ComputeSDKRequest struct {
	Input  platform.ValueSchema `json:"input"`
	Output platform.ValueSchema `json:"output"`
}
type ComputeSDK struct {
	Source string `json:"source"`
}

func readCapabilityBody(w http.ResponseWriter, r *http.Request, value any) bool {
	raw, err := io.ReadAll(io.LimitReader(r.Body, (128<<10)+1))
	if err != nil || len(raw) > 128<<10 {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return false
	}
	if _, err = platform.DecodeValue(raw, 128<<10); err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"message": err.Error()}})
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(value); err != nil || decoder.Decode(new(any)) != io.EOF {
		WriteJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"message": "Use the declared request fields"}})
		return false
	}
	return true
}
