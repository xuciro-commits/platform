package modbus

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

const Profile = "modbus-tcp-abcd-read"

func Runtime() platform.DriverRuntime {
	return platform.DriverRuntime{ABI: platform.DriverABI, Profiles: []platform.DriverProfile{{
		ID: Profile, Title: "Modbus TCP ABCD (read only)",
		Description: "Read-only TCP with ABCD register order. Addresses use zero-based offsets: coil:0, discrete:0, holding:0, input:0; holding:0.3 selects register bit 3. Local protocol service verified; real devices are unverified.",
		Operations:  []string{"read", "sample"},
		ConnectionFields: []platform.Field{
			{Name: "address", Type: "string", Required: true, Description: "Modbus TCP address, with optional port (default 502)"},
			{Name: "timeoutMs", Type: "integer", Description: "Read timeout in milliseconds (default 5000)"},
		},
		DeviceFields: []platform.Field{{Name: "unitID", Type: "integer", Description: "Modbus unit ID (0–255; default 1)"}},
	}}}
}

type session struct {
	open     platform.DriverOpen
	configs  map[string]Config
	clients  map[string]*Client
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
				return nil, fmt.Errorf("Modbus session needs its connection, revision and generation")
			}
			if h.sessions[in.Session] != nil {
				return nil, fmt.Errorf("Modbus session is already open")
			}
			h.sessions[in.Session] = &session{open: in, configs: configs, clients: map[string]*Client{}}
		}
		return map[string]any{"valid": true, "session": in.Session}, nil
	case "close", "start", "stop":
		var in platform.DriverSession
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		s := h.sessions[in.Session]
		if s == nil {
			return nil, fmt.Errorf("Modbus session is not open")
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
		s := h.sessions[in.Session]
		if s == nil {
			return nil, fmt.Errorf("Modbus session is not open")
		}
		if in.Mode == "summary" || in.Mode == "points" || !in.Since.IsZero() || !in.Until.IsZero() {
			return nil, fmt.Errorf("Modbus reads live points only; historical windows require an external observation store")
		}
		return s.read(ctx, in)
	case "execute":
		return nil, fmt.Errorf("this Modbus profile is read only")
	}
	return nil, &driver.Error{Code: -32601, Message: "Unknown Modbus driver method"}
}

func validate(in platform.DriverOpen) (map[string]Config, error) {
	if in.Profile != Profile {
		return nil, fmt.Errorf("unsupported Modbus profile %q", in.Profile)
	}
	if err := in.Configuration.Check(); err != nil {
		return nil, err
	}
	if len(in.Configuration.Commands) != 0 {
		return nil, fmt.Errorf("this Modbus profile cannot declare write commands")
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
			return nil, fmt.Errorf("Modbus device %s needs its TCP address", device.ID)
		}
		config := Config{Address: address, UnitID: 1, Timeout: 5 * time.Second}
		if value, ok := settings["unitID"]; ok {
			n, err := strconv.ParseUint(fmt.Sprint(value), 10, 8)
			if err != nil {
				return nil, fmt.Errorf("Modbus unit ID must be 0..255")
			}
			config.UnitID = byte(n)
		}
		if value, ok := settings["timeoutMs"]; ok {
			n, err := strconv.Atoi(fmt.Sprint(value))
			if err != nil || n < 100 || n > 60000 {
				return nil, fmt.Errorf("Modbus timeout must be 100..60000 milliseconds")
			}
			config.Timeout = time.Duration(n) * time.Millisecond
		}
		configs[device.ID] = config
	}
	for _, point := range in.Configuration.Points {
		if _, err := parsePoint(point); err != nil {
			return nil, fmt.Errorf("Modbus point %s: %w", point.ID, err)
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
		points := []platform.DriverPoint{}
		for _, point := range s.open.Configuration.Points {
			if point.Device == device.ID && (len(in.Signals) == 0 || slices.Contains(in.Signals, point.ID)) {
				points = append(points, point)
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
		for i, point := range points {
			s.sequence++
			sample := platform.DriverSample{Device: device.ID, Signal: point.ID, Type: point.Type, Unit: point.Unit, At: at, ReceivedAt: at, Quality: "good", Source: platform.DriverSampleSource{Connection: s.open.Connection, Revision: s.open.Revision, Generation: s.open.Generation, Sequence: strconv.FormatUint(s.sequence, 10), Clock: "gateway"}}
			if problem != nil {
				sample.Quality, sample.Reason = "bad", problem.Error()
			} else if values[i].Error != "" {
				sample.Quality, sample.Reason = "bad", values[i].Error
			} else if value, err := driver.TransformPointValue(point, values[i].Value); err != nil {
				sample.Quality, sample.Reason = "bad", err.Error()
			} else {
				sample.Value = value
			}
			result.Samples = append(result.Samples, sample)
		}
	}
	if len(result.Samples) == 0 {
		return result, fmt.Errorf("no declared Modbus points match this read")
	}
	return result, nil
}
