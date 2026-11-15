package publication

import "platformserver/platform"

type Resolver struct {
	CheckPage func(platform.Page) error
}
