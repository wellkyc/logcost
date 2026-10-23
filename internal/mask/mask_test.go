package mask

import (
	"strings"
	"testing"
)

func TestTokens(t *testing.T) {
	cases := map[string]string{
		"GET /api/users/123/orders 200 in 15ms":             "GET /api/users/<*>/orders <*> in <*>",
		"user 550e8400-e29b-41d4-a716-446655440000 deleted": "user <*> deleted",
		"from 10.0.0.12:8080 to fe80::1":                    "from <*> to <*>",
		"key=product:36516 ttl=219s size=2839":              "key=<*> ttl=<*> size=<*>",
		"took 1.25s (cpu 93%)":                              "took <*> (cpu <*>)",
		"hash a3f9c2d81b7e done":                            "hash <*> done",
		"at 2026-10-23T10:00:01Z":                           "at <*>",
		`status="500" reason="timeout"`:                     `status="<*>" reason="timeout"`,
		"https://shop.example.com/p/987?ref=mail opened":    "https://shop.example.com/p/<*>?<*> opened",
		"sent to anna@example.com":                          "sent to <*>",
		"worker-3 started":                                  "worker-3 started",
		"api v2 ready":                                      "api v2 ready",
		"[main] INFO Service started":                       "[main] INFO Service started",
	}
	for in, want := range cases {
		if got := strings.Join(Tokens(in), " "); got != want {
			t.Errorf("Tokens(%q)\n got %q\nwant %q", in, got, want)
		}
	}
}

func TestTokensCapsLongLines(t *testing.T) {
	line := strings.Repeat("word ", 1000)
	tokens := Tokens(line)
	if len(tokens) != MaxTokens+1 {
		t.Fatalf("got %d tokens", len(tokens))
	}
}

func TestLongTokensBecomeWildcards(t *testing.T) {
	if got := Token(strings.Repeat("a", MaxTokenLen+1)); got != Wildcard {
		t.Fatalf("got %d bytes", len(got))
	}
}

func TestKey(t *testing.T) {
	if Key("ttl=30s") != "ttl=" || Key("12:30:01") != "" || Key("plain") != "" {
		t.Fatal("Key is wrong")
	}
}

func TestNestedKeysDoNotExplode(t *testing.T) {
	token := strings.Repeat("a=", 30000) + "1"
	if got := Token(token); len(got) > len(token) {
		t.Fatalf("token grew from %d to %d bytes", len(token), len(got))
	}
}

func FuzzTokens(f *testing.F) {
	for _, s := range []string{"GET /a/1 200", "k=v", `"x"`, "a=b=c", "::", "/", "<*>", "[]"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		tokens := Tokens(s)
		if len(tokens) > MaxTokens+1 {
			t.Fatalf("too many tokens: %d", len(tokens))
		}
		for _, tok := range tokens {
			if tok == "" {
				t.Fatalf("empty token from %q", s)
			}
		}
	})
}
