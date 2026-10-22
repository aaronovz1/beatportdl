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
