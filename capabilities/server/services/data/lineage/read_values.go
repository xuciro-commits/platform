package lineage

import (
	"net/url"
	"strconv"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func requestInt(values url.Values, name string) (int, *kernel.Error) {
	raw := values.Get(name)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, name+" is a non-negative integer")
	}
	return value, nil
}

func (r Reader) ReadValues(m platform.Member, values url.Values) (Lineage, *kernel.Error) {
	request := LineageRequest{Mode: values.Get("mode"), Project: values.Get("project"), Focus: values.Get("focus"), Ref: values.Get("ref"),
		Direction: values.Get("direction"), Branch: values.Get("branch"), Fields: values.Get("fields") == "on", Field: values.Get("field")}
	if request.Focus == "" {
		request.Focus = values.Get("node")
	}
	for _, field := range []struct {
		name string
		into *int
	}{{"depth", &request.Depth}, {"nodeLimit", &request.NodeLimit}, {"edgeLimit", &request.EdgeLimit}} {
		value, refusal := requestInt(values, field.name)
		if refusal != nil {
			return Lineage{}, refusal
		}
		*field.into = value
	}
	return r.Read(m, request)
}
