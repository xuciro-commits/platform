package data

import (
	"encoding/json"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/protocol"
	"platformserver/platform"
)

type StreamRecords struct {
	Record     func(typ, id string) (any, error)
	Source     func(Source) (Source, error)
	Connection func(Connection) (Connection, error)
}

func (r StreamRecords) source(id string) (Source, bool, error) {
	record, err := r.Record(SourceType, id)
	if err != nil {
		return Source{}, false, err
	}
	switch source := record.(type) {
	case Source:
		source, err = r.Source(source)
		return source, err == nil, nil
	default:
		return Source{}, false, nil
	}
}

func (r StreamRecords) connection(id string) (Connection, bool, error) {
	record, err := r.Record(ConnectionType, id)
	if err != nil {
		return Connection{}, false, err
	}
	switch connection := record.(type) {
	case Connection:
		connection, err = r.Connection(connection)
		return connection, err == nil, nil
	default:
		return Connection{}, false, nil
	}
}

func (r StreamRecords) Resolve(m platform.Member, name string, intake *platform.StreamIntake) (Source, Connection, string, *platform.Error) {
	refuse := func(message string) (Source, Connection, string, *platform.Error) {
		return Source{}, Connection{}, "", platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, message)
	}
	if intake == nil {
		return refuse("The Flow has no controlled source intake")
	}
	source, ok, err := r.source(intake.SourceRecord)
	if err != nil {
		return refuse("The Flow source is not readable")
	}
	if !ok {
		return refuse("The Flow source must be a published incremental stream source")
	}
	connection, ok, err := r.connection(source.Connection)
	if err != nil {
		return refuse("The Flow connection is not readable")
	}
	if !ok {
		return refuse("The Flow connection is not ready")
	}
	hash, refusal := BindStream(name, intake, source, connection, m)
	return source, connection, hash, refusal
}

type StreamReader struct {
	Check  func() *platform.Error
	Latest func(Source, Connection) (string, error)
	Read   func(Source, Connection, ...string) ([]map[string]any, error)
}

func (r StreamReader) Rows(intake *platform.StreamIntake, source Source, connection Connection, latestBootstrap bool, columns []string) ([]map[string]any, string, *platform.Error) {
	retry := intake.Retry
	if err := protocol.CheckStreamPreparationRetry(retry); err != nil {
		return nil, "", platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	cursor, initialized := source.Cursor, !latestBootstrap
	var readErr error
	for attempt := 0; attempt < protocol.StreamPreparationRetryTries(retry); attempt++ {
		if attempt > 0 && retry.BackoffMS > 0 {
			time.Sleep(time.Duration(retry.BackoffMS) * time.Millisecond)
		}
		if refusal := r.Check(); refusal != nil {
			return nil, "", refusal
		}
		if !initialized {
			cursor, readErr = r.Latest(source, connection)
			if readErr != nil {
				continue
			}
			initialized = true
		}
		source.Cursor = cursor
		rows, err := r.Read(source, connection, columns...)
		if err == nil {
			if refusal := r.Check(); refusal != nil {
				return nil, "", refusal
			}
			return rows, cursor, nil
		}
		readErr = err
	}
	return nil, "", platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, readErr.Error())
}

func StreamColumns(source Source, intake *platform.StreamIntake) []string {
	columns := append([]string{source.Since, intake.Key, intake.EventTime, intake.Value}, intake.Partition...)
	return slices.Compact(slices.Sorted(slices.Values(append(columns, intake.Fields...))))
}

func TrimStreamRows(source Source, rows []map[string]any, budget int) ([]map[string]any, *platform.Error) {
	if len(rows) > budget {
		if source.Advance(rows[:budget]) == source.Advance(rows[:budget+1]) {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The source cursor group exceeds the Flow batch budget")
		}
		rows = rows[:budget]
	}
	return rows, nil
}

func StreamBatch(before platform.FlowInstance, source Source, connection Connection, hash string, intake *platform.StreamIntake, rows []map[string]any) (platform.FlowBatch, *platform.Error) {
	bootstrap := len(rows) == 0
	cursor := source.Cursor
	if !bootstrap {
		position := source
		for _, row := range rows {
			next := position.Advance([]map[string]any{row})
			if next == position.Cursor {
				return platform.FlowBatch{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Stream rows need unique increasing incremental positions")
			}
			position.Cursor = next
		}
		cursor = source.Advance(rows)
	}
	batch := platform.FlowBatch{Bootstrap: bootstrap, Source: &platform.FlowSourceCheckpoint{Record: source.ID, Config: hash, Cursor: cursor, Sources: []string{SourceType + "/" + source.ID, ConnectionType + "/" + connection.ID}, Offset: intake.Offset, Initialized: true}}
	if before.Batch != nil {
		batch.Predecessor = before.Batch.Cursor
	}
	for _, row := range rows {
		key, _ := row[intake.Key].(string)
		stamp, _ := row[intake.EventTime].(string)
		at, _ := time.Parse(time.RFC3339Nano, stamp)
		partition := make([]string, 0, len(intake.Partition))
		for _, field := range intake.Partition {
			value, ok := row[field].(string)
			if !ok || value == "" {
				return platform.FlowBatch{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A source partition field needs a nonempty string")
			}
			partition = append(partition, value)
		}
		input, exists := row[intake.Value]
		if !exists {
			return platform.FlowBatch{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The source signal column is missing")
		}
		if len(intake.Fields) > 0 {
			projected := map[string]any{intake.Value: input}
			for _, field := range append(slices.Clone(intake.Partition), intake.Fields...) {
				value, exists := row[field]
				if !exists {
					return platform.FlowBatch{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A declared source metadata field is missing")
				}
				projected[field] = value
			}
			input = projected
		}
		value, encodeErr := json.Marshal(input)
		if encodeErr != nil {
			return platform.FlowBatch{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A source signal needs a JSON value")
		}
		batch.Signals = append(batch.Signals, platform.FlowSignal{Key: key, Partition: string(platform.Raw(partition)), At: at, Value: value})
	}
	fingerprint, err := platform.CanonicalDigest([]any{batch.Predecessor, batch.Source, batch.Bootstrap, batch.Signals})
	if err != nil {
		if bootstrap {
			return platform.FlowBatch{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The source bootstrap cannot be bound")
		}
		return platform.FlowBatch{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The source batch cannot be bound")
	}
	batch.ID = "source:" + fingerprint
	return batch, nil
}

func (source Source) Resume(before platform.FlowInstance, hash string, intake *platform.StreamIntake) (Source, *platform.Error) {
	if before.Batch == nil || before.Batch.Source == nil {
		return source, nil
	}
	checkpoint := before.Batch.Source
	if checkpoint.Record != source.ID || checkpoint.Config != hash || checkpoint.Initialized && checkpoint.Offset != intake.Offset || intake.Offset == "latest" && !checkpoint.Initialized {
		return source, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The source changed after this Flow began consuming it")
	}
	source.Cursor = checkpoint.Cursor
	return source, nil
}

type StreamStart struct {
	Name       string
	Intake     *platform.StreamIntake
	Budget     int
	Source     Source
	Connection Connection
	Hash       string
	Bootstrap  bool
}

type StreamPreparation interface {
	Intake() (string, *platform.StreamIntake, int)
}

func BeginStream(plan StreamPreparation, before platform.FlowInstance, resolve func(string, *platform.StreamIntake) (Source, Connection, string, *platform.Error)) (StreamStart, *platform.Error) {
	var s StreamStart
	var refusal *platform.Error
	s.Name, s.Intake, s.Budget = plan.Intake()
	s.Source, s.Connection, s.Hash, refusal = resolve(s.Name, s.Intake)
	if refusal != nil {
		return s, refusal
	}
	s.Bootstrap = s.Intake.Offset == "latest" && (before.Batch == nil || before.Batch.Source == nil)
	s.Source, refusal = s.Source.Resume(before, s.Hash, s.Intake)
	return s, refusal
}

func (s StreamStart) Unchanged(source Source, connection Connection, hash string) bool {
	return hash == s.Hash && source.DefinitionVersion == s.Source.DefinitionVersion && source.Published == s.Source.Published && connection.DefinitionVersion == s.Connection.DefinitionVersion && connection.Published == s.Connection.Published
}

type StreamConsumption struct {
	Read    func(source Source, columns []string) ([]map[string]any, string, *platform.Error)
	Tick    func() (platform.FlowBatch, bool, *platform.Error)
	Consume func(batch platform.FlowBatch) (platform.FlowBatchOutcome, *platform.Error)
}

func (c StreamConsumption) Run(before platform.FlowInstance, start StreamStart) (platform.FlowBatchOutcome, *platform.Error) {
	source := start.Source
	rows, cursor, refusal := c.Read(source, StreamColumns(source, start.Intake))
	if refusal != nil {
		return platform.FlowBatchOutcome{}, refusal
	}
	source.Cursor = cursor
	rows, refusal = TrimStreamRows(source, rows, start.Budget)
	if refusal != nil {
		return platform.FlowBatchOutcome{}, refusal
	}
	if len(rows) == 0 && !start.Bootstrap {
		tick, due, tickErr := c.Tick()
		if tickErr != nil {
			return platform.FlowBatchOutcome{}, tickErr
		}
		if due {
			return c.Consume(tick)
		}
		return platform.FlowBatchOutcome{}, nil
	}
	batch, refusal := StreamBatch(before, source, start.Connection, start.Hash, start.Intake, rows)
	if refusal != nil {
		return platform.FlowBatchOutcome{}, refusal
	}
	return c.Consume(batch)
}
