package control

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type LogPolicy struct {
	Level         string `json:"level,omitempty"`
	RetentionDays *int   `json:"retentionDays,omitempty"`
	DebugUntil    string `json:"debugUntil,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

type LogPolicyView struct {
	App           string `json:"app"`
	Level         string `json:"level"`
	RetentionDays int    `json:"retentionDays"`
	DebugUntil    string `json:"debugUntil,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

type Logging struct {
	Settings *Settings
	Defaults []platform.Setting
	Rank     func(string) int
}

func (l Logging) Setting(name string) string {
	if value, ok := l.Settings.Get(PlatformApp + "/" + name); ok {
		return value
	}
	for _, setting := range l.Defaults {
		if setting.Name == name {
			return setting.Default
		}
	}
	return ""
}

func (l Logging) Level() string {
	level := l.Setting(SettingLogLevel)
	if l.Rank(level) < 0 || level == "debug" {
		return "info"
	}
	return level
}

func (l Logging) LevelFor(app string, now time.Time) string {
	level := l.Level()
	policy := l.Policy(app)
	if l.Rank(policy.Level) >= 0 && policy.Level != "debug" {
		level = policy.Level
	}
	if !l.DebugUntil(app, now).IsZero() {
		return "debug"
	}
	return level
}

func (l Logging) Allows(level, app string, now time.Time) bool {
	rank := l.Rank(level)
	if rank < 0 {
		rank = l.Rank("info")
	}
	return rank >= l.Rank(l.LevelFor(app, now))
}

func (l Logging) RequestPercent() int {
	value, err := strconv.Atoi(l.Setting(SettingLogRequests))
	if err != nil {
		return 10
	}
	return max(0, min(value, 100))
}

func (l Logging) RetentionDays() int {
	value, err := strconv.Atoi(l.Setting(SettingLogRetention))
	if err != nil {
		return 30
	}
	return max(0, min(value, 3650))
}

func (l Logging) Policy(app string) LogPolicy {
	raw := l.Setting("logPolicy/" + app)
	if raw == "" {
		return LogPolicy{}
	}
	var policy LogPolicy
	if json.Unmarshal([]byte(raw), &policy) != nil {
		return LogPolicy{}
	}
	return policy
}

func (l Logging) DebugUntil(app string, now time.Time) time.Time {
	var latest time.Time
	for _, raw := range []string{l.Setting(SettingLogDebugUntil), l.Policy(app).DebugUntil} {
		if raw == "" {
			continue
		}
		until, err := time.Parse(time.RFC3339, raw)
		if err == nil && until.After(now) && until.After(latest) {
			latest = until
		}
	}
	return latest
}

func (l Logging) QuotaBytes() int64 {
	value, err := strconv.Atoi(l.Setting(SettingLogQuota))
	if err != nil || value < 1 {
		value = 5120
	}
	return int64(value) << 20
}

func (l Logging) Policies(apps []string) []LogPolicyView {
	values := l.Settings.Snapshot()
	out := []LogPolicyView{}
	for _, app := range apps {
		policy := l.Policy(app)
		view := LogPolicyView{App: app, Level: policy.Level, RetentionDays: l.RetentionDays(), DebugUntil: policy.DebugUntil, Reason: policy.Reason}
		if view.Level == "" {
			view.Level = l.Level()
		}
		if policy.RetentionDays != nil {
			view.RetentionDays = *policy.RetentionDays
		}
		if view.DebugUntil == "" {
			if global := values[PlatformApp+"/"+SettingLogDebugUntil]; global != "" {
				view.DebugUntil = global
				view.Reason = values[PlatformApp+"/"+SettingLogDebugReason]
			}
		}
		out = append(out, view)
	}
	return out
}

func (l Logging) CheckSetting(name, value string, now time.Time) *kernel.Error {
	switch name {
	case SettingLogQuota:
		quota, err := strconv.Atoi(value)
		if err != nil || quota < 1 || quota > 1<<20 {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The daily log quota must be between 1 and 1048576 MiB")
		}
	case SettingLogDebugUntil:
		if value == "" {
			return nil
		}
		if refusal := checkDebugWindow(value, now); refusal != nil {
			return refusal
		}
		if strings.TrimSpace(l.Setting(SettingLogDebugReason)) == "" {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Temporary debug needs a reason")
		}
	case SettingLogDebugReason:
		if len(value) > 500 {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Temporary debug reason is too long")
		}
	case SettingLogLevel:
		if value == "debug" {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Use a temporary debug window instead of a permanent debug level")
		}
	}
	return nil
}

func CheckLogPolicy(raw string, now time.Time) *kernel.Error {
	var policy LogPolicy
	if strings.TrimSpace(raw) == "" || json.Unmarshal([]byte(raw), &policy) != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A log policy needs a JSON object")
	}
	if policy.Level != "" && !slices.Contains([]string{"info", "warn", "error", "debug"}, policy.Level) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose info, warn or error for a permanent level")
	}
	if policy.Level == "debug" {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Use a temporary debug window instead of a permanent debug level")
	}
	if policy.RetentionDays != nil && (*policy.RetentionDays < 0 || *policy.RetentionDays > 3650) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Retention must be between 0 and 3650 days")
	}
	if policy.DebugUntil == "" {
		return nil
	}
	if refusal := checkDebugWindow(policy.DebugUntil, now); refusal != nil {
		return refusal
	}
	if strings.TrimSpace(policy.Reason) == "" {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Temporary debug needs a reason")
	}
	return nil
}

func checkDebugWindow(raw string, now time.Time) *kernel.Error {
	until, err := time.Parse(time.RFC3339, raw)
	if err != nil || until.Before(now) || until.After(now.Add(30*time.Minute)) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Temporary debug must end within the next 30 minutes")
	}
	return nil
}
