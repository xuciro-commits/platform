package uaf

// DataFormat is the format of generated metamodel data, independent of the
// OMG standard version. Changing the encoder requires a new data format.
const DataFormat = 1

type Source struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
}

// GeneratedRelease preserves all runtime metamodel fields and identifies the
// complete original standard files used to generate them.
type GeneratedRelease struct {
	FormatVersion   int       `json:"formatVersion"`
	StandardVersion string    `json:"standardVersion"`
	Sources         []Source  `json:"sources"`
	Metamodel       Metamodel `json:"metamodel"`
}
