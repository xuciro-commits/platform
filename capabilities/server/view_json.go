package platformserver

// The three reads every page opens — the catalog, the entity types and the
// definitions — are answered from memory: encoded once per rights signature and
// language, compressed once, and revalidated with an ETag. A page load costs a
// map lookup and a write; a reload costs a 304.

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sync"

	"platformserver/platform"
)

// viewBody is one encoded answer, with the compressed form made on demand.
type viewBody struct {
	plain []byte
	etag  string
	once  sync.Once
	gz    []byte
}

func (b *viewBody) compressed() []byte {
	b.once.Do(func() {
		var buf bytes.Buffer
		writer, _ := gzip.NewWriterLevel(&buf, 6)
		_, _ = writer.Write(b.plain)
		_ = writer.Close()
		b.gz = buf.Bytes()
	})
	return b.gz
}

// viewEntry holds what one rights signature is shown, encoded.
type viewEntry struct {
	entities    []platform.EntityInfo
	catalog     []platform.Action
	definitions []platform.Definition
	bodies      map[string]*viewBody // "kind:lang" → encoded answer
}

// cachedView returns the encoded answer for a kind ("entities", "catalog",
// "definitions"), a member and a language, building it once.
func (t *Tenant) cachedView(kind string, m platform.Member, lang string) *viewBody {
	gen, key := t.definitionsFingerprint(), rights(m)
	bodyKey := kind + ":" + lang
	if body, ok := t.views.body(gen, key, bodyKey); ok {
		return body
	}
	var value any
	switch kind {
	case "entities":
		value = t.Entities(m)
	case "catalog":
		value = t.Catalog(m)
	case "definitions":
		value = t.Definitions(m)
	default:
		return &viewBody{plain: []byte("null"), etag: `"null"`}
	}
	if lang != "" {
		value = t.i18n.Translate(value, lang)
	}
	plain, err := json.Marshal(value)
	if err != nil {
		plain = []byte("null")
	}
	sum := sha256.Sum256(plain)
	body := &viewBody{plain: plain, etag: `"` + hex.EncodeToString(sum[:16]) + `"`}
	t.views.putBody(gen, key, bodyKey, body)
	return body
}

// WriteView answers a cached read: 304 when the client already holds it, the
// compressed form when it asks for one, the plain form otherwise. The body is
// already encoded, so the edge must not encode it again.
func WriteView(w http.ResponseWriter, r *http.Request, body *viewBody) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	w.Header().Set("ETag", body.etag)
	if match := r.Header.Get("If-None-Match"); match != "" && contentTypeMatches(match, body.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if noCompression, ok := w.(interface{ NoCompression() }); ok {
		noCompression.NoCompression()
	}
	if acceptsGzip(r) {
		compressed := body.compressed()
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Length", itoa(len(compressed)))
		_, _ = w.Write(compressed)
		return
	}
	w.Header().Set("Content-Length", itoa(len(body.plain)))
	_, _ = w.Write(body.plain)
}

func contentTypeMatches(header, etag string) bool {
	for _, candidate := range bytes.Split([]byte(header), []byte(",")) {
		if bytes.Equal(bytes.TrimSpace(candidate), []byte(etag)) {
			return true
		}
	}
	return false
}

func itoa(n int) string {
	var digits [20]byte
	i := len(digits)
	if n == 0 {
		return "0"
	}
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	return string(digits[i:])
}
