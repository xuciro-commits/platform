package publication

import (
	"bytes"
	"encoding/json"

	"platformserver/platform"
)

const DraftLimit = 128

type DraftChange struct {
	Ref        platform.AssetRef  `json:"ref"`
	Prior      platform.AssetRef  `json:"prior,omitzero"`
	Type       string             `json:"type"`
	ID         string             `json:"id"`
	Title      string             `json:"title"`
	Variant    string             `json:"variant,omitempty"`
	Published  bool               `json:"published"`
	Changed    platform.Stamp     `json:"changed"`
	Owner      string             `json:"owner"`
	Fields     []DraftChangeField `json:"fields"`
	Diagnostic string             `json:"diagnostic,omitempty"`
}

type DraftChangeField struct {
	Name    string          `json:"name"`
	Before  json.RawMessage `json:"before,omitempty"`
	After   json.RawMessage `json:"after,omitempty"`
	Removed []string        `json:"removed,omitempty"`
	Retyped []string        `json:"retyped,omitempty"`
}

func BreakingMembers(before, after json.RawMessage) (removed, retyped []string) {
	type member struct {
		Name string          `json:"name"`
		Type json.RawMessage `json:"type"`
	}
	var old, next []member
	if json.Unmarshal(before, &old) != nil || json.Unmarshal(after, &next) != nil {
		return nil, nil
	}
	for _, item := range old {
		if item.Name == "" {
			continue
		}
		index := -1
		for i := range next {
			if next[i].Name == item.Name {
				index = i
				break
			}
		}
		if index < 0 {
			removed = append(removed, item.Name)
		} else if len(item.Type) > 0 && !bytes.Equal(item.Type, next[index].Type) {
			retyped = append(retyped, item.Name)
		}
	}
	return removed, retyped
}

type JointDraftAssets struct {
	Before []platform.ReleaseAsset `json:"-"`
	After  []platform.ReleaseAsset `json:"-"`
	Priors []platform.AssetRef     `json:"-"`
	Nexts  []platform.AssetRef     `json:"-"`
	Checks []JointDraftCheck       `json:"-"`
}

type JointDraftCheck struct {
	Kind    platform.AssetKind `json:"kind"`
	ID      string             `json:"id"`
	Checked bool               `json:"checked"`
	Issues  []string           `json:"issues"`
	Change  *DraftChange       `json:"change,omitempty"`
}
