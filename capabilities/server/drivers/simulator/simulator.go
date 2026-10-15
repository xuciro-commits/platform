// Package simulator provides deterministic, protocol-neutral device data.
// Scenarios and point values are inputs, never application business rules.
package simulator

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"platformserver/driver"
	"platformserver/platform"
)

const Profile = "simulator"

var scenarios = []string{"normal", "anomaly", "offline", "stale", "unknown"}

func Runtime() platform.DriverRuntime {
	return platform.DriverRuntime{ABI: platform.DriverABI, Profiles: []platform.DriverProfile{{
		ID: Profile, Title: "Simulated device points", Description: "Deterministic simulated data; not a real device connection.",
		Operations: []string{"read", "sample", "execute"}, Simulation: true, Scenarios: slices.Clone(scenarios),
		ConnectionFields: []platform.Field{
			{Name: "time", Type: "string", Description: "Optional fixed RFC3339 scenario time"},
			{Name: "variation", Type: "string", Description: "Seeded numeric variation (default 0)"},
		},
	}}}
}

type change struct {
	at    time.Time
	value any
}

type session struct {
	open     platform.DriverOpen
	values   map[string][]change
	sampling bool
}

type Handler struct {
	mu       sync.Mutex
	sessions map[string]*session
	Now      func() time.Time
}

func New() *Handler { return &Handler{sessions: map[string]*session{}, Now: time.Now} }

func (h *Handler) Handle(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch method {
	case "describe":
		return Runtime(), nil
	case "validate", "open":
		var in platform.DriverOpen
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		if err := validate(in); err != nil {
			return nil, err
		}
		if method == "open" {
			if in.Session == "" || in.Connection == "" || in.Revision == "" || in.Generation == "" {
				return nil, fmt.Errorf("simulator session needs its connection, revision and generation")
			}
			if h.sessions[in.Session] != nil {
				return nil, fmt.Errorf("simulator session is already open")
			}
			h.sessions[in.Session] = &session{open: in, values: map[string][]change{}}
		}
		return map[string]any{"valid": true, "session": in.Session}, nil
	case "start", "stop", "close":
		var in platform.DriverSession
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		s, ok := h.sessions[in.Session]
		if !ok {
			return nil, fmt.Errorf("simulator session is not open")
		}
		if method == "close" {
			delete(h.sessions, in.Session)
		} else {
			s.sampling = method == "start"
		}
		return map[string]any{"session": in.Session, "sampling": s.sampling}, nil
	case "read":
		var in platform.DriverRead
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		s, ok := h.sessions[in.Session]
		if !ok {
			return nil, fmt.Errorf("simulator session is not open")
		}
		return s.read(ctx, in, h.clock(s))
	case "execute":
		var in platform.DriverExecute
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		s, ok := h.sessions[in.Session]
		if !ok {
			return nil, fmt.Errorf("simulator session is not open")
		}
		return s.execute(in, h.clock(s))
	}
	return nil, &driver.Error{Code: -32601, Message: "Unknown simulator driver method"}
}

func (h *Handler) clock(s *session) time.Time {
	if fixed, ok := s.open.Configuration.Connection["time"].(string); ok && fixed != "" {
		at, _ := time.Parse(time.RFC3339Nano, fixed)
		return at.UTC()
	}
	return h.Now().UTC()
}

func validate(in platform.DriverOpen) error {
	if in.Profile != Profile {
		return fmt.Errorf("unsupported simulator profile %q", in.Profile)
	}
	if err := in.Configuration.Check(); err != nil {
		return err
	}
	if in.Configuration.Scenario != "" && !slices.Contains(scenarios, in.Configuration.Scenario) {
		return fmt.Errorf("unknown simulator scenario %q", in.Configuration.Scenario)
	}
	if fixed, ok := in.Configuration.Connection["time"]; ok && fixed != "" {
		if _, err := time.Parse(time.RFC3339Nano, fmt.Sprint(fixed)); err != nil {
			return fmt.Errorf("simulator time must be an RFC3339 timestamp")
		}
	}
	if variation, ok := in.Configuration.Connection["variation"]; ok {
		if _, err := platform.ParseDecimal(fmt.Sprint(variation)); err != nil {
			return fmt.Errorf("simulator variation must be a bounded decimal")
		}
	}
	s := &session{open: in, values: map[string][]change{}}
	for _, point := range in.Configuration.Points {
		if _, err := s.value(point, time.Unix(0, 0)); err != nil {
			return fmt.Errorf("simulator point %s: %w", point.ID, err)
		}
	}
	return nil
}

func (s *session) value(point platform.DriverPoint, at time.Time) (any, error) {
	var value any = point.Address
	for _, device := range s.open.Configuration.Devices {
		if device.ID != point.Device {
			continue
		}
		if values, ok := device.Settings["values"].(map[string]any); ok {
			if configured, found := values[point.ID]; found {
				value = configured
			}
		}
		if s.open.Configuration.Scenario == "anomaly" {
			values, _ := device.Settings["anomalyValues"].(map[string]any)
			if configured, found := values[point.ID]; found {
				value = configured
			} else {
				switch point.Type {
				case "integer", "decimal", "number":
					value = "1000"
				case "boolean":
					value = true
				default:
					value = "anomaly"
				}
			}
		}
	}
	overridden := false
	for _, version := range s.values[point.ID] {
		if !at.Before(version.at) {
			value = version.value
			overridden = true
		}
	}
	if overridden {
		return driver.TransformPointValue(platform.DriverPoint{Type: point.Type}, value)
	}
	if point.Type == "boolean" {
		if text, ok := value.(string); ok {
			v, err := strconv.ParseBool(text)
			if err != nil {
				return nil, fmt.Errorf("boolean simulation addresses must be true or false")
			}
			value = v
		}
	}
	if slices.Contains([]string{"integer", "decimal", "number"}, point.Type) {
		if variation, exists := s.open.Configuration.Connection["variation"]; exists && fmt.Sprint(variation) != "0" {
			hash := fnv.New64a()
			_, _ = fmt.Fprintf(hash, "%d:%s:%s:%d", s.open.Configuration.Seed, point.Device, point.ID, at.UnixMilli()/int64(point.IntervalMS))
			phase := int(hash.Sum64()%3) - 1
			if phase != 0 {
				offset := strings.TrimPrefix(fmt.Sprint(variation), "-")
				if phase < 0 {
					offset = "-" + offset
				}
				var err error
				value, err = driver.TransformPointValue(platform.DriverPoint{Type: point.Type, Offset: offset}, value)
				if err != nil {
					return nil, err
				}
			}
		}
	}
	return driver.TransformPointValue(point, value)
}

func (s *session) sample(point platform.DriverPoint, at time.Time, received time.Time) (platform.DriverSample, error) {
	value, err := s.value(point, at)
	if err != nil {
		return platform.DriverSample{}, err
	}
	sample := platform.DriverSample{Device: point.Device, Signal: point.ID, Type: point.Type, Value: value, Unit: point.Unit, Quality: "good", At: at, ReceivedAt: received,
		Source: platform.DriverSampleSource{Connection: s.open.Connection, Revision: s.open.Revision, Generation: s.open.Generation, Sequence: strconv.FormatInt(at.UnixMilli()/int64(point.IntervalMS), 10), Clock: "simulated"}}
	switch s.open.Configuration.Scenario {
	case "offline":
		sample.Value, sample.Quality, sample.Reason = nil, "bad", "Simulated device is offline"
	case "stale":
		sample.At, sample.Quality, sample.Reason = at.Add(-5*time.Minute), "stale", "Simulated sample is stale"
	}
	return sample, nil
}

func (s *session) read(ctx context.Context, in platform.DriverRead, now time.Time) (platform.DriverReadResult, error) {
	selected := []platform.DriverPoint{}
	for _, point := range s.open.Configuration.Points {
		if (in.Device == "" || in.Device == point.Device) && (len(in.Signals) == 0 || slices.Contains(in.Signals, point.ID)) {
			selected = append(selected, point)
		}
	}
	if len(selected) == 0 {
		return platform.DriverReadResult{}, fmt.Errorf("no declared simulated points match this read")
	}
	if in.Mode == "summary" || in.Mode == "points" {
		if len(selected) != 1 || !slices.Contains([]string{"integer", "decimal", "number"}, selected[0].Type) || selected[0].Unit == "" {
			return platform.DriverReadResult{}, fmt.Errorf("simulated windows require one numeric point with a unit")
		}
		return s.window(ctx, in, selected[0], now)
	}
	if in.Mode != "" && in.Mode != "latest" {
		return platform.DriverReadResult{}, fmt.Errorf("unknown simulator read mode")
	}
	at := now
	if !in.Until.IsZero() {
		at = in.Until
	}
	result := platform.DriverReadResult{Samples: []platform.DriverSample{}}
	for _, point := range selected {
		sample, err := s.sample(point, at, now)
		if err != nil {
			return result, err
		}
		result.Samples = append(result.Samples, sample)
	}
	return result, nil
}

func (s *session) window(ctx context.Context, in platform.DriverRead, point platform.DriverPoint, now time.Time) (platform.DriverReadResult, error) {
	result := platform.DriverReadResult{Window: &platform.ObservationWindow{Since: in.Since, Until: in.Until, Unit: point.Unit}, Coverage: &platform.DriverCoverage{AvailableSince: time.Unix(0, 0).UTC(), CompleteUntil: in.Until}}
	if in.Since.IsZero() || in.Until.IsZero() || !in.Since.Before(in.Until) || in.Until.Sub(in.Since) > 366*24*time.Hour || in.Limit < 0 || in.Limit > 5000 {
		return result, fmt.Errorf("choose a bounded simulated window and at most 5000 displayed points")
	}
	step := time.Duration(point.IntervalMS) * time.Millisecond
	start := in.Since.Truncate(step)
	if start.Before(in.Since) {
		start = start.Add(step)
	}
	if in.Until.Sub(start)/step > 100000 {
		return result, fmt.Errorf("simulated window exceeds the generated sample budget")
	}
	folds := map[string]*platform.ExprNumericAggregate{}
	for _, op := range []string{"min", "max", "sum", "avg"} {
		folds[op], _ = platform.NewExprNumericAggregate(op, "decimal", "")
	}
	limit := in.Limit
	if limit == 0 {
		limit = 1000
	}
	for at := start; at.Before(in.Until); at = at.Add(step) {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		sample, err := s.sample(point, at, now)
		if err != nil {
			return result, err
		}
		if sample.Quality != "good" {
			if sample.Quality == "stale" {
				result.Coverage.Stale++
			} else {
				result.Coverage.Bad++
			}
			result.Coverage.CompleteUntil = in.Since
			continue
		}
		text := fmt.Sprint(sample.Value)
		if result.Window.Count == 0 {
			result.Window.FirstAt, result.Window.First = at, text
		}
		result.Window.Count++
		result.Coverage.Good++
		result.Window.LastAt, result.Window.Last = at, text
		for _, fold := range folds {
			if err := fold.Add(platform.ExprPresent("decimal", text)); err != nil {
				return result, err
			}
		}
		if in.Mode == "points" && len(result.Points) < limit {
			result.Points = append(result.Points, platform.ObservationPoint{At: at, Value: text})
		}
	}
	if result.Window.Count > 0 {
		for _, item := range []struct {
			op  string
			dst *string
		}{{"min", &result.Window.Min}, {"max", &result.Window.Max}, {"sum", &result.Window.Sum}, {"avg", &result.Window.Mean}} {
			value, err := folds[item.op].Result()
			if err != nil {
				return result, err
			}
			_ = json.Unmarshal(value.Data, item.dst)
		}
	}
	result.Coverage.Truncated = in.Mode == "points" && result.Window.Count > int64(len(result.Points))
	return result, result.Window.Check()
}

func (s *session) execute(in platform.DriverExecute, now time.Time) (platform.DriverExecution, error) {
	refuse := func(reason string) (platform.DriverExecution, error) {
		return platform.DriverExecution{State: "rejected", Sending: "refused", Reason: reason}, nil
	}
	if in.Dispatch.RunID == "" || in.Dispatch.AttemptID == "" || in.Dispatch.BusinessKey == "" || in.Dispatch.BindingRevision != s.open.Revision || in.Dispatch.Deadline.IsZero() || !now.Before(in.Dispatch.Deadline) {
		return refuse("Simulated command needs its original dispatch identity, revision and unexpired deadline")
	}
	index := slices.IndexFunc(s.open.Configuration.Commands, func(command platform.DriverCommand) bool { return command.ID == in.Command.Name })
	if index < 0 {
		return refuse("Simulated command is not declared")
	}
	command := s.open.Configuration.Commands[index]
	point := s.open.Configuration.Points[slices.IndexFunc(s.open.Configuration.Points, func(point platform.DriverPoint) bool { return point.ID == command.Point })]
	if s.open.Configuration.Scenario == "offline" {
		return platform.DriverExecution{State: "unknown", Sending: "not-sent", Reason: "Simulated device is offline"}, nil
	}
	value, err := driver.TransformPointValue(platform.DriverPoint{Type: point.Type}, in.Command.Value)
	if err != nil {
		return refuse(err.Error())
	}
	if slices.Contains([]string{"integer", "decimal", "number"}, point.Type) {
		number, err := platform.ParseDecimal(fmt.Sprint(value))
		if err != nil {
			return refuse(err.Error())
		}
		for _, bound := range []struct {
			text string
			min  bool
		}{{command.Min, true}, {command.Max, false}} {
			if bound.text != "" {
				limit, _ := platform.ParseDecimal(bound.text)
				compared := number.Rat().Cmp(limit.Rat())
				if bound.min && compared < 0 || !bound.min && compared > 0 {
					return refuse("Simulated value is outside the declared command range")
				}
			}
		}
	}
	if len(s.values[point.ID]) >= 1000 {
		return refuse("Simulated scenario has reached its command history limit")
	}
	s.values[point.ID] = append(s.values[point.ID], change{at: now, value: value})
	if s.open.Configuration.Scenario == "unknown" {
		return platform.DriverExecution{State: "unknown", Sending: "possible", Reason: "Simulated command reply was lost"}, nil
	}
	return platform.DriverExecution{State: "accepted", Sending: "answered", Value: value}, nil
}
