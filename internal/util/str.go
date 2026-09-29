package util

import (
	"slices"
	"strings"
)

func FirstNonEmpty(ss ...*string) string {
	for _, s := range ss {
		if s != nil && *s != "" {
			return *s
		}
	}
	return ""
}

// StrSliceEquals reports whether v equals s or any element of ss, ignoring
// case (Unicode simple folding, without allocating lowered copies).
func StrSliceEquals(s string, ss []string, v string) bool {
	return strings.EqualFold(s, v) || slices.ContainsFunc(ss, func(el string) bool {
		return strings.EqualFold(el, v)
	})
}
