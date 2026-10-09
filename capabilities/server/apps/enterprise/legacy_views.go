package enterprise

import "encoding/json"

// Historical view payloads and snapshots used the eight UAF grid cells.
// Decode them into the current viewpoints; never rewrite journal bytes or
// restore the retired grid field in the live write contract (ADR-0093).
func legacyViewpoint(grid string) string {
	switch grid {
	case "Pr-Sr", "Pr-Cn", "Rs-Sr":
		return "organization"
	case "St-Tx", "St-Sr":
		return "function"
	case "Sv-Tx":
		return "output"
	case "Op-Pr", "Pj-Rm":
		return "control"
	}
	return ""
}

func (v *View) UnmarshalJSON(raw []byte) error {
	type current View
	var saved struct {
		current
		Grid string `json:"grid"`
	}
	if err := json.Unmarshal(raw, &saved); err != nil {
		return err
	}
	*v = View(saved.current)
	if v.Viewpoint == "" {
		v.Viewpoint = legacyViewpoint(saved.Grid)
	}
	return nil
}
