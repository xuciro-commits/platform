package authoring

import (
	"encoding/json"
	"fmt"
	"maps"
	"platformserver/kernel/protocol"
	"slices"
	"time"

	"platformserver/platform"
)

const (
	Builder = "builder"
	User    = "user"
)

type ProcessLookup struct {
	App           string
	Entity        func(string) (platform.RecordTypeInfo, bool)
	Action        func(string) (string, platform.Action, bool)
	Function      func(string, string, int) (platform.AIFunction, int, bool)
	Operation     func(string, string, int) (platform.Operation, int, bool)
	Query         func(string, int) (platform.NamedQuery, int, bool)
	ValidateQuery func(platform.NamedQuery) error
}

func processPaths(s protocol.ProcessStep) []string {
	paths := []string{s.Next, s.Error, s.Body}
	paths = append(paths, s.Branches...)
	paths = append(paths, slices.Collect(maps.Values(s.Cases))...)
	return paths
}

func ReadProcessImage(image []byte) (protocol.Process, error) {
	var record protocol.Process
	if err := json.Unmarshal(image, &record); err != nil {
		return protocol.Process{}, err
	}
	if record.State != "published" || record.Version < 1 || record.Version > 64 || len(record.Versions) != record.Version || record.Published != record.Versions[record.Version-1] {
		return protocol.Process{}, fmt.Errorf("accepted process has an invalid version family")
	}
	var latest protocol.Process
	for index, raw := range record.Versions {
		version, ok := readPublished[protocol.Process](raw)
		if !ok || version.ID != record.ID || version.Version != index+1 || version.Published != "" || len(version.Versions) != 0 || index > 0 && (version.Name != latest.Name || version.Object != latest.Object) {
			return protocol.Process{}, fmt.Errorf("accepted process has a malformed version %d", index+1)
		}
		latest = version
	}
	return latest, nil
}

type ProcessExecution struct {
	App               string
	FunctionAction    string
	FunctionResult    func(platform.Caller, string) (protocol.FunctionOutcome, bool)
	StateField        string
	Refused           func(platform.Caller, *platform.Error, time.Time)
	Notify            func(platform.Caller, *platform.Run, string, protocol.NotificationEffect, json.RawMessage) (json.RawMessage, *platform.Error)
	Balance           func(platform.Caller, *platform.Run, string, protocol.BalanceRule) (string, *platform.Error)
	Paused            func(platform.Caller) bool
	ContinuousEnabled func(platform.Caller) bool
	Subject           func(platform.Caller, *platform.Run) (json.RawMessage, *platform.Error)
	SubjectPath       func(platform.Caller, *platform.Run, []string) (json.RawMessage, []string, *platform.Error)
	Query             func(platform.Caller, *platform.Run, string, string, int, json.RawMessage) (json.RawMessage, *platform.Error)
}
