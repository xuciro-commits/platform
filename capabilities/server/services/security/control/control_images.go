package control

import (
	"encoding/json"

	"platformserver/kernel/acceptedformat"
)

func (s *Settings) InstallImage(raw json.RawMessage) error {
	var values map[string]string
	if err := acceptedformat.DecodeControl(raw, &values); err != nil {
		return err
	}
	s.Restore(values)
	return nil
}

func (w *Words) InstallImage(raw json.RawMessage) error {
	var words map[string]map[string]string
	if err := acceptedformat.DecodeControl(raw, &words); err != nil {
		return err
	}
	w.Restore(words)
	return nil
}
