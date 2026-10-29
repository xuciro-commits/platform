package agents

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"platformserver/platform"
	"slices"
	"strings"
	"unicode/utf8"
)

func Clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}

func Observation(s string, n int) string {
	if len(s) <= n {
		return s
	}
	decoder := json.NewDecoder(strings.NewReader(s))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil || decoder.Decode(new(any)) != io.EOF {
		return Clip(s, n)
	}
	prefix := `{"truncated":true,"continue":"Use context for a record, or query/read with a narrower scope.","data":`
	const suffix = `}`
	budget := n - len(prefix) - len(suffix)
	if budget < 4 {
		return `{"truncated":true}`
	}
	return prefix + string(boundedJSON(value, budget)) + suffix
}

func boundedJSON(value any, budget int) json.RawMessage {
	raw, _ := json.Marshal(value)
	if len(raw) <= budget {
		return raw
	}
	switch value := value.(type) {
	case string:
		low, high := 0, len(value)
		for low < high {
			mid := (low + high + 1) / 2
			candidate, _ := json.Marshal(Clip(value, mid))
			if len(candidate) <= budget {
				low = mid
			} else {
				high = mid - 1
			}
		}
		raw, _ = json.Marshal(Clip(value, low))
		if len(raw) <= budget {
			return raw
		}
	case []any:
		out := []json.RawMessage{}
		remaining := budget - 2
		for _, item := range value {
			if remaining < 4 {
				break
			}
			part := boundedJSON(item, remaining)
			out = append(out, part)
			remaining -= len(part) + 1
		}
		raw, _ = json.Marshal(out)
		return raw
	case map[string]any:
		out := map[string]json.RawMessage{}
		remaining := budget - 2
		for _, key := range slices.Sorted(func(yield func(string) bool) {
			for key := range value {
				if !yield(key) {
					return
				}
			}
		}) {
			name, _ := json.Marshal(key)
			room := remaining - len(name) - 2
			if room < 4 {
				continue
			}
			out[key] = boundedJSON(value[key], room)
			remaining = room - len(out[key])
		}
		raw, _ = json.Marshal(out)
		return raw
	}
	return json.RawMessage(`null`)
}

func CompleteToolCalls(calls []platform.ToolCall, run string, turn int) ([]platform.ToolCall, error) {
	out := slices.Clone(calls)
	seen := map[string]bool{}
	for i := range out {
		call := &out[i]
		if call.ID == "" {
			call.ID = fmt.Sprintf("%s:%d:%d", run, turn, i+1)
		}
		if call.Name == "" || seen[call.ID] {
			return nil, fmt.Errorf("invalid or duplicate tool call %q", call.ID)
		}
		seen[call.ID] = true
		value, err := platform.DecodeValue(call.Arguments, platform.AcceptedResultMaxBytes)
		if _, object := value.(map[string]any); err != nil || !object {
			return nil, fmt.Errorf("tool call %q requires a complete JSON object", call.ID)
		}
		raw, _ := json.Marshal(value)
		call.Arguments = raw
	}
	return out, nil
}

func NormalizeStepJSON(raw json.RawMessage) (json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("step JSON has trailing data")
	}
	return json.Marshal(value)
}

func LegacyOutcome(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "…"
}

func ActionPayload(raw json.RawMessage, completeCall bool) json.RawMessage {
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if completeCall {
		decoder.UseNumber()
	}
	decoder.Decode(&payload)
	if completeCall {
		if input, ok := payload["payload"].(map[string]any); ok {
			body, _ := json.Marshal(input)
			return body
		}
	}
	delete(payload, "target")
	delete(payload, "rationale")
	body, _ := json.Marshal(payload)
	return body
}
