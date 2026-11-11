// Package slug builds URL-safe slugs from titles.
package slug

import (
	"strings"
	"unicode"
)

const maxLength = 120

// Make converts arbitrary text into a lowercase, dash-separated slug.
func Make(input string) string {
	var builder strings.Builder
	builder.Grow(len(input))
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(input)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastDash = false
		case unicode.IsSpace(r) || r == '-' || r == '_' || r == '.' || r == '/' || r == '&':
			if builder.Len() > 0 && !lastDash {
				builder.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(builder.String(), "-")
	if len(out) > maxLength {
		out = strings.Trim(out[:maxLength], "-")
	}
	if out == "" {
		out = "item"
	}
	return out
}
