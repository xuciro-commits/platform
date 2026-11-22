package automate

import (
	"maps"
	"slices"

	pb "platformkernel/gen/platform/kernel/v1alpha1"

	"platformserver/kernel/protocol"
	"platformserver/platform"
)

func Balance(c platform.Caller, info platform.RecordTypeInfo, field string, rule protocol.BalanceRule, members []string) (string, *platform.Error) {
	members = slices.Compact(slices.Sorted(slices.Values(members)))
	if len(members) == 0 {
		return "", platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Nobody holds the role {role} to assign this to", rule.Role)
	}
	chosen, least := "", -1
	for _, member := range members {
		domain := [][]any{{field, "=", member}}
		for _, name := range slices.Sorted(maps.Keys(rule.Open)) {
			domain = append(domain, []any{name, "in", rule.Open[name]})
		}
		_, total, err := c.FindOf(info.Go, platform.Query{Domain: platform.Raw(domain), Limit: 1})
		if err != nil {
			return "", err
		}
		if least < 0 || total < least {
			chosen, least = member, total
		}
	}
	return chosen, nil
}
