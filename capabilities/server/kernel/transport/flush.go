package transport

import "net/http"

func flushWriter(w http.ResponseWriter) (http.Flusher, bool) {
	switch w := w.(type) {
	case http.Flusher:
		return w, true
	case interface{ Unwrap() http.ResponseWriter }:
		return flushWriter(w.Unwrap())
	default:
		return nil, false
	}
}
