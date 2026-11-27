package avatarref

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

func Default(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return fmt.Sprintf("default/global/gl-%03d", int(sum[0])%20+1)
}

func Valid(value string) bool {
	if value == "" {
		return true
	}
	if rest, ok := strings.CutPrefix(value, "default/"); ok {
		if len(rest) == 2 && rest >= "01" && rest <= "20" {
			return true
		}
		pack, item, found := strings.Cut(rest, "/")
		if !found || pack == "" || item == "" || len(pack) > 24 || len(item) > 32 || strings.Contains(item, "/") {
			return false
		}
		return isAvatarSlug(pack) && isAvatarSlug(item)
	}
	if hex, ok := strings.CutPrefix(value, "sha256:"); ok && len(hex) == 64 {
		for _, c := range hex {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
				return false
			}
		}
		return true
	}
	return false
}

func isAvatarSlug(s string) bool {
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			return false
		}
	}
	return true
}
