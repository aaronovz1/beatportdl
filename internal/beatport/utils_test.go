package beatport

import (
	"encoding/json"
	"path/filepath"
	"strings"
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

func TestSanitizedStringDecodesJSONEscapes(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  string
	}{
		{`"Caf\u00e9"`, "Caf\u00e9"},
		{"\"Caf\u00e9\"", "Caf\u00e9"},
		{`"Quote \"Mix\""`, `Quote "Mix"`},
		{`"Slash \/ Mix"`, "Slash / Mix"},
		{`"One\nTwo\rThree\tFour"`, "One Two Three Four"},
		{`"Literal \\n and \\t"`, `Literal \n and \t`},
		{`"Literal \\u00e9"`, `Literal \u00e9`},
		{`"Clef \ud834\udd1e"`, "Clef \U0001d11e"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			var got SanitizedString
			if err := json.Unmarshal([]byte(tc.input), &got); err != nil {
				t.Fatal(err)
			}
			if got.String() != tc.want {
				t.Fatalf("decoded name = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSanitizedStringRejectsNonStringJSONWithoutChangingValue(t *testing.T) {
	for _, input := range []string{`123`, `true`, `{}`, `[]`, `"bad\x"`} {
		t.Run(input, func(t *testing.T) {
			value := SanitizedString("Keep")
			if err := json.Unmarshal([]byte(input), &value); err == nil {
				t.Error("non-string or malformed JSON accepted")
			}
			if value != "Keep" {
				t.Fatalf("failed decode changed value to %q", value)
			}
		})
	}
}

func TestSanitizedStringNullPreservesExistingValue(t *testing.T) {
	for _, previous := range []string{"", "Keep", " A  B \n"} {
		value := SanitizedString(previous)
		if err := json.Unmarshal([]byte(`null`), &value); err != nil || string(value) != previous {
			t.Fatalf("JSON null = %q, %v; want unchanged value %q", value, err, previous)
		}
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

func TestSanitizePathPreservesSafeWhitespaceReplacement(t *testing.T) {
	for _, tc := range []struct{ replacement, want string }{
		{"  ", "Deep  House  Mix"},
		{"_  _", "Deep_  _House_  _Mix"},
		{"\u00a0", "Deep\u00a0House\u00a0Mix"},
	} {
		if got := SanitizePath("Deep House Mix", tc.replacement); got != tc.want {
			t.Errorf("replacement %q produced %q, want %q", tc.replacement, got, tc.want)
		}
	}
}

func TestWhitespaceReplacementCannotIntroducePathSyntax(t *testing.T) {
	for _, replacement := range []string{`/../../`, `\..\..\`, `:`, `*`, `?`, `"`, `<`, `>`, `|`, "\x00", "\n", "\u0080"} {
		t.Run(replacement, func(t *testing.T) {
			component := SanitizePath("A B", replacement)
			if strings.ContainsAny(component, "/\\<>:\"|?*\x00\n\r\t\u0080") {
				t.Errorf("replacement introduced unsafe component %q", component)
			}
			base := t.TempDir()
			rel, err := filepath.Rel(base, filepath.Join(base, component))
			if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				t.Errorf("component escapes destination: %q, %v", rel, err)
			}
			directory := SanitizeDirectoryPath("Genre Name/Playlist Name", replacement)
			if len(strings.Split(directory, "/")) != 2 || strings.Contains(directory, "\\") {
				t.Errorf("replacement changed template hierarchy: %q", directory)
			}
		})
	}
}

func TestWhitespaceReplacementCannotCreateReservedName(t *testing.T) {
	if got := SanitizePath("CO N", "\x00"); got != "CON_" {
		t.Fatalf("replacement created reserved filename %q, want CON_", got)
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
