package platform

import "fmt"

// ownedChildren includes dormant ownership solely for validation/projection.
// Renderers only traverse the node's actual Children.
func (d *PageDocument) ownedChildren(id string) []string {
	children := append([]string(nil), d.Nodes[id].Children...)
	for _, entry := range d.UnusedWidgets {
		if entry.Parent == id {
			children = append(children, entry.Node)
		}
	}
	return children
}

func (d *PageDocument) checkUnusedWidgets() (map[string]bool, error) {
	unused := map[string]bool{}
	limits := pageWidgets.Layout
	if len(d.UnusedWidgets) > limits.MaxUnused || len(d.UnusedWidgets) > 0 && !PageUIProfileSupports(d.UIProfile, limits.UnusedProfile) {
		return nil, fmt.Errorf("unused widgets need %s and a bounded inventory", limits.UnusedProfile)
	}
	for _, entry := range d.UnusedWidgets {
		leaf, nodeOK := d.Nodes[entry.Node]
		parent, parentOK := d.Nodes[entry.Parent]
		if !nodeOK || !parentOK || leaf.Kind != "widget" || parent.Kind == "widget" || unused[entry.Node] || !pageNodeID.MatchString(entry.Node) || !pageNodeID.MatchString(entry.Parent) {
			return nil, fmt.Errorf("unused widget needs one original leaf and a layout parent")
		}
		unused[entry.Node] = true
	}
	return unused, nil
}
