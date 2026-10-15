package s7

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"platformserver/driver"
	"platformserver/platform"
)

const Profile = "s7-1200-standard-db"

func Runtime() platform.DriverRuntime {
	return platform.DriverRuntime{ABI: platform.DriverABI, Profiles: []platform.DriverProfile{{
		ID: Profile, Title: "S7-1200 standard DB (read only)",
		Description: "Prototype verified with a local S7 service; PLC model and firmware compatibility has not been verified.",
		Operations:  []string{"read", "sample"},
		ConnectionFields: []platform.Field{
			{Name: "address", Type: "string", Required: true, Description: "PLC address, with optional TCP port (default 102)"},
			{Name: "rack", Type: "integer", Description: "Rack (default 0)"},
			{Name: "slot", Type: "integer", Description: "Slot (default 1)"},
			{Name: "timeoutMs", Type: "integer", Description: "Read timeout in milliseconds (default 5000)"},
		},
	}}}
}

type session struct {
	open     platform.DriverOpen
	clients  map[string]*Client
	configs  map[string]Config
	sequence uint64
	sampling bool
}

type Handler struct {
	mu       sync.Mutex
	sessions map[string]*session
}

func New() *Handler { return &Handler{sessions: map[string]*session{}} }

func (h *Handler) Handle(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch method {
	case "describe":
		return Runtime(), nil
	case "validate", "open":
		var in platform.DriverOpen
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		configs, err := validate(in)
		if err != nil {
			return nil, err
		}
		if method == "open" {
			if in.Session == "" || in.Connection == "" || in.Revision == "" || in.Generation == "" {
				return nil, fmt.Errorf("S7 session needs its connection, revision and generation")
			}
			if h.sessions[in.Session] != nil {
				return nil, fmt.Errorf("S7 session is already open")
			}
			h.sessions[in.Session] = &session{open: in, configs: configs, clients: map[string]*Client{}}
		}
		return map[string]any{"valid": true, "session": in.Session}, nil
	case "close", "start", "stop":
		var in platform.DriverSession
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		s, ok := h.sessions[in.Session]
		if !ok {
			return nil, fmt.Errorf("S7 session is not open")
		}
		if method == "close" {
			for _, c := range s.clients {
				_ = c.Close()
			}
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
			return nil, fmt.Errorf("S7 session is not open")
		}
		if in.Mode == "summary" || in.Mode == "points" || !in.Since.IsZero() || !in.Until.IsZero() {
			return nil, fmt.Errorf("S7 reads live points only; historical windows require an external observation store")
		}
		return s.read(ctx, in)
	case "execute":
		return nil, fmt.Errorf("this S7 profile is read only")
	}
	return nil, &driver.Error{Code: -32601, Message: "Unknown S7 driver method"}
}

func validate(in platform.DriverOpen) (map[string]Config, error) {
	if in.Profile != Profile {
		return nil, fmt.Errorf("unsupported S7 profile %q", in.Profile)
	}
	if err := in.Configuration.Check(); err != nil {
		return nil, err
	}
	if len(in.Configuration.Commands) != 0 {
		return nil, fmt.Errorf("this S7 profile cannot declare write commands")
	}
	configs := map[string]Config{}
	for _, device := range in.Configuration.Devices {
		settings := map[string]any{}
		for key, value := range in.Configuration.Connection {
			settings[key] = value
		}
		for key, value := range device.Settings {
			settings[key] = value
		}
		address, _ := settings["address"].(string)
		if strings.TrimSpace(address) == "" {
			return nil, fmt.Errorf("S7 device %s needs its PLC address", device.ID)
		}
		config := Config{Address: address, Slot: 1, Timeout: 5 * time.Second}
		for _, field := range []struct {
			name string
			dst  *int
			min  int
			max  int
		}{{"rack", &config.Rack, 0, 7}, {"slot", &config.Slot, 0, 31}} {
			if value, ok := settings[field.name]; ok {
				n, err := strconv.Atoi(fmt.Sprint(value))
				if err != nil || n < field.min || n > field.max {
					return nil, fmt.Errorf("S7 %s must be %d..%d", field.name, field.min, field.max)
				}
				*field.dst = n
			}
		}
		if value, ok := settings["timeoutMs"]; ok {
			n, err := strconv.Atoi(fmt.Sprint(value))
			if err != nil || n < 100 || n > 60000 {
				return nil, fmt.Errorf("S7 timeout must be 100..60000 milliseconds")
			}
			config.Timeout = time.Duration(n) * time.Millisecond
		}
		configs[device.ID] = config
	}
	for _, point := range in.Configuration.Points {
		if err := ValidatePoint(Point{Name: point.ID, Address: point.Address, Type: point.DataType}); err != nil {
			return nil, fmt.Errorf("S7 point %s: %w", point.ID, err)
		}
		if strings.EqualFold(point.DataType, "BOOL") && point.Type != "boolean" || !strings.EqualFold(point.DataType, "BOOL") && !slices.Contains([]string{"integer", "decimal", "number"}, point.Type) || strings.EqualFold(point.DataType, "REAL") && point.Type == "integer" {
			return nil, fmt.Errorf("S7 point %s has incompatible scalar and protocol types", point.ID)
		}
	}
	return configs, nil
}

func (s *session) read(ctx context.Context, in platform.DriverRead) (platform.DriverReadResult, error) {
	result := platform.DriverReadResult{Samples: []platform.DriverSample{}}
	for _, device := range s.open.Configuration.Devices {
		if in.Device != "" && in.Device != device.ID {
			continue
		}
		points := []Point{}
		definitions := []platform.DriverPoint{}
		for _, point := range s.open.Configuration.Points {
			if point.Device == device.ID && (len(in.Signals) == 0 || slices.Contains(in.Signals, point.ID)) {
				points = append(points, Point{Name: point.ID, Address: point.Address, Type: point.DataType})
				definitions = append(definitions, point)
			}
		}
		if len(points) == 0 {
			continue
		}
		client := s.clients[device.ID]
		var problem error
		if client == nil {
			client, problem = Dial(ctx, s.configs[device.ID])
			if problem == nil {
				s.clients[device.ID] = client
			}
		}
		var values []Result
		if problem == nil {
			values, problem = client.Read(ctx, points)
			if problem != nil {
				_ = client.Close()
				delete(s.clients, device.ID)
			}
		}
		at := time.Now().UTC()
		for i, point := range definitions {
			s.sequence++
			sample := platform.DriverSample{Device: device.ID, Signal: point.ID, Type: point.Type, Unit: point.Unit, At: at, ReceivedAt: at, Quality: "good", Source: platform.DriverSampleSource{Connection: s.open.Connection, Revision: s.open.Revision, Generation: s.open.Generation, Sequence: strconv.FormatUint(s.sequence, 10), Clock: "gateway"}}
			if problem != nil {
				sample.Quality, sample.Reason = "bad", problem.Error()
			} else if values[i].Error != "" {
				sample.Quality, sample.Reason = "bad", values[i].Error
			} else {
				value, err := driver.TransformPointValue(point, values[i].Value)
				if err != nil {
					sample.Quality, sample.Reason = "bad", err.Error()
				} else {
					sample.Value = value
				}
			}
			result.Samples = append(result.Samples, sample)
		}
	}
	if len(result.Samples) == 0 {
		return result, fmt.Errorf("no declared S7 points match this read")
	}
	return result, nil
}
