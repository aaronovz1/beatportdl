package beatport

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
	"unspok3n/beatportdl/internal/validator"
)

type SanitizedString string
type Duration int
type NamingPreferences struct {
	Template           string
	Whitespace         string
	ArtistsLimit       int
	ArtistsShortForm   string
	TrackNumberPadding int
	KeySystem          string
}

func (d *Duration) Display() string {
	seconds := *d / 1000
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	remainingSeconds := seconds % 60
	if hours > 0 {
		return fmt.Sprintf("%02d-%02d-%02d", hours, minutes, remainingSeconds)
	}
	return fmt.Sprintf("%02d-%02d", minutes, remainingSeconds)
}

var templatePlaceholderRegexp = regexp.MustCompile(`\{(\w+)}`)

func (s *SanitizedString) UnmarshalJSON(data []byte) error {
	var rawValue *string
	if err := json.Unmarshal(data, &rawValue); err != nil {
		return err
	}
	if rawValue == nil {
		return nil
	}
	*s = SanitizedString(strings.Join(strings.Fields(*rawValue), " "))
	return nil
}

func (s *SanitizedString) String() string {
	return string(*s)
}

var pathUnsafeReplacer = strings.NewReplacer(
	"\\", "＼",
	"/", "／",
	"<", "＜",
	">", "＞",
	":", "：",
	"\"", "＂",
	"|", "｜",
	"?", "？",
	"*", "＊",
)

var windowsReservedPathNames = map[string]struct{}{
	"CON":       {},
	"CONIN$":    {},
	"CONOUT$":   {},
	"PRN":       {},
	"AUX":       {},
	"NUL":       {},
	"COM1":      {},
	"COM2":      {},
	"COM3":      {},
	"COM4":      {},
	"COM5":      {},
	"COM6":      {},
	"COM7":      {},
	"COM8":      {},
	"COM9":      {},
	"LPT1":      {},
	"LPT2":      {},
	"LPT3":      {},
	"LPT4":      {},
	"LPT5":      {},
	"LPT6":      {},
	"LPT7":      {},
	"LPT8":      {},
	"LPT9":      {},
	"COM\u00b9": {},
	"COM\u00b2": {},
	"COM\u00b3": {},
	"LPT\u00b9": {},
	"LPT\u00b2": {},
	"LPT\u00b3": {},
}

const maxPathComponentBytes = 255

func cleanPathComponent(name string) string {
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)

	name = pathUnsafeReplacer.Replace(name)
	name = strings.Join(strings.Fields(name), " ")

	if name == "." || name == ".." {
		name = strings.ReplaceAll(name, ".", "．")
	}
	return trimPathComponent(name)
}

func trimPathComponent(name string) string {
	return strings.TrimRightFunc(name, func(r rune) bool {
		return unicode.IsSpace(r) || r == '.'
	})
}

func truncatePathComponent(name string, limit int) string {
	if len(name) > limit {
		for !utf8.RuneStart(name[limit]) {
			limit--
		}
		name = name[:limit]
	}
	return trimPathComponent(name)
}

func safePathBasename(name string, limit int) string {
	name = truncatePathComponent(name, limit)
	if name == "" {
		return "_"
	}

	base, rest, hasExtension := strings.Cut(name, ".")
	base = strings.TrimRight(base, " ")
	if _, reserved := windowsReservedPathNames[strings.ToUpper(base)]; reserved {
		name = base + "_"
		if hasExtension {
			name += "." + rest
		}
		return truncatePathComponent(name, limit)
	}
	return name
}

func SanitizeForPath(s string) string {
	// Template fields are not limited independently of the rendered component.
	name := cleanPathComponent(s)
	return safePathBasename(name, len(name)+1)
}

func SanitizePath(name string, whitespace string) string {
	return sanitizePath(name, whitespace, maxPathComponentBytes)
}

func sanitizePath(name, whitespace string, limit int) string {
	name = cleanPathComponent(name)
	if whitespace != "" {
		name = strings.ReplaceAll(name, " ", whitespace)
		if !validator.SafePathReplacement(whitespace) {
			name = cleanPathComponent(name)
		}
	}
	name = strings.ToValidUTF8(name, "\ufffd")
	if name == "." || name == ".." {
		name = strings.ReplaceAll(name, ".", "．")
	}
	return safePathBasename(name, limit)
}

// FitTrackFilename budgets space for the generated collision marker and audio
// extension without applying whitespace preferences to them.
func FitTrackFilename(stem, suffix string) string {
	return safePathBasename(stem, maxPathComponentBytes-len(suffix)) + suffix
}

func SanitizeDirectoryPath(name string, whitespace string) string {
	components := strings.Split(name, "/")
	for i, component := range components {
		components[i] = SanitizePath(component, whitespace)
	}
	return strings.Join(components, "/")
}

func NumberWithPadding(value, total, padding int) string {
	if padding == 0 {
		padding = len(strconv.Itoa(total))
	}
	return fmt.Sprintf("%0*d", padding, value)
}

func ParseTemplate(template string, values map[string]string) string {
	result := templatePlaceholderRegexp.ReplaceAllStringFunc(template, func(placeholder string) string {
		key := strings.Trim(placeholder, "{}")
		if value, found := values[key]; found {
			return value
		}
		return placeholder
	})
	return result
}

func storeUrl(id int64, entity, slug string) string {
	return fmt.Sprintf("https://www.beatport.com/%s/%s/%d", entity, slug, id)
}
