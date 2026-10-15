package host

import "platformserver/platform"

// DriverPackages reads the original Package installation state. It grants no device access.
type DriverPackages interface {
	CheckDriverBinding(platform.DriverBinding) error
}
