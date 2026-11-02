package branches

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
)

type absent struct{}

type PathConflict struct {
	Path   string          `json:"path"`
	Base   json.RawMessage `json:"base,omitempty"`
	Main   json.RawMessage `json:"main,omitempty"`
	Branch json.RawMessage `json:"branch,omitempty"`
}

type PathChange struct {
	Path   string          `json:"path"`
	Op     string          `json:"op" enum:"add,remove,replace"`
	Before json.RawMessage `json:"before,omitempty"`
	After  json.RawMessage `json:"after,omitempty"`
}

type Resolution struct {
	Path string `json:"path"`
	Take string `json:"take" enum:"branch,main"`
}

func decode(raw json.RawMessage) (any, error) {
	if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return absent{}, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if decoder.Decode(new(any)) == nil {
		return nil, fmt.Errorf("one JSON value required")
	}
	return value, nil
}

func encode(value any) json.RawMessage {
	if _, none := value.(absent); none {
		return nil
	}
	raw, _ := json.Marshal(value)
	return raw
}

func equal(a, b any) bool {
	_, an := a.(absent)
	_, bn := b.(absent)
	if an || bn {
		return an && bn
	}
	return bytes.Equal(encode(a), encode(b))
}

func escape(segment string) string {
	return strings.ReplaceAll(strings.ReplaceAll(segment, "~", "~0"), "/", "~1")
}

func pointer(path []string) string {
	var out strings.Builder
	for _, segment := range path {
		out.WriteByte('/')
		out.WriteString(escape(segment))
	}
	return out.String()
}

type keyedArray struct {
	order []string
	items map[string]any
}

func keyed(values ...any) ([]keyedArray, bool) {
	lists := make([][]any, len(values))
	nonEmpty := false
	for i, value := range values {
		switch list := value.(type) {
		case []any:
			lists[i] = list
			nonEmpty = nonEmpty || len(list) > 0
		case absent:
		default:
			return nil, false
		}
	}
	if !nonEmpty {
		return nil, false
	}
	for _, field := range []string{"name", "id"} {
		out := make([]keyedArray, len(lists))
		good := true
		for i, list := range lists {
			out[i] = keyedArray{items: map[string]any{}}
			for _, item := range list {
				object, isObject := item.(map[string]any)
				key, _ := object[field].(string)
				if _, repeated := out[i].items[key]; !isObject || key == "" || repeated {
					good = false
					break
				}
				out[i].order = append(out[i].order, key)
				out[i].items[key] = item
			}
			if !good {
				break
			}
		}
		if good {
			return out, true
		}
	}
	return nil, false
}

func (k keyedArray) at(key string) any {
	if item, present := k.items[key]; present {
		return item
	}
	return absent{}
}

func union(arrays ...keyedArray) []string {
	var order []string
	for _, array := range arrays {
		for _, key := range array.order {
			if !slices.Contains(order, key) {
				order = append(order, key)
			}
		}
	}
	return order
}

func child(value any, key string) any {
	if object, ok := value.(map[string]any); ok {
		if item, present := object[key]; present {
			return item
		}
	}
	return absent{}
}

type merger struct {
	resolutions map[string]string
	conflicts   []PathConflict
}

func (m *merger) merge(path []string, base, main, branch any) any {
	switch {
	case equal(main, branch):
		return main
	case equal(base, main):
		return branch
	case equal(base, branch):
		return main
	}
	mainObject, mainIsObject := main.(map[string]any)
	branchObject, branchIsObject := branch.(map[string]any)
	_, baseIsObject := base.(map[string]any)
	_, baseNone := base.(absent)
	if mainIsObject && branchIsObject && (baseIsObject || baseNone) {
		out := map[string]any{}
		keys := map[string]bool{}
		for key := range mainObject {
			keys[key] = true
		}
		for key := range branchObject {
			keys[key] = true
		}
		for key := range keys {
			merged := m.merge(append(slices.Clone(path), key), child(base, key), child(main, key), child(branch, key))
			if _, none := merged.(absent); !none {
				out[key] = merged
			}
		}
		return out
	}
	_, mainIsList := main.([]any)
	_, branchIsList := branch.([]any)
	if arrays, ok := keyed(base, main, branch); ok && mainIsList && branchIsList {
		out := []any{}
		for _, key := range union(arrays[1], arrays[2]) {
			merged := m.merge(append(slices.Clone(path), key), arrays[0].at(key), arrays[1].at(key), arrays[2].at(key))
			if _, none := merged.(absent); !none {
				out = append(out, merged)
			}
		}
		return out
	}
	point := pointer(path)
	m.conflicts = append(m.conflicts, PathConflict{Path: point, Base: encode(base), Main: encode(main), Branch: encode(branch)})
	switch m.resolutions[point] {
	case "branch":
		return branch
	}
	return main
}

func Merge(base, main, branch json.RawMessage, resolutions []Resolution) (merged json.RawMessage, conflicts []PathConflict, err error) {
	b, err := decode(base)
	if err != nil {
		return nil, nil, err
	}
	o, err := decode(main)
	if err != nil {
		return nil, nil, err
	}
	t, err := decode(branch)
	if err != nil {
		return nil, nil, err
	}
	state := &merger{resolutions: map[string]string{}}
	for _, resolution := range resolutions {
		if resolution.Take != "branch" && resolution.Take != "main" {
			return nil, nil, fmt.Errorf("a resolution takes branch or main")
		}
		state.resolutions[resolution.Path] = resolution.Take
	}
	result := state.merge(nil, b, o, t)
	conflicts = slices.DeleteFunc(state.conflicts, func(c PathConflict) bool { return state.resolutions[c.Path] != "" })
	sort.Slice(conflicts, func(i, j int) bool { return conflicts[i].Path < conflicts[j].Path })
	return encode(result), conflicts, nil
}

func Diff(from, to json.RawMessage) ([]PathChange, error) {
	a, err := decode(from)
	if err != nil {
		return nil, err
	}
	b, err := decode(to)
	if err != nil {
		return nil, err
	}
	out := []PathChange{}
	var walk func(path []string, a, b any)
	walk = func(path []string, a, b any) {
		if equal(a, b) {
			return
		}
		ao, aIsObject := a.(map[string]any)
		bo, bIsObject := b.(map[string]any)
		_, aNone := a.(absent)
		_, bNone := b.(absent)
		if aIsObject && bIsObject {
			keys := map[string]bool{}
			for key := range ao {
				keys[key] = true
			}
			for key := range bo {
				keys[key] = true
			}
			for _, key := range slices.Sorted(func(yield func(string) bool) {
				for key := range keys {
					if !yield(key) {
						return
					}
				}
			}) {
				walk(append(slices.Clone(path), key), child(a, key), child(b, key))
			}
			return
		}
		_, aIsList := a.([]any)
		_, bIsList := b.([]any)
		if arrays, ok := keyed(a, b); ok && aIsList && bIsList {
			for _, key := range union(arrays[0], arrays[1]) {
				walk(append(slices.Clone(path), key), arrays[0].at(key), arrays[1].at(key))
			}
			return
		}
		change := PathChange{Path: pointer(path), Before: encode(a), After: encode(b)}
		switch {
		case aNone:
			change.Op = "add"
		case bNone:
			change.Op = "remove"
		default:
			change.Op = "replace"
		}
		out = append(out, change)
	}
	walk(nil, a, b)
	return out, nil
}
