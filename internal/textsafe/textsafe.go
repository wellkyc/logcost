package textsafe

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

func unsafeRune(r rune) bool {
	switch {
	case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f:
		return true
	case r == ' ', r == ' ':
		return true
	case r >= '‪' && r <= '‮', r >= '⁦' && r <= '⁩', r == '‎', r == '‏', r == '؜':
		return true
	}
	return false
}

func Clean(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unsafeRune(r) {
			return false
		}
	}
	return true
}

func Sanitize(s string) string {
	if Clean(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 16)
	for _, r := range strings.ToValidUTF8(s, "�") {
		switch {
		case r == '\t':
			b.WriteByte(' ')
		case !unsafeRune(r):
			b.WriteRune(r)
		case r < 0x100:
			fmt.Fprintf(&b, "\\x%02x", r)
		default:
			fmt.Fprintf(&b, "\\u%04x", r)
		}
	}
	return b.String()
}

func Truncate(s string, width int) string {
	if width <= 0 || utf8.RuneCountInString(s) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	runes := []rune(s)
	return string(runes[:width-1]) + "…"
}
