package platform

import (
	"context"
	"io"
	"strconv"
	"time"
)

const (
	FileApp            = "files"
	FileType           = "files.file"
	SchemaFileAttach   = "files.file.attach"
	SchemaFileDetach   = "files.file.detach"
	SchemaFileAnnotate = "files.file.annotate"
	SettingFileMax     = "max-size-mb"
)

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

func MaxFileSize(c Caller) int {
	mb, err := strconv.Atoi(c.Setting(SettingFileMax))
	if err != nil || mb <= 0 {
		mb = 25
	}
	return mb << 20
}

type FileHost interface {
	Readable(m Member, ref string, now time.Time) bool
	Stored(tenant, hash string) bool
}

type AttachedFiles interface{ AttachFiles(FileHost) }

type FileStore interface {
	Put(ctx context.Context, key string, data []byte, contentType string) error
	Get(ctx context.Context, key string) (io.ReadCloser, int64, error)
	Exists(ctx context.Context, key string) bool
	Delete(ctx context.Context, key string) error
}

type FileInventory interface {
	Inventory(context.Context, string, string, int) (FileInventoryPage, error)
}

type StoredFile struct {
	Key      string
	Size     int64
	Modified time.Time
}

type FileInventoryPage struct {
	Files []StoredFile
	Next  string
}

const MaxFileInventoryPage = 1024
