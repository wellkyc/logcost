package parse

import (
	"strings"
	"testing"
	"time"
)

func init() {
	nowFunc = func() time.Time { return time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC) }
}

func TestTextLines(t *testing.T) {
	cases := []struct {
		line    string
		message string
		level   Level
		time    string
	}{
		{"2026-10-20T10:00:01.123Z INFO  [http] GET /healthz", "INFO  [http] GET /healthz", Info, "2026-10-20T10:00:01.123Z"},
		{"2026-10-20 10:00:01,250 WARNING disk almost full", "WARNING disk almost full", Warn, "2026-10-20T10:00:01.25Z"},
		{`127.0.0.1 - - [20/Oct/2026:10:00:01 +0000] "GET / HTTP/1.1" 200 512`, `127.0.0.1 - - [20/Oct/2026:10:00:01 +0000] "GET / HTTP/1.1" 200 512`, Unknown, "2026-10-20T10:00:01Z"},
		{"Oct 20 10:00:01 host sshd[42]: error: bad login", "host sshd[42]: error: bad login", Error, "2026-10-20T10:00:01Z"},
		{"no time here, just DEBUG text", "no time here, just DEBUG text", Debug, ""},
		{"I think this is fine", "I think this is fine", Unknown, ""},
	}
	for _, c := range cases {
		rec := Line(c.line)
		if rec.Message != c.message {
			t.Errorf("%q: message %q", c.line, rec.Message)
		}
		if rec.Level != c.level {
			t.Errorf("%q: level %v, want %v", c.line, rec.Level, c.level)
		}
		got := ""
		if rec.HasTime {
			got = rec.Time.UTC().Format(time.RFC3339Nano)
		}
		if got != c.time {
			t.Errorf("%q: time %q, want %q", c.line, got, c.time)
		}
	}
}

func TestJSONLines(t *testing.T) {
	rec := Line(`{"time":"2026-10-20T10:00:00Z","level":"warn","msg":"slow query","ms":812}`)
	if rec.Message != "slow query" || rec.Level != Warn || !rec.HasTime {
		t.Fatalf("got %+v", rec)
	}
	rec = Line(`{"ts":1792490400.5,"severity":"ERROR","message":"boom"}`)
	if rec.Message != "boom" || rec.Level != Error || !rec.HasTime || rec.Time.Year() != 2026 {
		t.Fatalf("got %+v", rec)
	}
	rec = Line(`{"level":50,"msg":"pino style"}`)
	if rec.Level != Error {
		t.Fatalf("numeric level: got %v", rec.Level)
	}
	rec = Line(`{"b":1,"a":2}`)
	if rec.Message != "{a,b}" {
		t.Fatalf("schema fallback: got %q", rec.Message)
	}
	rec = Line(`{"log":"2026-10-20T10:00:00Z ERROR inner text\n","stream":"stderr"}`)
	if rec.Level != Error || !strings.HasPrefix(rec.Message, "ERROR inner text") {
		t.Fatalf("docker style: got %+v", rec)
	}
}

func TestBrokenJSONIsText(t *testing.T) {
	rec := Line(`{"msg": "unterminated`)
	if rec.Message != `{"msg": "unterminated` {
		t.Fatalf("got %q", rec.Message)
	}
}

func TestImplausibleTimesAreIgnored(t *testing.T) {
	if _, ok := Time("1970-01-01T00:00:00Z"); ok {
		t.Fatal("1970 accepted")
	}
	if _, ok := Time("2999-01-01T00:00:00Z"); ok {
		t.Fatal("far future accepted")
	}
}

func FuzzLine(f *testing.F) {
	for _, s := range []string{"", "{}", `{"msg":1}`, "2026-10-20T10:00:00Z INFO x", "[", "{\"time\":\"x\""} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		rec := Line(s)
		if rec.Level >= LevelCount {
			t.Fatalf("bad level %d", rec.Level)
		}
	})
}
