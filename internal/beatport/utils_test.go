package beatport

import "testing"

func TestSanitizePathRemovesInvalidFilenameCharacters(t *testing.T) {
	got := SanitizePath(`Artist <Name>: "Track" Mix? * |`, "")
	want := "Artist Name Track Mix"

	if got != want {
		t.Fatalf("SanitizePath() = %q, want %q", got, want)
	}
}

func TestSanitizeForPathRemovesPathSeparators(t *testing.T) {
	got := SanitizeForPath(`AC/DC \ Remix`)
	want := "ACDC Remix"

	if got != want {
		t.Fatalf("SanitizeForPath() = %q, want %q", got, want)
	}
}

func TestSanitizePathUsesConfiguredWhitespace(t *testing.T) {
	got := SanitizePath("Deep House Mix", "_")
	want := "Deep_House_Mix"

	if got != want {
		t.Fatalf("SanitizePath() = %q, want %q", got, want)
	}
}
