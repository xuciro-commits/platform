package kernel

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
)

func ControlDiff(base, current map[string]json.RawMessage, cancelled map[string]bool) (*acceptedformat.Control, error) {
	result := &acceptedformat.Control{}
	for _, kind := range slices.Sorted(maps.Keys(current)) {
		prior := base[kind]
		image := current[kind]
		if kind == "effects" {
			var earlier, after []acceptedformat.EffectState
			if err := acceptedformat.DecodeControl(prior, &earlier); err != nil {
				return nil, err
			}
			if err := acceptedformat.DecodeControl(image, &after); err != nil {
				return nil, err
			}
			lookup := map[string]acceptedformat.EffectState{}
			for _, x := range earlier {
				lookup[x.ID] = x
			}
			var beforeChanges, afterChanges []acceptedformat.EffectState
			for _, x := range after {
				old, known := lookup[x.ID]
				if !known {
					return nil, fmt.Errorf("control may not create effect intents")
				}
				oldHash, _ := platform.CanonicalDigest(old)
				newHash, _ := platform.CanonicalDigest(x)
				if oldHash != newHash && !cancelled[x.ID] {
					beforeChanges = append(beforeChanges, old)
					afterChanges = append(afterChanges, x)
				}
			}
			if len(afterChanges) == 0 {
				continue
			}
			prior, _ = json.Marshal(beforeChanges)
			image, _ = json.Marshal(afterChanges)
		}
		before, err := platform.CanonicalDigest(prior)
		if err != nil {
			return nil, err
		}
		after, err := platform.CanonicalDigest(image)
		if err != nil {
			return nil, err
		}
		if before != after {
			result.Changes = append(result.Changes, acceptedformat.ControlChange{Kind: kind, Before: before, Image: image})
		}
	}
	if len(result.Changes) == 0 {
		return nil, nil
	}
	return result, nil
}

func ControlNext(current map[string]json.RawMessage, saved *acceptedformat.Control) (map[string]json.RawMessage, error) {
	seen := map[string]bool{}
	for _, change := range saved.Changes {
		prior := current[change.Kind]
		if change.Kind == "effects" {
			var after, all []acceptedformat.EffectState
			if err := acceptedformat.DecodeControl(change.Image, &after); err != nil {
				return nil, err
			}
			if err := acceptedformat.DecodeControl(prior, &all); err != nil {
				return nil, err
			}
			selected := []acceptedformat.EffectState{}
			seenIDs := map[string]bool{}
			for _, next := range after {
				i := slices.IndexFunc(all, func(x acceptedformat.EffectState) bool { return x.ID == next.ID })
				if i < 0 || seenIDs[next.ID] {
					return nil, fmt.Errorf("control effect is missing or duplicated")
				}
				seenIDs[next.ID] = true
				old := all[i]
				fixed := next
				fixed.State, fixed.Due, fixed.Since = old.State, old.Due, old.Since
				fixed.Last, fixed.Error, fixed.CallbackError = old.Last, old.Error, old.CallbackError
				oldHash, _ := platform.CanonicalDigest(old)
				fixedHash, _ := platform.CanonicalDigest(fixed)
				if oldHash != fixedHash {
					return nil, fmt.Errorf("control effect rewrites immutable intent")
				}
				selected = append(selected, old)
				all[i] = next
			}
			prior, _ = json.Marshal(selected)
			current[change.Kind], _ = json.Marshal(all)
		}
		before, err := platform.CanonicalDigest(prior)
		if err != nil || seen[change.Kind] || current[change.Kind] == nil || change.Before != before {
			return nil, fmt.Errorf("control %s predecessor differs", change.Kind)
		}
		seen[change.Kind] = true
		if change.Kind != "effects" {
			current[change.Kind] = change.Image
		}
	}
	return current, nil
}

func ControlImages(values map[string]any) (map[string]json.RawMessage, error) {
	out := map[string]json.RawMessage{}
	for kind, v := range values {
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		out[kind] = raw
	}
	return out, nil
}

func ValidateControl(saved *acceptedformat.Control, capture func() (map[string]json.RawMessage, error), fork func(map[string]json.RawMessage) error) error {
	if saved == nil {
		return nil
	}
	current, err := capture()
	if err != nil {
		return err
	}
	current, err = ControlNext(current, saved)
	if err != nil {
		return err
	}
	return fork(current)
}

func ApplyControl(saved *acceptedformat.Control, install func(kind string, image json.RawMessage) error, patchEffects func(json.RawMessage) error) error {
	if saved == nil {
		return nil
	}
	for _, change := range saved.Changes {
		var err error
		if change.Kind == "effects" {
			err = patchEffects(change.Image)
		} else {
			err = install(change.Kind, change.Image)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Sequence) CaptureControl(work func() (any, error), values func() map[string]any) (map[string]json.RawMessage, error) {
	connectors, err := s.ConnectorRoster.Image()
	if err != nil {
		return nil, err
	}
	image, err := work()
	if err != nil {
		return nil, err
	}
	all := values()
	all["work"] = image
	out, err := ControlImages(all)
	if err != nil {
		return nil, err
	}
	out["connectors"] = connectors
	return out, nil
}
