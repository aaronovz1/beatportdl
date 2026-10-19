package validator

import (
	"strings"
	"unicode"
)

func SafePathReplacement(value string) bool {
	return !strings.ContainsAny(value, `/\<>:"|?*`) && !strings.ContainsFunc(value, unicode.IsControl)
}

func PermittedValue[T comparable](value T, permittedValues ...T) bool {
	for i := range permittedValues {
		if value == permittedValues[i] {
			return true
		}
	}
	return false
}
