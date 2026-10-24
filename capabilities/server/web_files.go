package platformserver

// The workspace's build, held in memory: the page, its hashed assets and their
// cache headers. A request for a file costs a map lookup and one write; a
// repeat visit costs a 304 or nothing at all.

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// webAsset is one file of the build.
type webAsset struct {
	body      []byte
	etag      string
	mediaType string
	immutable bool // a hashed name: the bytes behind it never change
}

// webFiles is the build, loaded once at startup. A release swap starts a new
// process, so the files here are the files this process was started with.
type webFiles struct {
	files map[string]webAsset
	index webAsset
	total int64
}

// maxWebBytes bounds what the host holds in memory for the workspace. A build
// larger than this is served from disk, as before.
const maxWebBytes = 64 << 20

var mediaTypes = map[string]string{
	".html":  "text/html; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".mjs":   "text/javascript; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".json":  "application/json",
	".svg":   "image/svg+xml",
	".png":   "image/png",
	".jpg":   "image/jpeg",
	".jpeg":  "image/jpeg",
	".webp":  "image/webp",
	".ico":   "image/x-icon",
	".woff2": "font/woff2",
	".woff":  "font/woff",
	".wasm":  "application/wasm",
	".txt":   "text/plain; charset=utf-8",
	".map":   "application/json",
}

// hashedName reports whether a build artifact's name carries its content hash,
// which is what makes it safe to cache for a year.
func hashedName(name string) bool {
	base := strings.TrimSuffix(name, path.Ext(name))
	_, hash, ok := strings.Cut(base, "-")
	return ok && len(hash) >= 8 && !strings.Contains(hash, ".")
}

// loadWeb reads the build once. A directory it cannot read is not served.
func loadWeb(dir string) *webFiles {
	files := &webFiles{files: map[string]webAsset{}}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return files
	}
	for _, entry := range entries {
		if entry.IsDir() {
			sub := filepath.Join(dir, entry.Name())
			assets, err := os.ReadDir(sub)
			if err != nil {
				continue
			}
			for _, asset := range assets {
				if asset.IsDir() {
					continue
				}
				body, err := os.ReadFile(filepath.Join(sub, asset.Name()))
				if err != nil {
					continue
				}
				key := "/" + entry.Name() + "/" + asset.Name()
				files.add(key, asset.Name(), body)
			}
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		files.add("/"+entry.Name(), entry.Name(), body)
	}
	if index, ok := files.files["/index.html"]; ok {
		files.index = index
	} else {
		files.index = webAsset{body: []byte("<!doctype html><title>Platform</title><p>The workspace is not built."), mediaType: "text/html; charset=utf-8"}
		files.index.etag = `"index"`
	}
	return files
}

func (f *webFiles) add(key, name string, body []byte) {
	if f.total+int64(len(body)) > maxWebBytes {
		return
	}
	sum := sha256.Sum256(body)
	mediaType := mediaTypes[path.Ext(name)]
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	f.files[key] = webAsset{body: body, etag: `"` + hex.EncodeToString(sum[:16]) + `"`, mediaType: mediaType,
		immutable: hashedName(name) && strings.HasPrefix(key, "/assets/")}
	f.total += int64(len(body))
}

// serve answers a file, the page for any other path, with revalidation.
func (f *webFiles) serve(w http.ResponseWriter, r *http.Request) {
	asset, ok := f.files[path.Clean("/"+r.URL.Path)]
	if !ok || r.URL.Path == "/" {
		asset = f.index
	}
	if asset.immutable {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	w.Header().Set("ETag", asset.etag)
	if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, asset.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", asset.mediaType)
	w.Header().Set("Content-Length", strconv.Itoa(len(asset.body)))
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_, _ = w.Write(asset.body)
}
