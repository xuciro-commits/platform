package build

import "platformserver/platform"

// SavedObjectActions describes editable declarations, not executable grants or
// installed versions. The same object owner produces candidate action inputs.
func (b *Build) SavedObjectActions(c platform.Caller) ([]platform.Action, error) {
	objects, err := readDefinitionInventory[Object](c)
	if err != nil {
		return nil, err
	}
	actions := []platform.Action{}
	for _, object := range objects {
		if !object.Archived {
			actions = append(actions, objectActions(object, Entity(object))...)
		}
	}
	return actions, nil
}
