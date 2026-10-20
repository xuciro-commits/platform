package platformserver

import (
	"encoding/json"
	"platformserver/internal/acceptance"

	"platformserver/platform"
)

// rowDecodeFormattedImage keeps publication projections and owner record formats
// separate. Selecting an older release selects that projection's saved format;
// the model owner alone describes how that format becomes its current model.
func rowDecodeFormattedImage(info platform.EntityInfo, value json.RawMessage, projection *recordProjection, release, format string) (json.RawMessage, error) {
	decoded, err := rowDecodeImage(info, value, projection, release)
	if err != nil {
		return nil, err
	}
	if selected := projectionFor(projection, release); selected != nil {
		shape, err := acceptance.Digest(info.Fields)
		if err != nil {
			return nil, err
		}
		if shape == selected.Shape {
			format = selected.Format
		}
	}
	return platform.DecodeRecordImage(info, format, decoded)
}
