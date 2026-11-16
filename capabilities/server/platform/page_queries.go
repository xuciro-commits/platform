package platform

func (p Page) WindowVariableObject(id string) string { return p.CollectionInputObject(id).Name }
