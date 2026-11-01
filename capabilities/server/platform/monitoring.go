package platform

type MonitoringView struct {
	Record
	Title  string `json:"title" field:"required,search" source:"record"`
	Type   string `json:"type,omitempty" choices:"sync,build,automation,function,delivery" source:"record"`
	Status string `json:"status,omitempty" choices:"ok,failed,pending,running,warning,paused,cancelled,unknown" source:"record"`
	Owner  string `json:"owner" field:"readonly" source:"record"`
}
