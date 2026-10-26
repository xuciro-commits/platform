package platform

import "testing"

func TestConfirmationRejectsUndeclaredPoliciesAndReverseTransport(t *testing.T) {
	good := Confirmation{Parties: []ConfirmationParty{{Name: "erp", Title: "ERP", Writeback: "send", CompensateWriteback: "reverse"}}, DeadlineSeconds: 10, OnPartial: "hold", OnExpire: "hold", OnComplete: "complete", ReviewerRole: "reviewer"}
	for _, test := range []struct {
		name   string
		change func(*Confirmation)
	}{
		{"missing partial policy", func(c *Confirmation) { c.OnPartial = "" }},
		{"missing expiry policy", func(c *Confirmation) { c.OnExpire = "" }},
		{"implicit partial completion", func(c *Confirmation) { c.OnPartial = "proceed-with-flag" }},
		{"wrong reverse transport", func(c *Confirmation) { c.Parties[0].CompensateAction = "cancel" }},
		{"missing reverse request", func(c *Confirmation) { c.OnExpire = "compensate"; c.Parties[0].CompensateWriteback = "" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := good
			c.Parties = append([]ConfirmationParty(nil), good.Parties...)
			test.change(&c)
			if c.Check() == nil {
				t.Fatal("invalid confirmation accepted")
			}
		})
	}
}
