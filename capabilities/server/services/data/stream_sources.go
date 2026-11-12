package data

import (
	"slices"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func BindStream(name string, intake *platform.StreamIntake, source Source, connection Connection, m platform.Member) (string, *kernel.Error) {
	refuse := func(message string) (string, *kernel.Error) {
		return "", platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, message)
	}
	if !source.Stream || source.State != "published" || source.Name != name || source.Since == "" || source.Profile != "table" {
		return refuse("The Flow source must be a published incremental stream source")
	}
	if connection.State != "ready" {
		return refuse("The Flow connection is not ready")
	}
	if !(platform.MarkingSet{Level: connection.Marking, Compartments: connection.Compartments}).Cleared(m) {
		return refuse("The data carries a marking the caller is not cleared for")
	}
	frozenSource, frozenConnection := source, connection
	frozenSource.Record, frozenSource.State, frozenSource.Cursor, frozenSource.Requested, frozenSource.Last = platform.Record{}, "", "", false, nil
	frozenConnection.Record, frozenConnection.State, frozenConnection.Requested, frozenConnection.Last = platform.Record{}, "", false, nil
	frozenSource.Title, frozenSource.Puller = "", ""
	frozenSource.DefinitionVersion, frozenSource.Published, frozenSource.Versions = 0, "", nil
	frozenConnection.DefinitionVersion, frozenConnection.Published, frozenConnection.Versions = 0, "", nil
	frozenConnection.Name, frozenConnection.Title = "", ""
	hash, err := platform.CanonicalDigest([]any{frozenSource, frozenConnection, intake})
	if err != nil {
		return refuse("The Flow source configuration cannot be bound")
	}
	return hash, nil
}

func (source Source) CheckCheckpoint(connection Connection, hash string, intake *platform.StreamIntake, x platform.FlowInstance, checkpoint platform.FlowSourceCheckpoint, tick bool) *kernel.Error {
	refs := []string{SourceType + "/" + source.ID, ConnectionType + "/" + connection.ID}
	if checkpoint.Record != source.ID || checkpoint.Config != hash || !slices.Equal(checkpoint.Sources, refs) || x.Batch != nil && x.Batch.Source != nil && (x.Batch.Source.Record != source.ID || x.Batch.Source.Config != hash) || intake.Offset == "latest" && (!checkpoint.Initialized || checkpoint.Offset != "latest") || checkpoint.Initialized && checkpoint.Offset != intake.Offset {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The Flow source configuration or protected references changed")
	}
	if x.Batch != nil && x.Batch.Source != nil {
		source.Cursor = x.Batch.Source.Cursor
	}
	if tick {
		var prior *platform.FlowSourceCheckpoint
		if x.Batch != nil {
			prior = x.Batch.Source
		}
		if prior == nil || prior.Cursor != checkpoint.Cursor || prior.Initialized != checkpoint.Initialized || prior.Offset != checkpoint.Offset || !slices.Equal(prior.Sources, checkpoint.Sources) {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A slide tick cannot advance or replace the source checkpoint")
		}
		return nil
	}
	if checkpoint.Cursor == "" {
		if intake.Offset != "latest" || !checkpoint.Initialized || x.Batch != nil && x.Batch.Source != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The source checkpoint must advance its accepted position")
		}
		return nil
	}
	if source.Advance([]map[string]any{{source.Since: checkpoint.Cursor}}) != checkpoint.Cursor || source.Cursor == checkpoint.Cursor {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The source checkpoint must advance its accepted position")
	}
	return nil
}
