package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

type ApplicationRun struct {
	Applications []AssetBinding `json:"applications,omitempty"`
	Subject      string         `json:"subject,omitempty"`
	ID           string         `json:"id"`
	Kind         string         `json:"kind"`
	Title        string         `json:"title"`
	Resource     *AssetRef      `json:"resource,omitempty"`
	ResourceName string         `json:"resourceName"`
	State        string         `json:"state"`
	Version      string         `json:"version,omitempty"`
	Release      string         `json:"release,omitempty"`
	Dependencies string         `json:"dependencies,omitempty"`
	Module       string         `json:"module,omitempty"`
	Node         string         `json:"node,omitempty"`
	Error        string         `json:"error,omitempty"`
	Association  string         `json:"association"`
	Shared       []string       `json:"shared"`
	Created      time.Time      `json:"created"`
	AITokens     int            `json:"aiTokens,omitempty"`
	AIUSD        float64        `json:"aiUsd,omitempty"`
	CostReported bool           `json:"costReported,omitempty"`
}

type ApplicationProfile struct {
	ExpectedRecords map[string]int64  `json:"expectedRecords,omitempty"`
	ExpectedRuns    int64             `json:"expectedRuns,omitempty"`
	Retention       map[string]string `json:"retention,omitempty"`
	RPO             string            `json:"rpo,omitempty"`
	RTO             string            `json:"rto,omitempty"`
}

func (p *ApplicationProfile) UnmarshalJSON(raw []byte) error {
	type declaration ApplicationProfile
	var decoded declaration
	reader := json.NewDecoder(bytes.NewReader(raw))
	reader.DisallowUnknownFields()
	if err := reader.Decode(&decoded); err != nil {
		return err
	}
	if reader.Decode(new(any)) != io.EOF {
		return fmt.Errorf("application profile has trailing data")
	}
	*p = ApplicationProfile(decoded)
	return nil
}
