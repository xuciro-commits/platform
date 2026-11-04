package objects

type InterfaceRecord struct {
	Type   string         `json:"type"`
	ID     string         `json:"id"`
	Record map[string]any `json:"record"`
}

type InterfaceRecordPage struct {
	Records []InterfaceRecord `json:"records"`
	Total   int               `json:"total"`
}
