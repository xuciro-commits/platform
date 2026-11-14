package transport

import (
	"bytes"
	"net/http"
)

type LaneBuffer struct {
	target http.ResponseWriter
	status int
	body   bytes.Buffer
	after  func(http.ResponseWriter)
}

func NewLaneBuffer(target http.ResponseWriter) *LaneBuffer { return &LaneBuffer{target: target} }

func (b *LaneBuffer) Header() http.Header { return b.target.Header() }

func (b *LaneBuffer) WriteHeader(status int) {
	if b.status == 0 {
		b.status = status
	}
}

func (b *LaneBuffer) Write(p []byte) (int, error) {
	if b.status == 0 {
		b.status = http.StatusOK
	}
	return b.body.Write(p)
}

func (b *LaneBuffer) Release() {
	if b.after != nil && b.status == 0 && b.body.Len() == 0 {
		b.after(b.target)
		return
	}
	if b.status != 0 {
		b.target.WriteHeader(b.status)
	}
	_, _ = b.target.Write(b.body.Bytes())
}

func OutsideLane(w http.ResponseWriter, f func(http.ResponseWriter)) {
	if b, ok := w.(*LaneBuffer); ok {
		b.after = f
		return
	}
	f(w)
}
