package platform

// PageCollectionInput transfers a complete query predicate, never downloaded
// rows, a caller grant, or the caller's pagination position.
type PageCollectionInput struct {
	Kind       string             `json:"kind"`
	Object     AssetRef           `json:"object"`
	Predicate  RecordSetPredicate `json:"predicate"`
	Sort       []string           `json:"sort"`
	SortLocked bool               `json:"sortLocked,omitempty"`
	Traversal  *LinkTraversal     `json:"traversal,omitempty"`
	Bindings   []AssetBinding     `json:"bindings,omitempty"`
}

func (p Page) CollectionInputObject(variable string) AssetRef {
	return p.collectionInputObject(variable, map[string]bool{})
}

func (p Page) collectionInputObject(variable string, seen map[string]bool) AssetRef {
	if seen[variable] {
		return AssetRef{}
	}
	seen[variable] = true
	v := p.DocumentVariables()[variable]
	if v.Type != "object-set" {
		return AssetRef{}
	}
	if v.Mode == "input" && p.Document != nil && p.Document.Interface != nil {
		for _, port := range p.Document.Interface.Inputs {
			if port.Variable == variable && port.Object != nil {
				return *port.Object
			}
		}
	}
	if v.Source != nil {
		if v.Mode == "shared" && v.Source.Object != nil {
			return *v.Source.Object
		}
		if v.Source.Kind == "plan" && p.Document != nil {
			return p.Document.Queries[v.Source.Query].Object
		}
		if v.Source.Kind == "query" {
			for _, s := range p.Sections {
				if s.ID == v.Source.Section && s.CollectionVariable != variable {
					return p.collectionInputObject(s.CollectionVariable, seen)
				}
			}
		}
	}
	return AssetRef{}
}
