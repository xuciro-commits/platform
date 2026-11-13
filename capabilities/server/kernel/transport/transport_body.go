package transport

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
)

func DecodeOne[T any](w http.ResponseWriter, r *http.Request, limit int64) (*T, bool) {
	var value *T
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || value == nil || decoder.Decode(new(any)) != io.EOF {
		return nil, false
	}
	return value, true
}

type laneHolderKey struct{}

func WithLaneHolder(ctx context.Context, holder string) context.Context {
	return context.WithValue(ctx, laneHolderKey{}, holder)
}

func LaneHolderOf(ctx context.Context, fallback string) string {
	if holder, ok := ctx.Value(laneHolderKey{}).(string); ok && holder != "" {
		return holder
	}
	return fallback
}

func WriteCSV(w http.ResponseWriter, name string, body []byte) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename="+name+".csv")
	w.Write(body)
}
