package aigateway

import (
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
)

func StopHandoff(c platform.Caller, saved []*pb.ChangeRecord, before acceptedformat.EffectState, detail string, recipients []string) *kernel.Error {
	change, _, _ := strings.Cut(before.Key, "#")
	for _, receipt := range saved {
		if receipt.GetChangeId() != change {
			continue
		}
		to := make([]platform.Recipient, 0, len(recipients))
		for _, member := range recipients {
			to = append(to, platform.Recipient{Member: member})
		}
		return c.Assign(receipt, platform.Assignment{Title: "Review the stopped model call", Body: detail, Ref: "platform.effect/" + before.ID, Key: "ai-handoff:" + before.ID, To: to})
	}
	return nil
}
