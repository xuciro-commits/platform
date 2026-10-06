package platform

import (
	"fmt"
	"slices"
)

// checkDevice refuses a device the contract does not lay out for.
func (d *PageDocument) checkDevice() error {
	if d.Device == "" {
		return nil
	}
	limits := pageWidgets.Runtime.Device
	if !PageUIProfileSupports(d.UIProfile, limits.RequiredUIProfile) || !slices.Contains(limits.Kinds, d.Device) {
		return fmt.Errorf("page device %q needs its profile and a known kind", d.Device)
	}
	return nil
}
