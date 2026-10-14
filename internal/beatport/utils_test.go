package beatport

import (
	"encoding/json"
	"testing"
)

func TestParseTemplatePreservesUnknownPlaceholders(t *testing.T) {
	t.Parallel()
	got := ParseTemplate("{artists} - {name} {name} {unknown}", map[string]string{"artists": "Artist", "name": "Track"})
	if want := "Artist - Track Track {unknown}"; got != want {
		t.Fatalf("ParseTemplate() = %q, want %q", got, want)
	}
}

func TestSanitizedStringUnmarshalRemovesEscapedWhitespace(t *testing.T) {
	t.Parallel()
	var value SanitizedString
	if err := json.Unmarshal([]byte(`"  Track\n\r\t (Original  Mix) "`), &value); err != nil {
		t.Fatal(err)
	}
	if got, want := value.String(), "Track (Original Mix)"; got != want {
		t.Fatalf("SanitizedString = %q, want %q", got, want)
	}
}

var benchmarkStringResult string

func BenchmarkParseTemplate(b *testing.B) {
	values := map[string]string{"artists": "Artist", "name": "Track", "mix_name": "Original Mix"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		benchmarkStringResult = ParseTemplate("{artists} - {name} ({mix_name})", values)
	}
}

func BenchmarkSanitizedStringUnmarshal(b *testing.B) {
	data := []byte(`"  Track\n\t (Original  Mix) "`)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var value SanitizedString
		_ = value.UnmarshalJSON(data)
		benchmarkStringResult = value.String()
	}
}

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
