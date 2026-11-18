package authoring

import (
	"platformserver/platform"
	"time"
)

func (b *Build) Run(c platform.Caller, job string, at time.Time) *platform.Error {
	if job == "dispatches" {
		return b.data.RunDispatches(c, at)
	}
	if job != "confirmations" {
		return nil
	}
	if err := b.automation.Run(c, at); err != nil {
		return err
	}
	return b.data.RunConfirmations(c, at)
}
