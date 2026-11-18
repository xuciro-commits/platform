package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"platformserver/platform"
)

func ReadEvaluationSet(raw []byte) (platform.EvaluationSetDescriptor, error) {
	var e platform.EvaluationSetDescriptor
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&e); err != nil {
		return e, err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return e, fmt.Errorf("evaluation set has trailing data")
	}
	return e, e.Check()
}
