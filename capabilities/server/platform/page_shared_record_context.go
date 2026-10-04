package platform

func (d *PageDocument) sharedContextRecord(id string) bool {
	if d == nil || !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.ContextViews.SharedUIProfile) {
		return false
	}
	v := d.Variables[id]
	return v.Type == "record" && v.Scope == "application" && v.Mode == "shared" && v.Source != nil && v.Source.Kind == "application" && v.Source.Object != nil && v.Source.Object.Kind == AssetObject && v.Source.Object.Check() == nil
}

// Telemetry keeps the original application record; file, part and history
// resources remain owned by the consuming page or overlay.
func (d *PageDocument) sharedTelemetryRecord(id string) bool {
	return d != nil && PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.Telemetry.SharedUIProfile) && d.sharedContextRecord(id)
}
func (d *PageDocument) recordInputOwner(s Section) (scope, owner string, ok bool) {
	v := d.Variables[s.RecordVariable]
	if !d.sharedTelemetryRecord(s.RecordVariable) {
		return v.Scope, v.Owner, true
	}
	overlays, loops := d.overlayOwners(), d.loopOwners()
	for id, n := range d.Nodes {
		if n.Section == s.ID {
			if loops[id] != "" {
				return "", "", false
			}
			owner = overlays[id]
			scope = "page"
			if owner != "" {
				scope = "overlay"
			}
			return scope, owner, true
		}
	}
	return "", "", false
}
