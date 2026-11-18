package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"platformserver/platform"
)

func ReadTestPlan(raw []byte) (platform.TestPlanDescriptor, error) {
	var p platform.TestPlanDescriptor
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if len(raw) > 512<<10 || decoder.Decode(&p) != nil || decoder.Decode(new(any)) != io.EOF {
		return p, fmt.Errorf("invalid test plan descriptor")
	}
	return p, p.Check()
}
