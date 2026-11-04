package objects

import (
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
)

func (t *Owner) traversalAround(store *records.Store, m platform.Member, typ string, q platform.Query) (*platform.SearchAround, *kernel.Error) {
	edge := q.Traversal
	if edge == nil {
		return nil, nil
	}
	if store != t.BaseStore {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Link traversal is unavailable in a staged read")
	}
	if edge.Binding.Ref.Kind != platform.AssetLinkType || edge.Binding.SourceVersion == "" || edge.ID == "" || (edge.Direction != "forward" && edge.Direction != "reverse") {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A link traversal needs a version, direction and start record")
	}
	link := t.visibleLink(m, edge.Binding)
	if link == nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Link type or start record is unavailable")
	}
	target := link.Child.Name
	if edge.Direction == "reverse" {
		target = link.Parent.Name
	}
	if typ != target {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Link target object does not match its direction")
	}
	var domain []any
	if len(q.Domain) > 0 && json.Unmarshal(q.Domain, &domain) != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid link query")
	}
	return &platform.SearchAround{From: platform.ObjectSet{IDs: []string{edge.ID}, Archived: true}, Link: edge.Binding, Direction: edge.Direction}, nil
}
