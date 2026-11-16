package pages

import "platformserver/platform"

func QueryReferences(p platform.Page) []platform.AssetRef {
	refs := []platform.AssetRef{}
	for _, q := range p.InputSchema.Queries {
		if q.Object.Name != "" {
			refs = append(refs, q.Object)
		}
		if q.Query != nil {
			refs = append(refs, q.Query.Ref)
		}
	}
	for _, v := range p.InputSchema.Variables {
		if v.Source != nil && v.Source.Object != nil {
			refs = append(refs, *v.Source.Object)
		}
	}
	return refs
}

func computeResources(p platform.Page) map[string]platform.PageComputeResource {
	out := map[string]platform.PageComputeResource{}
	for id, v := range p.InputSchema.Variables {
		if v.Source != nil && v.Source.Kind == "compute" && v.Source.Compute != nil {
			out[id] = *v.Source.Compute
		}
	}
	return out
}

func eventNavigation(b platform.PageEventBinding) *platform.PageNavigation {
	for _, effect := range b.Effects {
		if effect.Kind == "navigate" {
			return effect.Navigate
		}
	}
	return nil
}
