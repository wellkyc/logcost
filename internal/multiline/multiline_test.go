package multiline

import (
	"strings"
	"testing"
)

func events(input string) []string {
	var s Splitter
	var out []string
	for _, line := range strings.SplitAfter(input, "\n") {
		if line == "" {
			continue
		}
		if s.Continues([]byte(line)) && len(out) > 0 {
			out[len(out)-1] += line
		} else {
			out = append(out, line)
		}
	}
	return out
}

func TestJavaStackTrace(t *testing.T) {
	input := `2026-10-20 10:00:00 ERROR [main] OrderService - failed to save order 42
java.lang.IllegalStateException: connection closed
	at com.shop.db.Pool.get(Pool.java:88)
	at com.shop.OrderService.save(OrderService.java:31)
Caused by: java.net.SocketException: Broken pipe
	at java.base/sun.nio.ch.NioSocketImpl.write(NioSocketImpl.java:420)
	... 12 more
2026-10-20 10:00:01 INFO [main] OrderService - retrying
`
	got := events(input)
	if len(got) != 2 {
		t.Fatalf("got %d events: %q", len(got), got)
	}
	if !strings.HasSuffix(got[0], "... 12 more\n") {
		t.Fatalf("first event: %q", got[0])
	}
}

func TestPythonTraceback(t *testing.T) {
	input := `2026-10-20 10:00:00,123 ERROR worker crashed
Traceback (most recent call last):
  File "/app/worker.py", line 10, in run
    process(job)
ValueError: bad job

During handling of the above exception, another exception occurred:

Traceback (most recent call last):
  File "/app/worker.py", line 12, in run
    report(err)
RuntimeError: report failed
2026-10-20 10:00:01,000 INFO worker restarted
`
	got := events(input)
	if len(got) != 2 {
		t.Fatalf("got %d events: %q", len(got), got)
	}
	if !strings.Contains(got[0], "RuntimeError: report failed") {
		t.Fatalf("first event: %q", got[0])
	}
}

func TestPlainLinesStaySeparate(t *testing.T) {
	input := "INFO one\nINFO two\nERROR three\nNullPointerException in handler\n"
	if got := events(input); len(got) != 4 {
		t.Fatalf("got %d events: %q", len(got), got)
	}
}

func TestJSONLinesStaySeparate(t *testing.T) {
	input := "Traceback (most recent call last):\n{\"msg\":\"a\"}\n{\"msg\":\"b\"}\n"
	if got := events(input); len(got) != 3 {
		t.Fatalf("got %d events: %q", len(got), got)
	}
}

func TestTimestampEndsTraceback(t *testing.T) {
	input := "ERROR x\nTraceback (most recent call last):\n  File \"a.py\"\n2026-10-20 next event\n"
	if got := events(input); len(got) != 2 {
		t.Fatalf("got %d events: %q", len(got), got)
	}
}

func TestEventsAreCapped(t *testing.T) {
	input := "ERROR head\n" + strings.Repeat("\tat frame\n", MaxLines+10)
	got := events(input)
	if len(got) != 2 {
		t.Fatalf("got %d events", len(got))
	}
}

func FuzzContinues(f *testing.F) {
	f.Add("ERROR x\n\tat y\n")
	f.Add("Traceback (most recent call last):\n")
	f.Fuzz(func(t *testing.T, input string) {
		var s Splitter
		for _, line := range strings.SplitAfter(input, "\n") {
			s.Continues([]byte(line))
			if s.lines > MaxLines {
				t.Fatalf("event grew past the cap: %d", s.lines)
			}
		}
	})
}
