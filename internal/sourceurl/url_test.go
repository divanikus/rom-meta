package sourceurl

import (
	"strings"
	"testing"
)

func TestStripSecretsRemovesSensitiveQueryAndUserInfo(t *testing.T) {
	raw := "https://user:pass@example.test/media.php?media=box&softname=RomMeta&ssid=user&sspassword=pass&devid=dev&devpassword=devpass&token=secret"

	sanitized := StripSecrets(raw)

	if sanitized != "https://example.test/media.php?media=box" {
		t.Fatalf("unexpected sanitized URL: %s", sanitized)
	}
	for _, secret := range []string{"user:pass", "softname", "ssid", "sspassword", "devid", "devpassword", "token", "secret"} {
		if strings.Contains(sanitized, secret) {
			t.Fatalf("sanitized URL still contains %q: %s", secret, sanitized)
		}
	}
}

func TestAddQueryMergesCredentialsInMemory(t *testing.T) {
	signed := AddQuery("https://example.test/media.php?media=box", map[string]string{
		"ssid":       "user",
		"sspassword": "pass",
	})

	if !strings.Contains(signed, "media=box") || !strings.Contains(signed, "ssid=user") || !strings.Contains(signed, "sspassword=pass") {
		t.Fatalf("unexpected signed URL: %s", signed)
	}
}
