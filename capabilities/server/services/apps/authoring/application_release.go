package authoring

import (
	"encoding/json"
	"fmt"
	"platformserver/kernel/protocol"

	"platformserver/platform"
	"platformserver/services/resources/publication"
)

func (o *Owner) ApplicationAsset(a protocol.Application, sourceVersion string) (platform.ReleaseAsset, error) {
	return o.env.Resolver().ApplicationReleaseAsset(o.env.AppID(), sourceVersion, ApplicationDescriptor(a))
}

func (o *Owner) PrepareApplicationPublication(asset platform.ReleaseAsset, current protocol.Application) (protocol.Application, error) {
	var frozen protocol.Application
	if err := json.Unmarshal(asset.Body, &frozen); err != nil || frozen.Name != asset.Ref.Name {
		return protocol.Application{}, fmt.Errorf("invalid saved application %s", asset.Ref)
	}
	frozen.Record = current.Record
	current.State, current.Published = "published", o.PublishedApplication(frozen)
	return current, nil
}

func (o *Owner) ApplicationFromAsset(asset platform.ReleaseAsset) (protocol.Application, error) {
	var application protocol.Application
	if err := json.Unmarshal(asset.Body, &application); err != nil || application.Name != asset.Ref.Name {
		return protocol.Application{}, fmt.Errorf("invalid saved application %s", asset.Ref)
	}
	return application, nil
}

func (o *Owner) CheckApplicationDraft(record protocol.Application, validate bool) (protocol.Application, platform.AssetRef, platform.AssetRef, bool, error) {
	var prior platform.AssetRef
	hadPrior := false
	if was, ok := publication.Read[protocol.Application](record.Published); ok {
		prior = platform.AssetRef{App: o.env.AppID(), Kind: platform.AssetApp, Name: was.Name}
		hadPrior = true
	}
	next := platform.AssetRef{App: o.env.AppID(), Kind: platform.AssetApp, Name: record.Name}
	if err := o.checkApplication(record); err != nil {
		return protocol.Application{}, prior, next, hadPrior, err
	}
	if validate {
		if err := o.env.Host().ValidateInstallApplication(ApplicationDescriptor(record)); err != nil {
			return protocol.Application{}, prior, next, hadPrior, err
		}
	}
	record.Published = o.PublishedApplication(record)
	return record, prior, next, hadPrior, nil
}
