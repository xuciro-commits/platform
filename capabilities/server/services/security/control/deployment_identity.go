package control

import (
	"fmt"
	"os"
	"strings"
)

func ConfigurePersonalTokens(issuer string) ([]byte, error) {
	key := os.Getenv("PLATFORM_PERSONAL_TOKEN_KEY")
	if key == "" && issuer == "" {
		return nil, nil
	}
	if len(key) < 32 {
		return nil, fmt.Errorf("PLATFORM_PERSONAL_TOKEN_KEY requires at least 32 bytes")
	}
	UseTokenKey([]byte(key))
	return []byte(key), nil
}

func HostAdministrators(list string) map[string]bool {
	out := map[string]bool{}
	for _, subject := range strings.Split(list, ",") {
		if subject = strings.TrimSpace(subject); subject != "" {
			out[subject] = true
		}
	}
	return out
}
