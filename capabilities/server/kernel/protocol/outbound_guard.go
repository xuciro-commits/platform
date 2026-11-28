package protocol

import (
	"context"
	"net/netip"
	"strings"
)

type loopbackPackageKey struct{}

func WithLoopbackPackage(ctx context.Context) context.Context {
	return context.WithValue(ctx, loopbackPackageKey{}, true)
}

func IsLoopbackPackage(ctx context.Context) bool {
	ok, _ := ctx.Value(loopbackPackageKey{}).(bool)
	return ok
}

func IsRestrictedHostIP(host string, allowPrivate, allowLoopback bool) bool {
	cleaned := strings.TrimSpace(strings.Trim(host, "[]"))
	if zone := strings.IndexByte(cleaned, '%'); zone >= 0 {
		cleaned = cleaned[:zone]
	}
	addr, err := netip.ParseAddr(cleaned)
	if err != nil || !addr.IsValid() {
		return !allowPrivate
	}
	addr = addr.Unmap()
	if addr.IsLoopback() {
		return !(allowPrivate || allowLoopback)
	}
	if addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsMulticast() || addr.IsInterfaceLocalMulticast() || addr.IsUnspecified() {
		return !allowPrivate
	}
	if addr.Is4() {
		b := addr.As4()
		if b[0] == 0 || (b[0] == 100 && b[1]&0xc0 == 64) || (b[0] == 198 && (b[1] == 18 || b[1] == 19)) || (b[0] == 192 && b[1] == 0 && b[2] == 0) {
			return !allowPrivate
		}
	}
	return false
}
