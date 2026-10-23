package redact

import (
	"strings"
	"testing"
)

func TestRedactsSecrets(t *testing.T) {
	cases := map[string]string{
		"login password=hunter2 ok":                      "login password=<redacted> ok",
		`{"api_key": "abc123", "user": "x"}`:             `{"api_key": "<redacted>", "user": "x"}`,
		"Authorization: Bearer abcdefghijklmnop":         "Authorization: Bearer <redacted>",
		"retry with token qwertyuiop123":                 "retry with token <redacted>",
		"db postgres://app:s3cret@db:5432/shop down":     "db postgres://<redacted>@db:5432/shop down",
		"key AKIAIOSFODNN7EXAMPLE used":                  "key <redacted> used",
		"gh ghp_abcdefghijklmnopqrstuvwxyz0123456789":    "gh <redacted>",
		"jwt eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.sig12": "jwt <redacted>",
		"mail anna@example.com sent":                     "mail <email> sent",
		"card 4111 1111 1111 1111 charged":               "card <card> charged",
		"-----BEGIN RSA PRIVATE KEY----- MIIEow":         "-----BEGIN RSA PRIVATE KEY-----<redacted>",
		"session_id=deadbeef42 user=7":                   "session_id=<redacted> user=7",
		"slack xoxb-1234567890-abcdefghij":               "slack <redacted>",
	}
	for in, want := range cases {
		if got := String(in); got != want {
			t.Errorf("String(%q)\n got %q\nwant %q", in, got, want)
		}
	}
}

func TestLeavesOrdinaryTextAlone(t *testing.T) {
	for _, s := range []string{
		"GET /api/orders 200 in 12ms",
		"order 1234567890 shipped",
		"user_id=42 status=open",
		"token=<*>",
		"cache hit ratio 0.93",
	} {
		if got := String(s); got != s {
			t.Errorf("String(%q) changed it to %q", s, got)
		}
	}
}

func TestLuhn(t *testing.T) {
	if !luhn("4111111111111111") || luhn("4111111111111112") || luhn("1234") {
		t.Fatal("luhn check is wrong")
	}
}

func FuzzString(f *testing.F) {
	for _, s := range []string{"password=x", "Bearer abcdefghij", "a@b.co", "4111 1111 1111 1111", ""} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		out := String(s)
		if strings.Contains(s, "password=hunter2") && strings.Contains(out, "hunter2") && !strings.Contains(s, "hunter2hunter2") {
			t.Fatalf("password leaked: %q -> %q", s, out)
		}
	})
}
