package platform

import (
	"encoding/json"

	"google.golang.org/protobuf/proto"
)

type SimulationFactory interface {
	NewSimulationApp(tenant string) App
}

type SimulationIdentityFactory interface {
	NewSimulationIdentity(tenant string, members []Member) App
}

type SimulationOrganizationFactory interface {
	NewSimulationOrganization(tenant string, seed OrgSeed) App
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
