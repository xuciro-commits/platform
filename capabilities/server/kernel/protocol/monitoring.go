package protocol

import (
	"platformserver/platform"
	"time"
)

const MonitoringQuotaType = "monitoring.quota"
const MonitoringBackupType = "monitoring.backup"

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

type QuotaThresholds struct {
	Soft int64 `json:"soft,omitempty"`
	Hard int64 `json:"hard,omitempty"`
}

type AICostThresholds struct {
	Soft float64 `json:"soft,omitempty"`
	Hard float64 `json:"hard,omitempty"`
}

type TenantQuota struct {
	Plan              string           `json:"plan,omitempty"`
	Records           QuotaThresholds  `json:"records,omitempty"`
	StorageBytes      QuotaThresholds  `json:"storageBytes,omitempty"`
	DailyWrites       QuotaThresholds  `json:"dailyWrites,omitempty"`
	AITokens          QuotaThresholds  `json:"aiTokens,omitempty"`
	AICost            AICostThresholds `json:"aiCost,omitempty"`
	InstalledPackages QuotaThresholds  `json:"installedPackages,omitempty"`
}

type QuotaPlan struct {
	ID          string      `json:"id"`
	Title       string      `json:"title"`
	Description string      `json:"description,omitempty"`
	Quota       TenantQuota `json:"quota"`
}

type QuotaRequest struct {
	Plan   string       `json:"plan,omitempty"`
	Quota  *TenantQuota `json:"quota,omitempty"`
	Reason string       `json:"reason,omitempty"`
}

type WorkspaceQuota struct {
	platform.Record
	Plan         string  `json:"plan,omitempty" source:"record"`
	RecordsSoft  int64   `json:"recordsSoft,omitempty" source:"record"`
	RecordsHard  int64   `json:"recordsHard,omitempty" source:"record"`
	StorageSoft  int64   `json:"storageSoft,omitempty" source:"record"`
	StorageHard  int64   `json:"storageHard,omitempty" source:"record"`
	WritesSoft   int64   `json:"writesSoft,omitempty" source:"record"`
	WritesHard   int64   `json:"writesHard,omitempty" source:"record"`
	AITokensSoft int64   `json:"aiTokensSoft,omitempty" source:"record"`
	AITokensHard int64   `json:"aiTokensHard,omitempty" source:"record"`
	AICostSoft   float64 `json:"aiCostSoft,omitempty" source:"record"`
	AICostHard   float64 `json:"aiCostHard,omitempty" source:"record"`
	PackagesSoft int64   `json:"packagesSoft,omitempty" source:"record"`
	PackagesHard int64   `json:"packagesHard,omitempty" source:"record"`
	Reason       string  `json:"reason,omitempty" source:"record"`
	AssignedBy   string  `json:"assignedBy,omitempty" field:"readonly" source:"record"`
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
	Quota             *TenantQuota  `json:"quota,omitempty"`
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

type BackupStatus struct {
	At      time.Time `json:"at"`
	Status  string    `json:"status"`
	Entries int64     `json:"entries"`
	Bytes   int64     `json:"bytes"`
	Error   string    `json:"error,omitempty"`
	By      string    `json:"by,omitempty"`
}

type BackupRequest struct {
	Status  string `json:"status,omitempty"`
	Entries int64  `json:"entries,omitempty"`
	Bytes   int64  `json:"bytes,omitempty"`
	Error   string `json:"error,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

type WorkspaceBackup struct {
	platform.Record
	At      time.Time `json:"at"`
	Status  string    `json:"status"`
	Entries int64     `json:"entries"`
	Bytes   int64     `json:"bytes"`
	Error   string    `json:"error,omitempty"`
	Reason  string    `json:"reason,omitempty"`
	By      string    `json:"by,omitempty"`
}

type TenantUsageSummary struct {
	Records           int64   `json:"records"`
	StorageBytes      int64   `json:"storageBytes"`
	WritesToday       int64   `json:"writesToday"`
	AITokens          int64   `json:"aiTokens"`
	AICost            float64 `json:"aiCost"`
	InstalledPackages int64   `json:"installedPackages"`
}
