package kernel

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
)

func acceptedStateDigest(owner platform.AcceptedStateApp, image json.RawMessage) (string, error) {
	if digest, ok := owner.(interface {
		AcceptedStateDigest(json.RawMessage) (string, error)
	}); ok {
		return digest.AcceptedStateDigest(image)
	}
	return platform.CanonicalDigest(image)
}

func SavedStates(owners map[string]platform.AcceptedStateApp, bases map[string]json.RawMessage) ([]acceptedformat.State, error) {
	var out []acceptedformat.State
	for _, id := range slices.Sorted(maps.Keys(owners)) {
		owner := owners[id]
		image, err := owner.AcceptedState()
		if err != nil {
			return nil, err
		}
		if err := owner.ValidateAcceptedState(image); err != nil {
			return nil, err
		}
		before, err := acceptedStateDigest(owner, bases[id])
		if err != nil {
			return nil, err
		}
		after, err := acceptedStateDigest(owner, image)
		if err != nil {
			return nil, err
		}
		if before != after {
			out = append(out, acceptedformat.State{App: id, Before: before, Image: image})
		}
	}
	return out, nil
}

func CheckStates(owners map[string]platform.AcceptedStateApp, states []acceptedformat.State) error {
	seen := map[string]bool{}
	for _, state := range states {
		owner := owners[state.App]
		if owner == nil || seen[state.App] || state.Before == "" || len(state.Image) == 0 {
			return fmt.Errorf("accepted result has a duplicate or unknown state owner %s", state.App)
		}
		seen[state.App] = true
		if err := owner.ValidateAcceptedState(state.Image); err != nil {
			return err
		}
		prior, err := owner.AcceptedState()
		if err != nil {
			return err
		}
		if hash, err := acceptedStateDigest(owner, prior); err != nil || hash != state.Before {
			return fmt.Errorf("accepted state predecessor differs for %s", state.App)
		}
	}
	return nil
}

func ApplyStates(owners map[string]platform.AcceptedStateApp, states []acceptedformat.State) error {
	for _, state := range states {
		if err := owners[state.App].ApplyAcceptedState(state.Image); err != nil {
			return err
		}
	}
	return nil
}

func CheckDirectAndChild(direct []acceptedformat.InputRow, child acceptedformat.Batch) error {
	refs := make(map[string]bool, len(direct))
	for _, row := range direct {
		refs[row.Type+"/"+row.ID] = true
	}
	for _, row := range child.Rows {
		if refs[row.Type+"/"+row.ID] {
			return fmt.Errorf("accepted result writes the same direct and child record")
		}
	}
	for _, row := range child.DirectRows {
		if refs[row.Row.Type+"/"+row.Row.ID] {
			return fmt.Errorf("accepted result writes the same direct and child record")
		}
	}
	return nil
}
