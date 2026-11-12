package releases

import (
	"encoding/json"
	"fmt"
	"time"

	"platformserver/platform"
	build "platformserver/services/resources/authoring"
)

func DraftRestore(owner build.ConstructionDrafts, entities []platform.EntityInfo, m platform.Member, kind platform.AssetKind, id string, now time.Time) (ReleaseDraftRestore, error) {
	typ, published, found, err := owner.DraftPublished(kind, id)
	if err != nil || !found {
		return ReleaseDraftRestore{}, fmt.Errorf("The draft is unavailable")
	}
	if published == "" {
		return ReleaseDraftRestore{Type: typ, Action: "archive", Payload: json.RawMessage(`{}`)}, nil
	}
	var asset platform.ReleaseAsset
	if err := json.Unmarshal([]byte(published), &asset); err != nil {
		return ReleaseDraftRestore{}, fmt.Errorf("The installed version of this draft cannot be restored")
	}
	image := json.RawMessage(published)
	if asset.Ref.Kind != "" {
		lifecycle, ok := owner.(platform.AssetLifecycleOwner)
		if !ok {
			return ReleaseDraftRestore{}, fmt.Errorf("tenant has no builder")
		}
		drafts, err := lifecycle.ReleaseDrafts([]platform.ReleaseAsset{asset}, platform.AssetReleaseContext{DraftsOnly: true, Member: m, At: now})
		if err != nil {
			return ReleaseDraftRestore{}, err
		}
		image = nil
		for _, draft := range drafts {
			if draft.Type == typ {
				image = draft.Image
				break
			}
		}
	}
	var info *platform.EntityInfo
	for _, entity := range entities {
		if entity.Type == typ {
			info = &entity
		}
	}
	if info == nil || len(image) == 0 {
		return ReleaseDraftRestore{}, fmt.Errorf("The installed version of this draft cannot be restored")
	}
	payload, err := AssetDraftPayload(*info, image)
	if err != nil {
		return ReleaseDraftRestore{}, err
	}
	fields := map[string]json.RawMessage{}
	_ = json.Unmarshal(payload, &fields)
	if typ == build.ObjectType {
		delete(fields, "actions")
		delete(fields, "actionTypes")
	}
	for _, field := range info.Fields {
		if _, ok := fields[field.Name]; !ok && !field.ReadOnly && !field.Aside {
			fields[field.Name] = json.RawMessage("null")
		}
	}
	payload, _ = json.Marshal(fields)
	return ReleaseDraftRestore{Type: typ, Action: "edit", Payload: payload}, nil
}
