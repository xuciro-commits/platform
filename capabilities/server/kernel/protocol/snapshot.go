package protocol

import (
	"encoding/json"
	"google.golang.org/protobuf/proto"
	"platformserver/platform"
)

type SimulationFactory interface {
	NewSimulationApp(tenant string) platform.Service
}

type SimulationIdentityFactory interface {
	NewSimulationIdentity(tenant string, members []platform.Member) platform.Service
}

type SimulationOrganizationFactory interface {
	NewSimulationOrganization(tenant string, seed platform.OrgSeed) platform.Service
}

func Protos[T proto.Message](list []T) (json.RawMessage, error) {
	out := make([][]byte, len(list))
	for i, m := range list {
		raw, err := proto.Marshal(m)
		if err != nil {
			return nil, err
		}
		out[i] = raw
	}
	return json.Marshal(out)
}

func Unprotos[T interface {
	proto.Message
	*M
}, M any](raw json.RawMessage) ([]T, error) {
	var list [][]byte
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	out := make([]T, len(list))
	for i, r := range list {
		m := T(new(M))
		if err := proto.Unmarshal(r, m); err != nil {
			return nil, err
		}
		out[i] = m
	}
	return out, nil
}
