package kernel

import (
	"encoding/json"
	"log"

	acceptedformat "platformserver/kernel/acceptedformat"
	persistence "platformserver/kernel/store"
	"platformserver/platform"
)

func BackfillAudit(tenant string, entries []persistence.Entry, remember func(platform.AuditEntry)) {
	written := 0
	for _, entry := range entries {
		if entry.Kind != "accepted-result" {
			continue
		}
		var envelope struct{ Kind string }
		if json.Unmarshal(entry.Body, &envelope) != nil {
			continue
		}
		var member platform.Member
		_ = json.Unmarshal(entry.Principal, &member)
		switch envelope.Kind {
		case "record":
			saved, receipt, err := acceptedformat.DecodeRecord(entry.Body)
			if err != nil {
				continue
			}
			remember(platform.AuditEntry{
				ID: "submit:" + entry.App + ":" + receipt.GetSubmission().GetIdempotencyKey(),
				At: saved.At, Member: member.ID, App: entry.App,
				Action: receipt.GetSubmission().GetSchema().GetName(),
				Target: receipt.GetSubmission().GetTarget().GetType() + "/" + receipt.GetSubmission().GetTarget().GetId(),
			})
			written++
		case "record-batch":
			saved, receipt, err := acceptedformat.DecodeBatch(entry.Body)
			if err != nil {
				continue
			}
			sub, err := acceptedformat.BatchSubmission(saved, receipt)
			if err != nil {
				continue
			}
			remember(platform.AuditEntry{
				ID: "submit:" + entry.App + ":" + sub.GetIdempotencyKey(),
				At: saved.At, Member: member.ID, App: entry.App,
				Action: sub.GetSchema().GetName(),
				Target: sub.GetTarget().GetType() + "/" + sub.GetTarget().GetId(),
			})
			written++
		case "input-result":
			input, err := acceptedformat.DecodeInput(entry.Body)
			if err != nil {
				continue
			}
			remember(platform.AuditEntry{
				At: input.At, Member: input.Member, App: input.App, Action: "input:" + input.Name,
			})
			written++
		}
	}
	if written > 0 {
		log.Printf("audit backfill %s: %d accepted events", tenant, written)
	}
}
