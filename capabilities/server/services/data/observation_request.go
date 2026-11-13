package data

import (
	"bytes"
	"encoding/json"
	"io"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func DecodeObservationRequest(body io.Reader, value any) *kernel.Error {
	raw, err := io.ReadAll(io.LimitReader(body, (16<<10)+1))
	if err != nil || len(raw) > 16<<10 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Observation request exceeds its bounded shape")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil || decoder.Decode(new(any)) != io.EOF {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Observation request has an invalid shape")
	}
	return nil
}
