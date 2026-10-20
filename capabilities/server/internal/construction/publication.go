package construction

import "encoding/json"

// ReadPublication is the definition that was published, or false when none was.
func ReadPublication[T any](raw string) (T, bool) {
	var out T
	if raw == "" || json.Unmarshal([]byte(raw), &out) != nil {
		return out, false
	}
	return out, true
}
