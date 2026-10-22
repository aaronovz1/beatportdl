package beatport

import (
	"net/url"
	"regexp"
	"strings"
)

var logURLPattern = regexp.MustCompile(`https?://[^\s"'<>]+`)

// SanitizeLogMessage retains request destinations but never URL credentials,
// fragments, signed query values, or the caller's known account secrets.
func SanitizeLogMessage(message string, secrets ...string) string {
	message = logURLPattern.ReplaceAllStringFunc(message, func(raw string) string {
		u, err := url.Parse(raw)
		if err != nil {
			return "[redacted-url]"
		}
		u.User = nil
		u.Fragment = ""
		if u.RawQuery != "" {
			u.RawQuery = "redacted"
		}
		return u.String()
	})
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		for _, value := range []string{secret, url.QueryEscape(secret), url.PathEscape(secret)} {
			message = strings.ReplaceAll(message, value, "[redacted]")
		}
	}
	return message
}

func (b *Beatport) RedactLog(message string) string {
	if b == nil || b.auth == nil {
		return SanitizeLogMessage(message)
	}
	secrets := []string{b.auth.username, b.auth.password}
	if token := b.auth.snapshot(); token != nil {
		secrets = append(secrets, token.AccessToken, token.RefreshToken)
	}
	return SanitizeLogMessage(message, secrets...)
}
