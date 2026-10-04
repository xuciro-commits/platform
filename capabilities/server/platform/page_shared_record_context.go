package platform

func (d *PageDocument) sharedContextRecord(id string) bool {
	if d == nil || !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.ContextViews.SharedUIProfile) {
		return false
	}
	v := d.Variables[id]
	return v.Type == "record" && v.Scope == "application" && v.Mode == "shared" && v.Source != nil && v.Source.Kind == "application" && v.Source.Object != nil && v.Source.Object.Kind == AssetObject && v.Source.Object.Check() == nil
}
