package build

import (
	"platformserver/platform"
	dataowner "platformserver/services/data"
	"strings"
	"time"
)

func (b *Build) Answer(c platform.Caller, effect platform.Effect, out platform.Outcome, now time.Time) *platform.Error {
	if strings.HasPrefix(effect.Endpoint, dataowner.WritebackEndpoint) {
		return b.data.Answer(c, effect, out, now)
	}
	return b.functions.AnswerCompilation(c, b, effect, out, now)
}

func (b *Build) OperationDefinition(name string, version int) (platform.Operation, int, bool) {
	return b.functions.OperationDefinition(name, version)
}

func (b *Build) CodeReleaseAssets() ([]platform.ReleaseAsset, error) {
	return b.functions.CodeReleaseAssets()
}

func (b *Build) InstallOperationAsset(asset platform.ReleaseAsset) error {
	return b.functions.InstallOperationAsset(asset)
}
