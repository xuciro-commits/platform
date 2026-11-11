package lineage

import (
	"slices"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type Request struct {
	Params
	Ref    string
	Branch string
	Fields bool
	Field  string
}

type Shaped struct {
	Params     Params
	Graph      Graph
	FieldNodes []FieldNode
	FieldEdges []FieldEdge
	Impact     *Impact
}

func Shape(r Request, raw Input, preview, guard Graph, meta map[string]Meta, visible map[string]bool) (Shaped, *kernel.Error) {
	p := r.Params
	if p.Mode == "" {
		if p.Focus != "" || r.Ref != "" {
			p.Mode = "focus"
		} else {
			p.Mode = "overview"
		}
	}
	if p.Mode != "overview" && p.Mode != "focus" {
		return Shaped{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "mode is overview or focus")
	}
	if p.Direction == "" {
		p.Direction = "both"
	}
	if p.Direction != "upstream" && p.Direction != "downstream" && p.Direction != "both" {
		return Shaped{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "direction is upstream, downstream or both")
	}
	if p.Depth == 0 {
		p.Depth = 1
	}
	if p.Depth < 1 || p.Depth > 3 {
		return Shaped{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "depth is 1 to 3")
	}
	preview = ApplyAccess(preview, meta)
	guardNodes := map[string]Node{}
	for _, node := range guard.Nodes {
		guardNodes[node.ID] = node
	}
	for i := range preview.Nodes {
		value, ok := guardNodes[preview.Nodes[i].ID]
		if !ok {
			preview.Nodes[i].Guard = preview.Nodes[i].Effective
			preview.Nodes[i].GuardCompartments = slices.Clone(preview.Nodes[i].EffectiveCompartments)
			preview.Nodes[i].MarkingStatus = "unknown"
			if r.Branch != "" {
				preview.Nodes[i].MarkingStatus = "preview-only"
			}
			continue
		}
		preview.Nodes[i].Guard = value.Guard
		preview.Nodes[i].GuardCompartments = slices.Clone(value.GuardCompartments)
		preview.Nodes[i].MarkingStatus = "consistent"
		if preview.Nodes[i].Guard != preview.Nodes[i].Effective || !slices.Equal(preview.Nodes[i].GuardCompartments, preview.Nodes[i].EffectiveCompartments) {
			preview.Nodes[i].MarkingStatus = "preview-only"
		}
	}
	preview = Project(preview, func(node Node) bool {
		value, ok := visible[node.ID]
		return !ok || value
	}, "other-project")
	out := Shaped{Params: p}
	if r.Fields || r.Field != "" {
		out.FieldNodes, out.FieldEdges = Fields(preview, raw)
	}
	if p.Mode == "focus" && p.Focus != "" {
		out.Impact = ComputeImpact(preview, p.Focus)
	}
	preview = Scope(preview, p)
	if r.Field != "" {
		out.FieldNodes, out.FieldEdges = FieldTrace(out.FieldNodes, out.FieldEdges, r.Field)
	} else if r.Fields {
		out.FieldNodes, out.FieldEdges = ScopeFields(preview, out.FieldNodes, out.FieldEdges)
	}
	if r.Fields || r.Field != "" {
		preview.Coverage.FieldsComplete = true
	}
	if p.Mode == "focus" && p.Focus == "" {
		return Shaped{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "focus is required")
	}
	if p.Mode == "focus" && !slices.ContainsFunc(preview.Nodes, func(node Node) bool { return node.ID == p.Focus }) {
		return Shaped{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "This resource does not exist or you cannot open it")
	}
	out.Graph = preview
	return out, nil
}
