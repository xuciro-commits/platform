package platform

// SystemSource is the immutable source of the running system, supplied by
// deployment. File drafts overlay it; they never read arbitrary host paths.
type SystemSource struct {
	Version int
	Digest  string
	Files   []SystemSourceFile
	Read    func(string) (string, error)
}

type SystemSourceFile struct {
	Path     string `json:"path"`
	Digest   string `json:"digest"`
	Size     int    `json:"size"`
	Mode     uint32 `json:"mode"`
	Editable bool   `json:"editable"`
}
