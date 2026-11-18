package pages

import (
	"encoding/json"
	"platformserver/services/actions"
)

type ActionCheckRequest struct {
	Target PageModelRecord `json:"target"`
	Inputs json.RawMessage `json:"inputs,omitempty"`
}

type ActionCheckResult = actions.ActionCheckResult
