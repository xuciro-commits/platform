package pages

import "encoding/json"

type ActionCheckRequest struct {
	Target PageModelRecord `json:"target"`
	Inputs json.RawMessage `json:"inputs,omitempty"`
}
