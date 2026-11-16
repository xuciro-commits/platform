package platform

import (
	"encoding/json"
	"fmt"
)

func (p *Page) UnmarshalJSON(raw []byte) error {
	type value Page
	var decoded value
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	*p = Page(decoded)
	if p.Format != 4 {
		return fmt.Errorf("unsupported page format %d", p.Format)
	}
	return p.CheckEnvelope()
}
