package platform

import "time"

type MonitoringView struct {
	Record
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
