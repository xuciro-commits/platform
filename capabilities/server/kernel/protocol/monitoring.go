package protocol

import (
	"platformserver/platform"
	"time"
)

type MonitoringView struct {
	platform.Record
	Title   string    `json:"title" field:"required,search" source:"record"`
	Family  string    `json:"family,omitempty" choices:"runs,logs" source:"record"`
	Query   string    `json:"query,omitempty" source:"record"`
	Time    string    `json:"time,omitempty" choices:"15m,1h,24h,7d,30d,custom" source:"record"`
	From    time.Time `json:"from,omitzero" source:"record"`
	To      time.Time `json:"to,omitzero" source:"record"`
	Columns string    `json:"columns,omitempty" source:"record"`
	Density string    `json:"density,omitempty" choices:"compact,comfortable" source:"record"`
	Type    string    `json:"type,omitempty" choices:"sync,build,automation,function,delivery" source:"record"`
	Status  string    `json:"status,omitempty" choices:"ok,failed,pending,running,warning,paused,cancelled,unknown" source:"record"`
	Owner   string    `json:"owner" field:"readonly" source:"record"`
}

type UsageSource struct {
	Key       string              `json:"key"`
	Title     string              `json:"title"`
	App       string              `json:"app,omitempty"`
	Value     int64               `json:"value"`
	Cost      float64             `json:"cost,omitempty"`
	ObjectSet *platform.ObjectSet `json:"objectSet,omitempty"`
	Path      string              `json:"path,omitempty"`
}

type UsageDay struct {
	Date  string  `json:"date"`
	Value int64   `json:"value"`
	Cost  float64 `json:"cost,omitempty"`
}

type UsageMetric struct {
	Kind      string        `json:"kind"`
	Unit      string        `json:"unit"`
	Used      int64         `json:"used"`
	Cost      float64       `json:"cost,omitempty"`
	SoftLimit int64         `json:"softLimit,omitempty"`
	HardLimit int64         `json:"hardLimit,omitempty"`
	SoftCost  float64       `json:"softCost,omitempty"`
	HardCost  float64       `json:"hardCost,omitempty"`
	Trend     []UsageDay    `json:"trend"`
	Sources   []UsageSource `json:"sources"`
}

type TenantUsage struct {
	Tenant            string        `json:"tenant"`
	At                time.Time     `json:"at"`
	Month             string        `json:"month"`
	Plan              string        `json:"plan,omitempty"`
	Records           int64         `json:"records"`
	StorageBytes      int64         `json:"storageBytes"`
	WritesToday       int64         `json:"writesToday"`
	WritesMonth       int64         `json:"writesMonth"`
	DailyWrites       []UsageDay    `json:"dailyWrites"`
	AITokens          int64         `json:"aiTokens"`
	AIInputTokens     int64         `json:"aiInputTokens"`
	AIOutputTokens    int64         `json:"aiOutputTokens"`
	AICalls           int64         `json:"aiCalls"`
	AICost            float64       `json:"aiCost"`
	InstalledPackages int64         `json:"installedPackages"`
	Metrics           []UsageMetric `json:"metrics"`
}
