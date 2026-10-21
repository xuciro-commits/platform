package platform

// Count presentations share the declared exclusive state ports and owner.
func (d *PageDocument) checkTreemap(s Section) error {
	if s.Widget != "treemap" && s.Widget != "tag-counts" {
		return nil
	}
	return d.checkDeclaredWidgetPorts(s, pageWidget(s.Widget))
}
