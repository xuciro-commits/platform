package platform

import "strconv"

const (
	FileApp            = "files"
	FileType           = "files.file"
	SchemaFileAttach   = "files.file.attach"
	SchemaFileDetach   = "files.file.detach"
	SchemaFileAnnotate = "files.file.annotate"
	SettingFileMax     = "max-size-mb"
)

// File is a file attached to a record.
type File struct {
	Record
	Name        string        `json:"name" field:"required,search" help:"The file's name as it was uploaded"`
	ContentType string        `json:"contentType" field:"readonly" title:"Type"`
	Size        int           `json:"size" field:"readonly" help:"Bytes"`
	Hash        string        `json:"hash" field:"readonly" help:"The SHA-256 of its bytes: where the store keeps them"`
	Target      string        `json:"target" field:"readonly" title:"Attached to" help:"<type>/<id> of the record"`
	By          string        `json:"by" field:"readonly" title:"Attached by"`
	Regions     []ImageRegion `json:"regions,omitempty" field:"readonly,aside" type:"json" title:"Image regions"`
}

const (
	MaxImageRegions          = 64
	MaxImageRegionLabelBytes = 256
	MaxImageRegionIDBytes    = 128
)

// A rectangle refers to immutable original raster bytes, in normalized image coordinates.
type ImageRegion struct {
	ID     string  `json:"id"`
	Label  string  `json:"label"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}
type ImageRegionsRequest struct {
	Hash    string        `json:"hash"`
	Regions []ImageRegion `json:"regions"`
}

// Max is the largest file the tenant accepts, in bytes.
func MaxFileSize(c Caller) int {
	mb, err := strconv.Atoi(c.Setting(SettingFileMax))
	if err != nil || mb <= 0 {
		mb = 25
	}
	return mb << 20
}
