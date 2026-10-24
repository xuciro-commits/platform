package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"time"
)

const DriverABI = "driver/v1"

type DriverRuntime struct {
	ABI      string          `json:"abi"`
	Targets  []DriverTarget  `json:"targets"`
	Profiles []DriverProfile `json:"profiles"`
}
type DriverTarget struct {
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Artifact string `json:"artifact"`
}
type DriverProfile struct {
	ID               string   `json:"id"`
	Title            string   `json:"title"`
	Description      string   `json:"description,omitempty"`
	Operations       []string `json:"operations"`
	Simulation       bool     `json:"simulation,omitempty"`
	Scenarios        []string `json:"scenarios,omitempty"`
	ConnectionFields []Field  `json:"connectionFields,omitempty"`
	DeviceFields     []Field  `json:"deviceFields,omitempty"`
}
type DriverPackage struct {
	ID      string        `json:"id"`
	Version string        `json:"version"`
	Title   string        `json:"title,omitempty"`
	Digest  string        `json:"digest"`
	State   string        `json:"state"`
	Runtime DriverRuntime `json:"runtime"`
}
type DriverBinding struct {
	Scope         string              `json:"scope,omitempty"`
	Package       string              `json:"package"`
	Version       string              `json:"version"`
	Digest        string              `json:"digest"`
	Profile       string              `json:"profile"`
	Configuration DriverConfiguration `json:"configuration"`
}
type DriverConfiguration struct {
	Connection map[string]any  `json:"connection,omitempty"`
	Devices    []DriverDevice  `json:"devices"`
	Points     []DriverPoint   `json:"points"`
	Commands   []DriverCommand `json:"commands,omitempty"`
	Scenario   string          `json:"scenario,omitempty"`
	Seed       int64           `json:"seed,omitempty"`
}
type DriverDevice struct {
	ID       string         `json:"id"`
	Title    string         `json:"title"`
	Settings map[string]any `json:"settings,omitempty"`
}
type DriverPoint struct {
	ID         string `json:"id"`
	Device     string `json:"device"`
	Address    string `json:"address"`
	Type       string `json:"type"`
	DataType   string `json:"dataType,omitempty"`
	Unit       string `json:"unit,omitempty"`
	Scale      string `json:"scale,omitempty"`
	Offset     string `json:"offset,omitempty"`
	IntervalMS int    `json:"intervalMS"`
}
type DriverCommand struct {
	ID    string `json:"id"`
	Point string `json:"point"`
	Min   string `json:"min,omitempty"`
	Max   string `json:"max,omitempty"`
}
type DriverSampleSource struct {
	Connection string `json:"connection"`
	Revision   string `json:"revision"`
	Generation string `json:"generation"`
	Sequence   string `json:"sequence,omitempty"`
	Clock      string `json:"clock,omitempty"`
}
type DriverSample struct {
	Device     string             `json:"device"`
	Signal     string             `json:"signal"`
	Type       string             `json:"type"`
	Value      any                `json:"value,omitempty"`
	Unit       string             `json:"unit,omitempty"`
	Quality    string             `json:"quality"`
	At         time.Time          `json:"at"`
	ReceivedAt time.Time          `json:"receivedAt"`
	Reason     string             `json:"reason,omitempty"`
	Source     DriverSampleSource `json:"source"`
}
type DriverCoverage struct {
	AvailableSince time.Time `json:"availableSince"`
	CompleteUntil  time.Time `json:"completeUntil"`
	Truncated      bool      `json:"truncated"`
	Good           int64     `json:"good"`
	Bad            int64     `json:"bad"`
	Uncertain      int64     `json:"uncertain"`
	Stale          int64     `json:"stale"`
}
type DriverOpen struct {
	Session       string              `json:"session"`
	Connection    string              `json:"connection"`
	Revision      string              `json:"revision"`
	Generation    string              `json:"generation"`
	Profile       string              `json:"profile"`
	Configuration DriverConfiguration `json:"configuration"`
}
type DriverSession struct {
	Session string `json:"session"`
}
type DriverRead struct {
	Session string    `json:"session"`
	Device  string    `json:"device,omitempty"`
	Signals []string  `json:"signals,omitempty"`
	Since   time.Time `json:"since,omitempty"`
	Until   time.Time `json:"until,omitempty"`
	Mode    string    `json:"mode,omitempty"`
	Limit   int       `json:"limit,omitempty"`
}
type DriverReadResult struct {
	Samples  []DriverSample     `json:"samples,omitempty"`
	Window   *ObservationWindow `json:"window,omitempty"`
	Points   []ObservationPoint `json:"points,omitempty"`
	Coverage *DriverCoverage    `json:"coverage,omitempty"`
}
type DriverDispatchIdentity struct {
	Scope           string    `json:"scope,omitempty"`
	RunID           string    `json:"runID"`
	AttemptID       string    `json:"attemptID"`
	BusinessKey     string    `json:"businessKey"`
	Deadline        time.Time `json:"deadline"`
	BindingRevision string    `json:"bindingRevision"`
	PackageDigest   string    `json:"packageDigest"`
}
type DriverCommandValue struct {
	Name  string `json:"name"`
	Value any    `json:"value,omitempty"`
}
type DriverExecute struct {
	Session  string                 `json:"session,omitempty"`
	Dispatch DriverDispatchIdentity `json:"dispatch"`
	Command  DriverCommandValue     `json:"command"`
}
type DriverExecution struct {
	State   string `json:"state"`
	Reason  string `json:"reason,omitempty"`
	Sending string `json:"sending"` // not-sent, possible, answered, refused
	Value   any    `json:"value,omitempty"`
}

func DriverDigest(value string) bool {
	value = strings.TrimPrefix(value, "sha256:")
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}
func (r DriverRuntime) Check() error {
	if r.ABI != DriverABI || len(r.Targets) == 0 || len(r.Profiles) == 0 {
		return fmt.Errorf("driver needs its supported ABI, binary targets and profiles")
	}
	seen := map[string]bool{}
	for _, target := range r.Targets {
		key := target.OS + "/" + target.Arch
		if target.OS == "" || target.Arch == "" || !DriverDigest(target.Artifact) || seen[key] {
			return fmt.Errorf("driver binary targets must be unique and content addressed")
		}
		seen[key] = true
	}
	seen = map[string]bool{}
	for _, profile := range r.Profiles {
		if !driverName(profile.ID) || profile.Title == "" || len(profile.Operations) == 0 || seen[profile.ID] {
			return fmt.Errorf("driver needs unique named profiles with supported operations")
		}
		for _, operation := range profile.Operations {
			if !slices.Contains([]string{"read", "execute", "sample"}, operation) {
				return fmt.Errorf("unknown driver operation %s", operation)
			}
		}
		seen[profile.ID] = true
	}
	return nil
}
func (r DriverRuntime) Target() (DriverTarget, error) {
	for _, target := range r.Targets {
		if target.OS == runtime.GOOS && target.Arch == runtime.GOARCH {
			return target, nil
		}
	}
	return DriverTarget{}, fmt.Errorf("driver has no binary for %s/%s", runtime.GOOS, runtime.GOARCH)
}
func (b DriverBinding) Check() error {
	if b.Scope != "" && !DriverDigest(b.Scope) {
		return fmt.Errorf("driver scope must be a tenant digest")
	}
	if !driverName(b.Package) || !driverName(b.Version) || !DriverDigest(b.Digest) || !driverName(b.Profile) {
		return fmt.Errorf("driver binding needs an exact package, version, binary digest and profile")
	}
	return b.Configuration.Check()
}

// DriverScope is a deployment-local namespace, never a business identity.
func DriverScope(tenant string) string {
	sum := sha256.Sum256([]byte(tenant))
	return hex.EncodeToString(sum[:])
}

func (b DriverBinding) ConnectionID(id string) string {
	if b.Scope == "" {
		return id
	} // original retained routes and receipts
	sum := sha256.Sum256([]byte(b.Scope + "\x00" + id))
	key := hex.EncodeToString(sum[:])
	if strings.HasPrefix(id, "preview-") {
		return "preview-" + key
	}
	return key
}

func (b DriverBinding) Revision() string {
	raw, _ := json.Marshal(b)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}
func (c DriverConfiguration) Check() error {
	if len(c.Devices) == 0 || len(c.Devices) > 1000 || len(c.Points) == 0 || len(c.Points) > 10000 || len(c.Commands) > 1000 {
		return fmt.Errorf("driver configuration needs bounded devices, points and commands")
	}
	devices, points := map[string]bool{}, map[string]bool{}
	for _, device := range c.Devices {
		if !driverName(device.ID) || devices[device.ID] {
			return fmt.Errorf("driver device identities must be unique")
		}
		devices[device.ID] = true
	}
	for _, point := range c.Points {
		if !driverName(point.ID) || points[point.ID] || !devices[point.Device] || point.Address == "" || !slices.Contains([]string{"integer", "decimal", "number", "boolean", "text", "enum"}, point.Type) || point.IntervalMS < 100 || point.IntervalMS > 86400000 {
			return fmt.Errorf("driver points need unique identities, declared devices, typed addresses and bounded intervals")
		}
		for _, decimal := range []string{point.Scale, point.Offset} {
			if decimal != "" {
				if _, err := ParseDecimal(decimal); err != nil {
					return err
				}
			}
		}
		points[point.ID] = true
	}
	seen := map[string]bool{}
	for _, command := range c.Commands {
		if !driverName(command.ID) || seen[command.ID] || !points[command.Point] {
			return fmt.Errorf("driver commands must name distinct declared points")
		}
		seen[command.ID] = true
		for _, number := range []string{command.Min, command.Max} {
			if number != "" {
				if _, err := ParseDecimal(number); err != nil {
					return err
				}
			}
		}
	}
	raw, err := json.Marshal(c)
	if err != nil || len(raw) > 256<<10 {
		return fmt.Errorf("driver configuration exceeds its byte budget")
	}
	return nil
}
func driverName(value string) bool {
	return value != "" && len(value) <= 200 && !strings.ContainsAny(value, "/\\\x00\r\n\t ") && value != "." && value != ".."
}
