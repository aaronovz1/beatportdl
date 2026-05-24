package beatport

import "testing"

func TestSanitizePathRemovesInvalidFilenameCharacters(t *testing.T) {
	got := SanitizePath(`Artist <Name>: "Track" Mix? * |`, "")
	want := "Artist ＜Name＞： ＂Track＂ Mix？ ＊ ｜"

	if got != want {
		t.Fatalf("SanitizePath() = %q, want %q", got, want)
	}
}

func TestSanitizeForPathRemovesPathSeparators(t *testing.T) {
	got := SanitizeForPath(`AC/DC \ Remix`)
	want := "AC／DC ＼ Remix"

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

func TestSanitizePathAvoidsReservedWindowsNames(t *testing.T) {
	got := SanitizePath("CON", "")
	want := "CON_"

	if got != want {
		t.Fatalf("SanitizePath() = %q, want %q", got, want)
	}
}

func TestSanitizePathAvoidsDotOnlyNames(t *testing.T) {
	got := SanitizePath("..", "")
	want := "．．"

	if got != want {
		t.Fatalf("SanitizePath() = %q, want %q", got, want)
	}
}

func TestSanitizePathNormalizesTrailingDots(t *testing.T) {
	got := SanitizePath("Kasablanca >", "")
	want := "Kasablanca ＞"

	if got != want {
		t.Fatalf("SanitizePath() = %q, want %q", got, want)
	}

	got = SanitizePath("Artist.", "")
	want = "Artist"
	if got != want {
		t.Fatalf("SanitizePath() = %q, want %q", got, want)
	}
}
