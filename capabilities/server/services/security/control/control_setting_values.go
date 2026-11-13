package control

import (
	"encoding/json"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type AppSettings struct {
	App      string         `json:"app"`
	Settings []SettingValue `json:"settings"`
}

type SettingValue struct {
	platform.Setting
	Value string `json:"value"`
}

func (s *Settings) Apps(manifests []platform.Manifest) []AppSettings {
	out := []AppSettings{}
	for _, m := range manifests {
		if len(m.Settings) == 0 {
			continue
		}
		app := AppSettings{App: m.ID}
		for _, x := range m.Settings {
			v, ok := s.Get(m.ID + "/" + x.Name)
			if !ok {
				v = x.Default
			}
			app.Settings = append(app.Settings, SettingValue{Setting: x, Value: v})
		}
		out = append(out, app)
	}
	return out
}

func (s *Settings) AppsOf(apps []platform.App) []AppSettings {
	manifests := make([]platform.Manifest, 0, len(apps))
	for _, app := range apps {
		manifests = append(manifests, app.Manifest())
	}
	return s.Apps(manifests)
}

func (s *Settings) Value(app, name string, declared func(app string) ([]platform.Setting, bool)) string {
	if tenantWide, ok := strings.CutPrefix(name, PlatformApp+"/"); ok {
		app, name = PlatformApp, tenantWide
	}
	settings, ok := declared(app)
	if !ok {
		return ""
	}
	if v, ok := s.Get(app + "/" + name); ok {
		return v
	}
	i := slices.IndexFunc(settings, func(s platform.Setting) bool { return s.Name == name })
	if i < 0 {
		return ""
	}
	return settings[i].Default
}

func (s *Settings) ValueFor(app, name string, apps []platform.App) string {
	return s.Value(app, name, func(id string) ([]platform.Setting, bool) {
		for _, candidate := range apps {
			if candidate.Manifest().ID == id {
				return candidate.Manifest().Settings, true
			}
		}
		return nil, false
	})
}

type SettingDecision struct {
	Checks         SettingChecks
	Settings       *Settings
	EmbeddingModel string
	EmbeddingNext  string
	ActiveSpace    string
	VectorSpace    func(string) string
}

func (d SettingDecision) Decide(c platform.Caller, s *pb.Submission, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	id, value, refusal := d.Checks.Check(c, s, now)
	if refusal != nil {
		return nil, refusal
	}
	bindEmbedding := id == d.EmbeddingModel && !c.Replaying && c.Staging()
	space := ""
	if bindEmbedding && value != "" {
		space = d.VectorSpace(value)
	}
	return func(*pb.ChangeRecord) {
		d.Settings.Set(id, value)
		if bindEmbedding {
			d.Settings.Set(d.ActiveSpace, space)
			d.Settings.Set(d.EmbeddingNext, "")
		}
	}, nil
}

type SettingChecks struct {
	App     func(id string) platform.App
	Logging func(name, value string, at time.Time) *kernel.Error
	Values  []func(id, value string) *kernel.Error
}

func (c SettingChecks) Check(caller platform.Caller, s *pb.Submission, now time.Time) (string, string, *kernel.Error) {
	id := s.GetTarget().GetId()
	var p struct{ Value string }
	json.Unmarshal(s.GetPayload(), &p)
	app, name, _ := strings.Cut(id, "/")
	a := c.App(app)
	if a == nil {
		return id, p.Value, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if app == PlatformApp && strings.HasPrefix(name, "logPolicy/") {
		if c.App(strings.TrimPrefix(name, "logPolicy/")) == nil {
			return id, p.Value, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		if !caller.Replaying {
			return id, p.Value, CheckLogPolicy(p.Value, now)
		}
		return id, p.Value, nil
	}
	if !caller.Replaying {
		if refusal := c.Logging(name, p.Value, now); refusal != nil {
			return id, p.Value, refusal
		}
	}
	i := slices.IndexFunc(a.Manifest().Settings, func(x platform.Setting) bool { return x.Name == name })
	if i < 0 {
		return id, p.Value, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if !a.Manifest().Settings[i].Accepts(p.Value) {
		return id, p.Value, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if caller.Replaying {
		return id, p.Value, nil
	}
	for _, check := range c.Values {
		if refusal := check(id, p.Value); refusal != nil {
			return id, p.Value, refusal
		}
	}
	if id == "platform/enterpriseRoot" && p.Value != "" {
		element, ok := caller.Enterprise().Element(p.Value, now)
		if !ok || element.Stereotype != "ActualOrganization" || element.Owner != "" {
			return id, p.Value, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose an active local organization for this tenant")
		}
	}
	return id, p.Value, nil
}
