package platform

type AppConcept struct {
	ID     string     `json:"id"`
	Title  string     `json:"title"`
	Assets []AssetRef `json:"assets"`
}

type ApplicationHeader struct {
	Variant   string                  `json:"variant"`
	Title     string                  `json:"title"`
	Logo      string                  `json:"logo,omitempty"`
	Collapsed bool                    `json:"collapsed,omitempty"`
	Items     []ApplicationHeaderItem `json:"items"`
}

type ApplicationHeaderItem struct {
	Kind   string   `json:"kind"`
	Pages  []string `json:"pages,omitempty"`
	Label  string   `json:"label,omitempty"`
	Text   string   `json:"text,omitempty"`
	Action string   `json:"action,omitempty"`
}

func (v PageVariable) IsWritable() bool { return v.Mode == "state" || v.Mode == "shared" && v.Writable }

func (a Application) QueryPage() Page {
	return Page{Format: 4, InputSchema: PageInputSchema{profile: a.UIProfile, Variables: a.Variables, Queries: a.Queries}}
}
