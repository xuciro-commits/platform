package releases

import (
	"encoding/json"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
	"slices"
)

type ReleaseDraftReview struct {
	Kind       platform.AssetKind     `json:"kind"`
	ID         string                 `json:"id"`
	Checked    bool                   `json:"checked"`
	Issues     []string               `json:"issues"`
	Change     *build.DraftChange     `json:"change,omitempty"`
	Downstream []ReleaseDraftResource `json:"downstream"`
}

type ReleaseDraftResource struct {
	Ref   platform.AssetRef `json:"ref"`
	Title string            `json:"title"`
}

func (p Planner) reviewDrafts(reply *ReleasePreview) error {
	var breaking []int
	for i := range reply.DraftReviews {
		review := &reply.DraftReviews[i]
		change := review.Change
		if change == nil {
			continue
		}
		if change.Published && slices.ContainsFunc(change.Fields, func(field build.DraftChangeField) bool {
			return len(field.Removed)+len(field.Retyped) > 0
		}) {
			breaking = append(breaking, i)
		}
	}
	if len(breaking) == 0 {
		return nil
	}
	available, err := p.Assets(nil, false)
	if err != nil {
		return err
	}
	graph, err := platform.NewReleaseDependencyGraph(available)
	if err != nil {
		return err
	}
	resources := map[platform.AssetRef]platform.ReleaseAsset{}
	for _, asset := range available {
		resources[asset.Ref] = asset
	}
	for _, i := range breaking {
		review := &reply.DraftReviews[i]
		roots := []platform.AssetRef{review.Change.Ref}
		if review.Change.Prior != (platform.AssetRef{}) && review.Change.Prior != review.Change.Ref {
			roots = append(roots, review.Change.Prior)
		}
		impacted, err := graph.Impact(roots)
		if err != nil {
			return err
		}
		for _, ref := range impacted {
			if resource, exists := resources[ref]; exists && !slices.Contains(roots, ref) {
				var identity struct {
					Title  string
					Action struct{ Title string }
					Entity struct{ Title string }
				}
				if err := json.Unmarshal(resource.Body, &identity); err != nil {
					return err
				}
				if identity.Title == "" {
					switch {
					case identity.Action.Title != "":
						identity.Title = identity.Action.Title
					case identity.Entity.Title != "":
						identity.Title = identity.Entity.Title
					default:
						identity.Title = ref.Name
					}
				}
				review.Downstream = append(review.Downstream, ReleaseDraftResource{Ref: ref, Title: identity.Title})
			}
		}
	}
	return nil
}
