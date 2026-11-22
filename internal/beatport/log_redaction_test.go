package beatport

import (
	"strings"
	"testing"
)

func TestRedactLogRemovesPublishedAuthSecrets(t *testing.T) {
	auth := NewAuth("fixture-account", "fixture-password", "unused")
	auth.tokenPair = &tokenPair{AccessToken: "fixture-access", RefreshToken: "fixture-refresh"}
	inst := &Beatport{auth: auth}
	message := inst.RedactLog("fixture-account fixture-password fixture-access fixture-refresh https://proxy-user:proxy-pass@example.test/audio?signature=private-value#private-fragment")
	for _, secret := range []string{"fixture-account", "fixture-password", "fixture-access", "fixture-refresh", "proxy-user", "proxy-pass", "private-value", "private-fragment"} {
		if strings.Contains(message, secret) {
			t.Errorf("log retained %q: %s", secret, message)
		}
	}
	if !strings.Contains(message, "example.test/audio") {
		t.Fatal("request context missing")
	}
}

func TestSanitizeLogMessageCoversMixedCaseURLSchemes(t *testing.T) {
	for _, scheme := range []string{"HTTP", "HTTPS", "hTtPs"} {
		t.Run(scheme, func(t *testing.T) {
			msg := SanitizeLogMessage("request failed [" + scheme + "://name:fixture-password@example.test/audio?signature=fixture-signature#fixture-fragment]")
			if strings.Contains(msg, "fixture-") || !strings.Contains(msg, "example.test/audio?redacted") {
				t.Fatalf("URL was not redacted: %q", msg)
			}
		})
	}
}
