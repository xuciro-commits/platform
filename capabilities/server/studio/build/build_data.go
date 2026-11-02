package build

import (
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	dataowner "platformserver/services/data"
	"slices"
)

func (b *Build) scheduleEntity() platform.Entity {
	return dataowner.ScheduleEntity(func(c platform.Caller, schedule *platform.Schedule) *kernel.Error {
		if err := b.checkName(schedule.Name, schedule.ID); err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		pipeline, ok := platform.Get[Pipeline](c, schedule.Pipeline)
		if !ok || pipeline.Archived {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_REFERENCE, "The scheduled pipeline does not exist")
		}
		return nil
	})
}

func (b *Build) ExecutionSchedule(live platform.Schedule) (platform.Schedule, error) {
	fixed, err := b.acceptedIntegration(live, platform.AssetSchedule)
	if err != nil {
		return platform.Schedule{}, err
	}
	r := fixed.(platform.Schedule)
	r.Record, r.LastRun, r.Runner, r.RunnerScopes = live.Record, live.LastRun, live.Runner, live.RunnerScopes
	r.Enabled = live.Enabled
	r.DefinitionVersion, r.Published, r.Versions = live.DefinitionVersion, live.Published, live.Versions
	return r, nil
}

func (b *Build) mediaSetDefinition(set platform.MediaSet) (MediaSetDefinition, error) {
	definition := MediaSetDefinition{Name: set.Name, Title: set.Title, Description: set.Description, Version: set.Version, Items: set.Items, Blobs: map[string][]byte{}}
	caller := b.host.Automation(platform.Caller{}, ID)
	versions, _, err := platform.Find[platform.MediaSetVersion](caller, platform.Query{Domain: platform.Raw([][]any{{"mediaSet", "=", set.ID}}), Limit: 1000, Sort: []string{"version"}})
	if err != nil {
		return definition, err
	}
	byVersion := map[int]platform.MediaSetVersion{}
	for _, version := range platform.RetainedMediaVersions(set) {
		byVersion[version.Version] = version
	}
	for _, version := range versions {
		byVersion[version.Version] = version
	}
	for _, version := range byVersion {
		definition.History = append(definition.History, version)
	}
	slices.SortFunc(definition.History, func(a, b platform.MediaSetVersion) int { return a.Version - b.Version })
	files, ok := b.files.(platform.MediaFiles)
	if !ok {
		return definition, fmt.Errorf("Media content storage is unavailable")
	}
	items := append([]platform.MediaItem{}, set.Items...)
	for _, version := range definition.History {
		items = append(items, version.Items...)
	}
	for _, item := range items {
		if _, ok := definition.Blobs[item.Hash]; ok {
			continue
		}
		raw, err := files.MediaBytes(item.Hash)
		if err != nil {
			return definition, err
		}
		definition.Blobs[item.Hash] = raw
	}
	return definition, nil
}

type MediaSetDefinition struct {
	Name        string                     `json:"name"`
	Title       string                     `json:"title"`
	Description string                     `json:"description,omitempty"`
	Version     int                        `json:"version"`
	Items       []platform.MediaItem       `json:"items"`
	History     []platform.MediaSetVersion `json:"history"`
	Blobs       map[string][]byte          `json:"blobs"`
}

type ScheduleDefinition struct {
	Name     string `json:"name"`
	Title    string `json:"title"`
	Pipeline string `json:"pipeline"`
	Every    string `json:"every"`
	Enabled  bool   `json:"enabled"`
}
