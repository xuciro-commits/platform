package build

import (
	"strings"
	"testing"

	"platformserver/internal/construction"
	"platformserver/platform"
)

func TestProtocolBindingRejectsInvalidLifecycleWithoutPanicking(t *testing.T) {
	for _, tc := range []struct {
		name   string
		states []construction.State
		action construction.Action
		want   string
	}{
		{"missing destination", []construction.State{{Name: "open", Title: "Open"}}, construction.Action{Name: "finish", Title: "Finish", From: []string{"open"}, To: "missing"}, "leaves records"},
		{"missing source", []construction.State{{Name: "open", Title: "Open"}}, construction.Action{Name: "finish", Title: "Finish"}, "says no state"},
		{"missing states", nil, construction.Action{Name: "finish", Title: "Finish", From: []string{"open"}}, "needs states"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			object := construction.Object{
				Name: "example", Title: "Example", States: tc.states,
				Actions:   []construction.Action{tc.action},
				Protocols: []platform.ProtocolBinding{{Name: "example.lifecycle", Version: 1, Actions: map[string]string{"finish": "finish"}}},
			}
			if err := New("protocol-validation").checkObjectProtocols(object); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("invalid lifecycle was not returned as an owner validation error: %v", err)
			}
		})
	}
}
