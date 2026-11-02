package host

import (
	"context"
	"io"
)

func (h hostView) MediaBytes(hash string) ([]byte, error) {
	reader, _, err := h.t.files().Get(context.Background(), h.t.ID+"/"+hash)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

func (h hostView) StoreMedia(hash, contentType string, raw []byte) error {
	return h.t.files().Put(context.Background(), h.t.ID+"/"+hash, raw, contentType)
}
