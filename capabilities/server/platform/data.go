package platform

import "time"

const ScheduleType = "build.schedule"
const SchemaScheduleRan = "build.schedule.ran"

type Schedule struct {
	Record
	Name              string     `json:"name" field:"required,search"`
	Title             string     `json:"title" field:"required,search"`
	Pipeline          string     `json:"pipeline" field:"required" ref:"build.pipeline"`
	Every             string     `json:"every" field:"required"`
	Enabled           bool       `json:"enabled"`
	LastRun           *time.Time `json:"lastRun,omitempty" field:"readonly"`
	Runner            string     `json:"runner,omitempty" field:"readonly"`
	RunnerScopes      string     `json:"runnerScopes,omitempty" field:"readonly" type:"json"`
	DefinitionVersion int        `json:"definitionVersion,omitempty" field:"readonly"`
	Published         string     `json:"published,omitempty" field:"readonly" type:"longtext"`
	Versions          []string   `json:"definitionVersions,omitempty" field:"readonly" type:"json"`
}

func (s Schedule) NextDue() *time.Time {
	if !s.Enabled {
		return nil
	}
	if s.LastRun == nil {
		return nil
	}
	every, _ := time.ParseDuration(s.Every)
	next := s.LastRun.Add(every)
	return &next
}
