package platform

import (
	"encoding/json"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

// ReadQuery projects the owner query through the caller's current member scope.
// Unlike an app's internal Find, this is suitable for page/flow data bindings.
func (c Caller) ReadQuery(app, name string, inputs json.RawMessage, now time.Time) (json.RawMessage, *kernel.Error) {
	if rt, ok := c.rt.(interface {
		ReadQuery(Caller, string, string, json.RawMessage, time.Time) (json.RawMessage, *kernel.Error)
	}); ok {
		return rt.ReadQuery(c, app, name, inputs, now)
	}
	return nil, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Named queries require the host read path")
}

// ReadQueryVersion selects a retained tenant query. Version zero is reserved
// for code declarations; missing retained sources never fall back to latest.
func (c Caller) ReadQueryVersion(app, name string, version int, inputs json.RawMessage, now time.Time) (json.RawMessage, *kernel.Error) {
	if rt, ok := c.rt.(interface {
		ReadQueryVersion(Caller, string, string, int, json.RawMessage, time.Time) (json.RawMessage, *kernel.Error)
	}); ok {
		return rt.ReadQueryVersion(c, app, name, version, inputs, now)
	}
	return nil, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Retained queries require the host read path")
}

func (c Caller) ReadRecord(typ, id string, now time.Time) (json.RawMessage, *kernel.Error) {
	if rt, ok := c.rt.(interface {
		ReadRecord(Caller, string, string, time.Time) (json.RawMessage, *kernel.Error)
	}); ok {
		return rt.ReadRecord(c, typ, id, now)
	}
	return nil, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Record bindings require the host read path")
}

// ReadRecordPath traverses declared single-record references with this caller's
// current permissions. The host supplies field provenance for each hop.
func (c Caller) ReadRecordPath(typ, id string, path []string, now time.Time) (json.RawMessage, []string, *kernel.Error) {
	if rt, ok := c.rt.(interface {
		ReadRecordPath(Caller, string, string, []string, time.Time) (json.RawMessage, []string, *kernel.Error)
	}); ok {
		return rt.ReadRecordPath(c, typ, id, path, now)
	}
	return nil, nil, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Record paths require the host's scoped reads")
}

// RequestApproval stays in the existing Work request/accepted-result boundary.
func (c Caller) RequestApproval(app string, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	if rt, ok := c.rt.(interface {
		RequestApproval(Caller, string, *pb.Submission, time.Time) (*pb.ChangeRecord, *kernel.Error)
	}); ok {
		return rt.RequestApproval(c, app, s, now)
	}
	return nil, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Approval requests require an accepted decision")
}
