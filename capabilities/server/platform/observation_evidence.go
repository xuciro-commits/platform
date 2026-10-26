package platform

import "time"

// ObservationSampleSource identifies which connection revision produced a sample.
type ObservationSampleSource struct {
	Connection string `json:"connection"`
	Revision   string `json:"revision"`
	Generation string `json:"generation"`
	Sequence   string `json:"sequence,omitempty"`
	Clock      string `json:"clock,omitempty"`
}

// ObservationSample is one current reading with its quality and time.
type ObservationSample struct {
	Device     string                  `json:"device"`
	Signal     string                  `json:"signal"`
	Type       string                  `json:"type"`
	Value      any                     `json:"value,omitempty"`
	Unit       string                  `json:"unit,omitempty"`
	Quality    string                  `json:"quality"`
	At         time.Time               `json:"at"`
	ReceivedAt time.Time               `json:"receivedAt"`
	Reason     string                  `json:"reason,omitempty"`
	Source     ObservationSampleSource `json:"source"`
}

// ObservationCoverage is the source's completeness claim for a window.
type ObservationCoverage struct {
	AvailableSince time.Time `json:"availableSince"`
	CompleteUntil  time.Time `json:"completeUntil"`
	Truncated      bool      `json:"truncated"`
	Good           int64     `json:"good"`
	Bad            int64     `json:"bad"`
	Uncertain      int64     `json:"uncertain"`
	Stale          int64     `json:"stale"`
}
