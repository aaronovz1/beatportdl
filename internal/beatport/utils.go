package beatport

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
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

func (s *SanitizedString) UnmarshalJSON(data []byte) error {
	rawValue := string(bytes.Trim(data, `"`))
	r := strings.NewReplacer(
		"\\n", "",
		"\\r", "",
		"\\t", "",
	)
	sanitized := r.Replace(rawValue)
	*s = SanitizedString(strings.Join(strings.Fields(sanitized), " "))
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
	"CON":  {},
	"PRN":  {},
	"AUX":  {},
	"NUL":  {},
	"COM1": {},
	"COM2": {},
	"COM3": {},
	"COM4": {},
	"COM5": {},
	"COM6": {},
	"COM7": {},
	"COM8": {},
	"COM9": {},
	"LPT1": {},
	"LPT2": {},
	"LPT3": {},
	"LPT4": {},
	"LPT5": {},
	"LPT6": {},
	"LPT7": {},
	"LPT8": {},
	"LPT9": {},
}

func sanitizePathComponent(name string) string {
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, name)

	name = pathUnsafeReplacer.Replace(name)
	name = strings.Join(strings.Fields(name), " ")

	if name == "" {
		return "_"
	}

	if name == "." || name == ".." {
		name = strings.ReplaceAll(name, ".", "．")
	}

	name = strings.TrimRightFunc(name, func(r rune) bool {
		return unicode.IsSpace(r) || r == '.'
	})
	if name == "" {
		return "_"
	}

	if _, reserved := windowsReservedPathNames[strings.ToUpper(name)]; reserved {
		name += "_"
	}

	return name
}

func SanitizeForPath(s string) string {
	return sanitizePathComponent(s)
}

func SanitizePath(name string, whitespace string) string {
	if len(name) > 250 {
		name = name[:250]
	}

	name = sanitizePathComponent(name)
	if whitespace != "" {
		name = strings.ReplaceAll(name, " ", whitespace)
	}
	return name
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
	re := regexp.MustCompile(`\{(\w+)}`)
	result := re.ReplaceAllStringFunc(template, func(placeholder string) string {
		key := strings.Trim(placeholder, "{}")
		if value, found := values[key]; found {
			return value
		}
		return placeholder
	})
	return result
}

func storeUrl(id int64, entity, slug string, store Store) string {
	var domain string
	switch store {
	default:
		domain = "beatport.com"
	case StoreBeatsource:
		domain = "beatsource.com"
	}
	return fmt.Sprintf("https://www.%s/%s/%s/%d", domain, entity, slug, id)
}
