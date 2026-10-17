package build

import (
	"bytes"
	"encoding/json"
)

// UnmarshalJSON decodes a step strictly: every step names its kind and binds
// its inputs with typed bindings. There is no older representation beside
// this one; a record that cannot be read this way is refused where it is read.
func (s *ProcessStep) UnmarshalJSON(raw []byte) error {
	type current ProcessStep
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var node current
	if err := decoder.Decode(&node); err != nil {
		return err
	}
	*s = ProcessStep(node)
	return nil
}
