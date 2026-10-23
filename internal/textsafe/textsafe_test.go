package textsafe

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSanitizeKeepsNormalText(t *testing.T) {
	for _, s := range []string{"", "GET /api 200", "привет, мир", "emoji 🚀 ok"} {
		if got := Sanitize(s); got != s {
			t.Errorf("Sanitize(%q) = %q", s, got)
		}
	}
}

func TestSanitizeEscapesTerminalControl(t *testing.T) {
	cases := map[string]string{
		"\x1b[31mred\x1b[0m":   `\x1b[31mred\x1b[0m`,
		"bell\a":               `bell\x07`,
		"back\rspace":          `back\x0dspace`,
		"tab\there":            "tab here",
		"osc \x1b]0;title\x07": `osc \x1b]0;title\x07`,
		"c1 \u009b31m":         `c1 \x9b31m`,
		"del\x7f":              `del\x7f`,
	}
	for in, want := range cases {
		if got := Sanitize(in); got != want {
			t.Errorf("Sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeEscapesBidiOverrides(t *testing.T) {
	override := string(rune(0x202e))
	got := Sanitize("user" + override + "txt.exe")
	if strings.Contains(got, override) || !strings.Contains(got, `\u202e`) {
		t.Errorf("bidi override not escaped: %q", got)
	}
}

func TestSanitizeFixesInvalidUTF8(t *testing.T) {
	got := Sanitize("bad \xff\xfe bytes")
	if !utf8.ValidString(got) {
		t.Errorf("result is not valid UTF-8: %q", got)
	}
}

func TestTruncate(t *testing.T) {
	if got := Truncate("hello world", 5); got != "hell…" {
		t.Errorf("got %q", got)
	}
	if got := Truncate("привет", 3); got != "пр…" {
		t.Errorf("got %q", got)
	}
	if got := Truncate("short", 0); got != "short" {
		t.Errorf("got %q", got)
	}
}

func FuzzSanitize(f *testing.F) {
	for _, s := range []string{"plain", "\x1b[2J", "\u202e", "\xff", "a\tb"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		out := Sanitize(s)
		if !Clean(out) {
			t.Fatalf("Sanitize(%q) left unsafe characters: %q", s, out)
		}
	})
}
