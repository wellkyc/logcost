package suggest

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/wellkyc/logcost/internal/drain"
	"github.com/wellkyc/logcost/internal/mask"
	"github.com/wellkyc/logcost/internal/parse"
)

type line struct {
	text  string
	level parse.Level
	json  bool
	times int
}

func clusters(lines ...line) ([]*drain.Cluster, int64) {
	m := drain.New(0.4, 1000)
	var total int64
	for _, l := range lines {
		message := l.text
		if l.json {
			message = parse.Line(l.text).Message
		}
		tokens := mask.Tokens(message)
		for range l.times {
			c := m.Add(tokens, strings.Join(tokens, " "))
			c.Events++
			c.Bytes += int64(len(l.text) + 1)
			c.Levels[l.level]++
			if l.json {
				c.JSON++
			}
			if c.Example == "" {
				c.Example = l.text
			}
			total += int64(len(l.text) + 1)
		}
	}
	return m.Clusters(), total
}

func find(t *testing.T, list []Suggestion, prefix string) Suggestion {
	t.Helper()
	for _, s := range list {
		if strings.HasPrefix(s.Template, prefix) {
			return s
		}
	}
	t.Fatalf("no suggestion for %q in %+v", prefix, list)
	return Suggestion{}
}

func TestSuggestsDropsAndSamples(t *testing.T) {
	cs, total := clusters(
		line{"INFO GET /healthz 200 in 3ms", parse.Info, false, 300},
		line{"DEBUG cache hit key=1 ttl=30s", parse.Debug, false, 200},
		line{"INFO order 42 shipped to warehouse 7", parse.Info, false, 400},
		line{"ERROR payment 9 failed: card declined", parse.Error, false, 500},
		line{"WARN disk 91% full on node 3", parse.Warn, false, 500},
	)
	list := Find(cs, total)
	if s := find(t, list, "INFO GET /healthz"); s.Action != Drop || s.Reason != "health check" {
		t.Fatalf("health check: %+v", s)
	}
	if s := find(t, list, "DEBUG cache hit"); s.Action != Drop || s.Reason != "debug level" {
		t.Fatalf("debug: %+v", s)
	}
	if s := find(t, list, "INFO order"); s.Action != Sample || s.Saved != s.Bytes-s.Bytes/SampleRate {
		t.Fatalf("sample: %+v", s)
	}
	for _, s := range list {
		if strings.HasPrefix(s.Template, "ERROR") || strings.HasPrefix(s.Template, "WARN") {
			t.Fatalf("suggested to cut %q", s.Template)
		}
	}
}

func TestSmallTemplatesAreLeftAlone(t *testing.T) {
	cs, total := clusters(
		line{"DEBUG tiny thing happened here", parse.Debug, false, 1},
		line{"ERROR big error number 5", parse.Error, false, 10_000},
	)
	if list := Find(cs, total); len(list) != 0 {
		t.Fatalf("got %+v", list)
	}
}

func TestPatternsMatchTheirLines(t *testing.T) {
	cases := []struct {
		template string
		json     bool
		match    []string
		miss     []string
	}{
		{
			template: "INFO [http] GET /healthz <*> in <*>",
			match:    []string{"2026-10-20T10:00:00Z INFO  [http] GET /healthz 200 in 3ms"},
			miss:     []string{"2026-10-20T10:00:00Z INFO  [http] GET /healthz 200 in 3ms extra", "ERROR [http] GET /healthz 200 in 3ms"},
		},
		{
			template: "DEBUG query status = 'open' took <*>,",
			match:    []string{"DEBUG query status = 'open' took 5ms,"},
		},
		{
			template: "request finished",
			json:     true,
			match:    []string{`{"time":"x","level":"info","msg":"request finished","ms":5}`, "request finished"},
			miss:     []string{`{"msg":"request finished early"}`},
		},
		{
			template: "login user=<*> password=<redacted> from <email>",
			match:    []string{"login user=7 password=hunter2 from anna@example.com"},
		},
	}
	for _, c := range cases {
		p := Pattern(c.template, c.json)
		re, err := regexp.Compile(p)
		if err != nil || p == "" {
			t.Fatalf("%q: pattern %q, %v", c.template, p, err)
		}
		if strings.Contains(p, "'") {
			t.Errorf("%q: pattern has a raw quote: %s", c.template, p)
		}
		for _, m := range c.match {
			if !re.MatchString(m) {
				t.Errorf("%q: %s does not match %q", c.template, p, m)
			}
		}
		for _, m := range c.miss {
			if re.MatchString(m) {
				t.Errorf("%q: %s should not match %q", c.template, p, m)
			}
		}
	}
}

func TestVagueTemplatesGetNoPattern(t *testing.T) {
	for _, template := range []string{"<*> <*> <*>", "INFO <*> <*>", "DEBUG <*>", "<empty line>", ""} {
		if p := Pattern(template, false); p != "" {
			t.Errorf("%q got pattern %q", template, p)
		}
	}
}

func TestSecretsNeverReachConfigs(t *testing.T) {
	cs, total := clusters(
		line{"DEBUG auth ok password=hunter2 token=ghp_abcdefghijklmnopqrstuvwxyz0123456789", parse.Debug, false, 100},
		line{"ERROR x", parse.Error, false, 10},
	)
	list := Find(cs, total)
	if len(list) != 1 {
		t.Fatalf("got %+v", list)
	}
	var out bytes.Buffer
	Vector(&out, list, "test")
	FluentBit(&out, list, "test")
	OTel(&out, list, "test")
	if strings.Contains(out.String(), "hunter2") || strings.Contains(out.String(), "ghp_") {
		t.Fatalf("secret leaked into config:\n%s", out.String())
	}
}

func TestConfigs(t *testing.T) {
	list := []Suggestion{
		{Action: Drop, Pattern: `DEBUG\s+cache`},
		{Action: Sample, Rate: SampleRate, Pattern: `request\s+done\s*$`},
	}
	var vector, fluent, otel bytes.Buffer
	Vector(&vector, list, "v1")
	FluentBit(&fluent, list, "v1")
	OTel(&otel, list, "v1")
	for _, want := range []string{"type: filter", "type: sample", "rate: 10", `match(msg, r'DEBUG\s+cache')`, "inputs: [logcost_drop]"} {
		if !strings.Contains(vector.String(), want) {
			t.Errorf("vector config misses %q:\n%s", want, vector.String())
		}
	}
	if !strings.Contains(fluent.String(), `Exclude log DEBUG\s+cache`) || strings.Contains(fluent.String(), "request") {
		t.Errorf("fluent bit config:\n%s", fluent.String())
	}
	if !strings.Contains(otel.String(), `IsMatch(body, "DEBUG\\s+cache")`) {
		t.Errorf("otel config:\n%s", otel.String())
	}
}

func TestEmptyConfigs(t *testing.T) {
	var out bytes.Buffer
	Vector(&out, nil, "v1")
	OTel(&out, nil, "v1")
	FluentBit(&out, nil, "v1")
	if strings.Contains(out.String(), "transforms:") || strings.Contains(out.String(), "processors:") || strings.Contains(out.String(), "[FILTER]") {
		t.Fatalf("empty list produced rules:\n%s", out.String())
	}
}

func FuzzPattern(f *testing.F) {
	f.Add("INFO GET /x <*> in <*>", false)
	f.Add(`a "b" 'c' \d <redacted> <card>`, true)
	f.Fuzz(func(t *testing.T, template string, json bool) {
		p := Pattern(template, json)
		if p == "" {
			return
		}
		if _, err := regexp.Compile(p); err != nil {
			t.Fatalf("invalid pattern %q: %v", p, err)
		}
		if strings.ContainsAny(p, "'\n\r") {
			t.Fatalf("pattern can break a config: %q", p)
		}
	})
}
