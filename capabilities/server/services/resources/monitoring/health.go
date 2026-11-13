package monitoring

import (
	"runtime/debug"
	"time"

	"platformserver/platform"
)

var Started = time.Now().UTC().Truncate(time.Second)

type ObservationHealth struct {
	Enabled       bool      `json:"enabled"`
	Pending       int       `json:"pending"`
	Dropped       int64     `json:"dropped"`
	Written       int64     `json:"written"`
	WriteFailures int64     `json:"writeFailures"`
	Purged        int64     `json:"purged"`
	QuotaBytes    int64     `json:"quotaBytes"`
	RetainedFrom  time.Time `json:"retainedFrom,omitzero"`
	LastWrite     time.Time `json:"lastWrite,omitzero"`
	LastPurge     time.Time `json:"lastPurge,omitzero"`
	Available     bool      `json:"available"`
}

type HealthCondition struct {
	Name   string `json:"name"`
	Ready  bool   `json:"ready"`
	Detail string `json:"detail,omitempty"`
}

type QueueHealth struct {
	App    string        `json:"app"`
	Depth  int           `json:"depth"`
	Oldest time.Duration `json:"-"`
	Age    float64       `json:"oldestSeconds"`
}

func Queues(now time.Time, apps []string, due func(app string) []time.Time) []QueueHealth {
	queues := []QueueHealth{}
	for _, app := range apps {
		dues := due(app)
		if len(dues) == 0 {
			continue
		}
		qh := QueueHealth{App: app, Depth: len(dues)}
		for _, at := range dues {
			if age := now.Sub(at); age > qh.Oldest {
				qh.Oldest = age
			}
		}
		qh.Age = qh.Oldest.Seconds()
		queues = append(queues, qh)
	}
	return queues
}

func OpenBreakers(breakers []platform.Breaker) int {
	open := 0
	for _, b := range breakers {
		if b.State != "closed" {
			open++
		}
	}
	return open
}

func Readiness(journalReady bool, journalDetail string, observations, workLoop bool) []HealthCondition {
	return []HealthCondition{
		{Name: "journal", Ready: journalReady, Detail: journalDetail},
		{Name: "observations", Ready: observations, Detail: map[bool]string{true: "", false: "operational log storage is unavailable"}[observations]},
		{Name: "work-loop", Ready: workLoop, Detail: map[bool]string{true: "", false: "business access is stopped"}[workLoop]},
	}
}

func Quarantined(reason string) []HealthCondition {
	return []HealthCondition{{Name: "journal", Ready: false, Detail: reason}, {Name: "observations", Ready: false, Detail: "tenant is quarantined"}, {Name: "work-loop", Ready: false, Detail: "tenant is quarantined"}}
}

func Built() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	revision, modified := "", false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if revision == "" {
		return ""
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	if modified {
		return revision + "-dirty"
	}
	return revision
}
