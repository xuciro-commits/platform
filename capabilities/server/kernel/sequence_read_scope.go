package kernel

import (
	"context"
	"errors"
	"fmt"

	persistence "platformserver/kernel/store"
	"platformserver/platform"
)

func (s *Sequence) OpenReadScope(ctx context.Context, apps []platform.App, copied bool, quarantine func(error)) error {
	view, err := s.Projection.Current().Open(ctx)
	if err != nil {
		var mismatch *persistence.StatePositionError
		if errors.As(err, &mismatch) && (!copied || mismatch.Position < mismatch.Applied) {
			quarantine(err)
		}
		return err
	}
	if view != nil {
		s.InstallRuntimeHistory(view, view.Position, apps)
	}
	return nil
}

func (s *Sequence) CloseReadScope(apps []platform.App, original bool, quarantine func(error)) error {
	current := s.Projection.Current()
	if original && current.Position() != nil && current.Position()() != current.Applied() {
		quarantine(fmt.Errorf("SQL commit is at %d before in-memory application at %d", current.Position()(), current.Applied()))
	}
	view := current.Detach()
	if view == nil {
		return nil
	}
	if store, ok := current.Backend().(AcceptedStateReader); ok {
		s.InstallRuntimeHistory(store, current.Position(), apps)
	}
	return view.Close()
}
