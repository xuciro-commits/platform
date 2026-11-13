package control

import (
	"fmt"
	"os"
	"strings"
)

func ConfigurePersonalTokens(issuer string) error {
	if issuer == "" {
		return nil
	}
	key := os.Getenv("PLATFORM_PERSONAL_TOKEN_KEY")
	if len(key) < 32 {
		return fmt.Errorf("OIDC delivery requires PLATFORM_PERSONAL_TOKEN_KEY of at least 32 bytes")
	}
	UseTokenKey([]byte(key))
	return nil
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
