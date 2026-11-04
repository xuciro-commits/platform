package authoring

import (
	"encoding/json"
	"fmt"

	"platformserver/platform"
	"platformserver/services/resources/publication"
)

func (o *Owner) ApplicationAsset(a Application, sourceVersion string) (platform.ReleaseAsset, error) {
	return platform.ApplicationReleaseAsset(o.env.AppID(), sourceVersion, ApplicationDescriptor(a))
}

func (o *Owner) PrepareApplicationPublication(asset platform.ReleaseAsset, current Application) (Application, error) {
	var frozen Application
	if err := json.Unmarshal(asset.Body, &frozen); err != nil || frozen.Name != asset.Ref.Name {
		return Application{}, fmt.Errorf("invalid saved application %s", asset.Ref)
	}
	frozen.Record = current.Record
	current.State, current.Published = "published", o.PublishedApplication(frozen)
	return current, nil
}

func (o *Owner) ApplicationFromAsset(asset platform.ReleaseAsset) (Application, error) {
	var application Application
	if err := json.Unmarshal(asset.Body, &application); err != nil || application.Name != asset.Ref.Name {
		return Application{}, fmt.Errorf("invalid saved application %s", asset.Ref)
	}
	return application, nil
}

func (o *Owner) CheckApplicationDraft(record Application, validate bool) (Application, platform.AssetRef, platform.AssetRef, bool, error) {
	var prior platform.AssetRef
	hadPrior := false
	if was, ok := publication.Read[Application](record.Published); ok {
		prior = platform.AssetRef{App: o.env.AppID(), Kind: platform.AssetApp, Name: was.Name}
		hadPrior = true
	}
	next := platform.AssetRef{App: o.env.AppID(), Kind: platform.AssetApp, Name: record.Name}
	if err := o.checkApplication(record); err != nil {
		return Application{}, prior, next, hadPrior, err
	}
	if validate {
		if err := o.env.Host().ValidateInstallApplication(ApplicationDescriptor(record)); err != nil {
			return Application{}, prior, next, hadPrior, err
		}
	}
	record.Published = o.PublishedApplication(record)
	return record, prior, next, hadPrior, nil
}
