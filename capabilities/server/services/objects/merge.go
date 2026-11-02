package objects

import (
	"encoding/json"
	"time"
)

type RecordFieldProvenance struct {
	Value      json.RawMessage `json:"-"`
	Kind       string          `json:"kind"`
	Connector  string          `json:"connector,omitempty"`
	SourceTime *time.Time      `json:"sourceTime,omitempty"`
	At         *time.Time      `json:"at,omitempty"`
	By         string          `json:"by,omitempty"`
}

func MergeAttribute(sources []RecordFieldProvenance, edit RecordFieldProvenance) RecordFieldProvenance {
	if edit.Kind != "" {
		return edit
	}
	var latest RecordFieldProvenance
	for _, source := range sources {
		if latest.Kind == "" || !source.SourceTime.Before(*latest.SourceTime) {
			latest = source
		}
	}
	return latest
}

func SourceDeletionRemovesObject(createdByAction, otherSourcePresent bool) bool {
	return !createdByAction && !otherSourcePresent
}
