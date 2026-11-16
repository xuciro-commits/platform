package platform

import "regexp"

var PageNodeID = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._:-]{0,79}$`)
