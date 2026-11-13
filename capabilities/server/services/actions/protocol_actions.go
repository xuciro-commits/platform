package actions

import "platformserver/platform"

func ProtocolActions(apps []platform.App, protocol string) []platform.Action {
	for _, a := range apps {
		for _, p := range a.Manifest().Provides {
			if p.Protocol.ID() == protocol {
				return p.Protocol.Actions
			}
		}
	}
	return nil
}
