package data

type ConnectionKind struct {
	ID                 string `json:"id"`
	Title              string `json:"title"`
	Kind               string `json:"kind"`
	AddressPlaceholder string `json:"addressPlaceholder,omitempty"`
}

type ConnectionKindPage struct {
	Items []ConnectionKind `json:"items"`
}

func ConnectionKinds() ConnectionKindPage {
	return ConnectionKindPage{Items: []ConnectionKind{
		{ID: "http", Kind: "http", Title: "HTTP / REST / files", AddressPlaceholder: "https://api.example.com/v1/"},
		{ID: "odata", Kind: "odata", Title: "OData (SAP Gateway)", AddressPlaceholder: "https://sap.example.com/sap/opu/odata/sap/API_PRODUCT_SRV/"},
		{ID: "postgres", Kind: "postgres", Title: "PostgreSQL (read-only)", AddressPlaceholder: "postgres://reader@db.example.com:5432/app?sslmode=require"},
	}}
}
