package platform

func (p Page) QueryReferences() []AssetRef {
	refs := []AssetRef{}
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

func (p Page) WindowVariableObject(id string) string { return p.CollectionInputObject(id).Name }
