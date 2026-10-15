package host

import "platformserver/platform"

type DispatchHost interface {
	platform.DispatchTransport
	Holding(string, string) []string
}
