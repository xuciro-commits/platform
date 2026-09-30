package flow

import (
	"encoding/json"
	"strings"
)

// UnmarshalJSON maps old durable token positions into the same current
// native semantics. It only decodes saved facts, never repeats an action.
func (x *FlowInstance) UnmarshalJSON(raw []byte) error {
	type image FlowInstance
	var saved image
	if err := json.Unmarshal(raw, &saved); err != nil {
		return err
	}
	for index := range saved.Tokens {
		token := &saved.Tokens[index]
		if token.Parent == 0 && token.Branch != "" {
			for _, candidate := range saved.Tokens {
				if candidate.Waits == "join" && candidate.Step == token.Branch {
					token.Parent = candidate.ID
					break
				}
			}
		}
		if node, ok := strings.CutPrefix(token.Step, "_function_"); ok && token.Waits == "wait" {
			var calls map[string]string
			if json.Unmarshal([]byte(saved.Data), &calls) == nil && calls[node] != "" {
				token.Step = node
				token.Waits = "invocation"
				token.Child = calls[node]
			}
		}
	}
	*x = FlowInstance(saved)
	return nil
}
