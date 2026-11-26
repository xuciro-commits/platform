package authoring

import (
	"encoding/json"
	"fmt"
	"platformserver/kernel/protocol"
	"slices"

	"platformserver/platform"
	"platformserver/services/resources/publication"
)

func (o *Owner) HandlesPublication(schema string) bool {
	return schema == protocol.SchemaRelease || schema == protocol.SchemaHandOver
}

func (o *Owner) DecodeInstallation(schema string, image json.RawMessage) (protocol.AssetInstallation, error) {
	typ, kind, err := o.publicationKind(schema)
	if err != nil {
		return protocol.AssetInstallation{}, err
	}
	var record struct {
		platform.Record
		Name string `json:"name"`
	}
	if json.Unmarshal(image, &record) != nil || record.ID == "" {
		return protocol.AssetInstallation{}, fmt.Errorf("invalid release publication row")
	}
	return protocol.AssetInstallation{
		Ref:    platform.AssetRef{App: o.env.AppID(), Kind: kind, Name: record.Name},
		Schema: schema, Record: typ + "/" + record.ID, Image: slices.Clone(image),
	}, nil
}

func (o *Owner) ValidateInstallation(plan protocol.AssetInstallation) error {
	decoded, err := o.DecodeInstallation(plan.Schema, plan.Image)
	if err != nil {
		return err
	}
	if decoded.Ref != plan.Ref || decoded.Record != plan.Record {
		return fmt.Errorf("installation identity differs from its saved image")
	}
	return o.ValidateImage(plan.Schema, plan.Image)
}

func (o *Owner) ValidateImage(schema string, image []byte) error {
	page, application, err := o.publicationDefinition(schema, image)
	if err != nil {
		return err
	}
	if page != nil {
		return o.env.Host().ValidateInstallPage(*page)
	}
	return o.env.Host().ValidateInstallApplication(*application)
}

func (o *Owner) ApplyImage(schema string, image []byte) error {
	page, application, err := o.publicationDefinition(schema, image)
	if err != nil {
		return err
	}
	if page != nil {
		return o.env.Host().InstallPage(platform.Caller{Replaying: true}, *page)
	}
	return o.env.Host().InstallApplication(platform.Caller{Replaying: true}, *application)
}

func (o *Owner) RestorePages() error {
	pages, err := o.Pages()
	if err != nil {
		return err
	}
	for _, p := range pages {
		was, ok := publication.Read[protocol.Page](p.Published)
		if !ok || p.Archived {
			continue
		}
		if err := o.env.Host().InstallPage(platform.Caller{Replaying: true}, Descriptor(was, o.env.AppID(), o.env.Version())); err != nil {
			return fmt.Errorf("page %s: %w", p.Name, err)
		}
	}
	for _, p := range pages {
		was, ok := publication.Read[protocol.Page](p.Published)
		if !ok || p.Archived {
			continue
		}
		if err := o.env.Host().ValidateInstallPage(Descriptor(was, o.env.AppID(), o.env.Version())); err != nil {
			return fmt.Errorf("page %s navigation: %w", p.Name, err)
		}
	}
	return nil
}

func (o *Owner) RestoreApplications() error {
	applications, err := o.Applications()
	if err != nil {
		return err
	}
	var installed []platform.Application
	for _, a := range applications {
		was, ok := publication.Read[protocol.Application](a.Published)
		if !ok || a.Archived {
			continue
		}
		app := ApplicationDescriptor(was)
		if err := o.env.Host().InstallApplication(platform.Caller{Replaying: true}, app); err != nil {
			return fmt.Errorf("application %s: %w", a.Name, err)
		}
		installed = append(installed, app)
	}
	for _, app := range installed {
		if err := o.env.Host().ValidateInstallApplication(app); err != nil {
			return fmt.Errorf("application %s: %w", app.Name, err)
		}
	}
	return nil
}

func (o *Owner) publicationKind(schema string) (string, platform.AssetKind, error) {
	switch schema {
	case protocol.SchemaRelease:
		return protocol.PageType, platform.AssetPage, nil
	case protocol.SchemaHandOver:
		return protocol.AppType, platform.AssetApp, nil
	default:
		return "", "", fmt.Errorf("not a page or application publication schema")
	}
}

func (o *Owner) publicationDefinition(schema string, image []byte) (*platform.Page, *platform.Application, error) {
	switch schema {
	case protocol.SchemaRelease:
		var record protocol.Page
		if err := json.Unmarshal(image, &record); err != nil {
			return nil, nil, err
		}
		published, ok := publication.Read[protocol.Page](record.Published)
		if !ok || record.State != "published" || record.ID != published.ID ||
			record.Name != published.Name {
			return nil, nil, fmt.Errorf("accepted page has no published definition")
		}
		page := Descriptor(published, o.env.AppID(), o.env.Version())
		return &page, nil, nil
	case protocol.SchemaHandOver:
		var record protocol.Application
		if err := json.Unmarshal(image, &record); err != nil {
			return nil, nil, err
		}
		published, ok := publication.Read[protocol.Application](record.Published)
		if !ok || record.State != "published" || record.ID != published.ID ||
			record.Name != published.Name {
			return nil, nil, fmt.Errorf("accepted application has no published definition")
		}
		application := ApplicationDescriptor(published)
		return nil, &application, nil
	default:
		return nil, nil, fmt.Errorf("not a page or application publication schema")
	}
}
