package beatport

import (
	"net/http"
	"strings"
	"testing"
)

func TestAPIErrorHintForAuthenticationFailure(t *testing.T) {
	err := &APIError{
		StatusCode: http.StatusForbidden,
		Detail:     "invalid credentials",
		Endpoint:   loginEndpoint,
		Store:      StoreBeatport,
	}

	if got := err.Hint(); !strings.Contains(got, "authentication failed") {
		t.Fatalf("Hint() = %q, want authentication guidance", got)
	}
}

func TestAPIErrorHintForQualityRestrictedDownload(t *testing.T) {
	err := &APIError{
		StatusCode: http.StatusForbidden,
		Detail:     "forbidden",
		Endpoint:   "/catalog/tracks/123/download/?quality=lossless",
		Store:      StoreBeatport,
	}

	got := err.Hint()
	if !strings.Contains(got, `requested quality "lossless"`) {
		t.Fatalf("Hint() = %q, want requested quality guidance", got)
	}
	if !strings.Contains(got, "subscription tier") {
		t.Fatalf("Hint() = %q, want subscription guidance", got)
	}
}

func TestAPIErrorHintForTerritorialRestriction(t *testing.T) {
	err := &APIError{
		StatusCode: http.StatusForbidden,
		Detail:     "not available in your territory",
		Endpoint:   "/catalog/releases/123/",
		Store:      StoreBeatport,
	}

	if got := err.Hint(); !strings.Contains(got, "territorial availability") {
		t.Fatalf("Hint() = %q, want territorial guidance", got)
	}
}

func TestAPIErrorHintForGenericBadRequest(t *testing.T) {
	err := &APIError{
		StatusCode: http.StatusBadRequest,
		Detail:     "",
		Endpoint:   "/catalog/releases/123/",
		Store:      StoreBeatport,
	}

	got := err.Hint()
	if !strings.Contains(got, "configured quality") {
		t.Fatalf("Hint() = %q, want configured quality guidance", got)
	}
	if !strings.Contains(got, "URL type") {
		t.Fatalf("Hint() = %q, want URL type guidance", got)
	}
}

func TestRequestedQualityFromEndpoint(t *testing.T) {
	got := requestedQualityFromEndpoint("/catalog/tracks/123/download/?quality=high")
	if got != "high" {
		t.Fatalf("requestedQualityFromEndpoint() = %q, want %q", got, "high")
	}
}
