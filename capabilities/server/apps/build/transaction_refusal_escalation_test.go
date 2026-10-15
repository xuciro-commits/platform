package build

import (
	"testing"

	"platformserver/platform"
)

func TestBusinessRefusalEscalationPublication(t *testing.T) {
	for _, scenario := range []string{"valid", "one request", "unbounded window", "unknown role"} {
		t.Run(scenario, func(t *testing.T) {
			b, object := refusalPublication(t)
			declaration := refusalDeclaration("block")
			declaration.Escalation = &platform.RefusalEscalation{Count: 3, WindowSeconds: 60, ReviewerRole: Builder}
			switch scenario {
			case "one request":
				declaration.Escalation.Count = 1
			case "unbounded window":
				declaration.Escalation.WindowSeconds = 604801
			case "unknown role":
				declaration.Escalation.ReviewerRole = "undeclared"
			}
			object.Actions[0].Transaction.Refusals = []platform.Refusal{declaration}
			err := b.checkTransactions(object)
			if (err == nil) != (scenario == "valid") {
				t.Fatalf("unexpected escalation publication result: %v", err)
			}
		})
	}
}
