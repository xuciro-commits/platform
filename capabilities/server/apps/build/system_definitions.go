package build

import (
	"encoding/json"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// Shared declarations have one authoring owner: the system tenant. A local
// draft may use them, but cannot create, rename, publish or archive over them.
func (b *Build) checkSystemDefinitionWrite(s *pb.Submission) *kernel.Error {
	typ, id := s.GetTarget().GetType(), s.GetTarget().GetId()
	_, err := draftAssetRef(typ, "")
	if err != nil {
		return nil
	} // generated business actions are not authoring writes
	var proposed struct{ Name string }
	_ = json.Unmarshal(s.GetPayload(), &proposed)
	names := []string{proposed.Name}
	if record, ok := b.host.Record(typ + "/" + id); ok {
		var current struct{ Name string }
		_ = json.Unmarshal(platform.Raw(record), &current)
		names = append(names, current.Name)
	}
	for _, name := range names {
		if name == "" {
			continue
		}
		original, _ := draftAssetRef(typ, name)
		if b.host.SystemDefinitionReadOnly(original) {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "System definition {name} is read-only in this tenant; edit it in Platform", original.String())
		}
	}
	return nil
}
