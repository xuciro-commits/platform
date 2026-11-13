package transport

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"strings"
	"sync/atomic"
	"time"
)

var fallbackCorrelationID atomic.Uint64

func RandomHex(size int) string {
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err == nil {
		return hex.EncodeToString(raw)
	}
	var fallback [16]byte
	binary.BigEndian.PutUint64(fallback[:8], uint64(time.Now().UnixNano()))
	binary.BigEndian.PutUint64(fallback[8:], fallbackCorrelationID.Add(1))
	copy(raw, fallback[:])
	return hex.EncodeToString(raw)
}

func Traceparent(value string) (string, string) {
	parts := strings.Split(value, "-")
	if len(parts) < 4 || len(parts[0]) != 2 || len(parts[3]) != 2 || parts[0] == "ff" || parts[0] == "00" && len(parts) != 4 {
		return "", ""
	}
	if len(parts[1]) != 32 || len(parts[2]) != 16 {
		return "", ""
	}
	for _, part := range []string{parts[0], parts[3], parts[1], parts[2]} {
		if _, err := hex.DecodeString(part); err != nil {
			return "", ""
		}
	}
	if strings.Trim(parts[1], "0") == "" || strings.Trim(parts[2], "0") == "" {
		return "", ""
	}
	return strings.ToLower(parts[1]), strings.ToLower(parts[2])
}
