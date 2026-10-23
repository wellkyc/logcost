package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/wellkyc/logcost/internal/analyze"
	"github.com/wellkyc/logcost/internal/drain"
	"github.com/wellkyc/logcost/internal/parse"
)

func result() *analyze.Result {
	health := &drain.Cluster{Tokens: []string{"GET", "/healthz", "<*>"}, Lines: 900, Bytes: 9_000_000_000, Example: "GET /healthz 200"}
	health.Levels[parse.Info] = 900
	login := &drain.Cluster{Tokens: []string{"login", "password=hunter2", "\x1b[2J|x`y"}, Lines: 100, Bytes: 1_000_000_000, Example: "login password=hunter2 \x1b[2J from anna@example.com"}
	login.Levels[parse.Error] = 100
	r := &analyze.Result{
		Clusters: []*drain.Cluster{health, login},
		Lines:    1000,
		Bytes:    10_000_000_000,
		First:    time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC),
		Last:     time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC),
		HasTime:  true,
	}
	r.LevelBytes[parse.Info], r.LevelLines[parse.Info] = 9_000_000_000, 900
	r.LevelBytes[parse.Error], r.LevelLines[parse.Error] = 1_000_000_000, 100
	return r
}

func TestMonthlyCostFromTimestamps(t *testing.T) {
	s := Build(result(), Options{Price: 0.5, Redact: true})
	if s.Monthly == nil || *s.Monthly != 150 {
		t.Fatalf("monthly %v", s.Monthly)
	}
	if *s.Rows[0].Monthly != 135 {
		t.Fatalf("row cost %v", *s.Rows[0].Monthly)
	}
}

func TestPeriodOverride(t *testing.T) {
	s := Build(result(), Options{Price: 1, Period: 30 * 24 * time.Hour})
	if *s.Monthly != 10 {
		t.Fatalf("monthly %v", *s.Monthly)
	}
}

func TestNoTimeMeansNoCost(t *testing.T) {
	r := result()
	r.HasTime = false
	if s := Build(r, Options{Price: 1}); s.Monthly != nil {
		t.Fatal("cost without a period")
	}
}

func TestOutputIsRedactedAndSafe(t *testing.T) {
	s := Build(result(), Options{Price: 0.5, Redact: true, Examples: true, Top: 10})
	var table, md, js bytes.Buffer
	if err := Table(&table, s, Options{Width: 200}); err != nil {
		t.Fatal(err)
	}
	if err := Markdown(&md, s); err != nil {
		t.Fatal(err)
	}
	if err := JSON(&js, s); err != nil {
		t.Fatal(err)
	}
	for name, out := range map[string]string{"table": table.String(), "markdown": md.String(), "json": js.String()} {
		if strings.Contains(out, "hunter2") || strings.Contains(out, "anna@example.com") {
			t.Errorf("%s output leaks a secret:\n%s", name, out)
		}
		if strings.Contains(out, "\x1b") {
			t.Errorf("%s output contains a raw escape character", name)
		}
	}
	if !strings.Contains(md.String(), `\|`) {
		t.Errorf("markdown pipe not escaped:\n%s", md.String())
	}
	var decoded Summary
	if err := json.Unmarshal(js.Bytes(), &decoded); err != nil || len(decoded.Rows) != 2 {
		t.Fatalf("json does not round-trip: %v", err)
	}
}

func TestNoRedactShowsRawButStillSafe(t *testing.T) {
	s := Build(result(), Options{Redact: false, Examples: true})
	var out bytes.Buffer
	Table(&out, s, Options{})
	if !strings.Contains(out.String(), "hunter2") || strings.Contains(out.String(), "\x1b") {
		t.Fatalf("unexpected output:\n%s", out.String())
	}
}

func TestTopAndRest(t *testing.T) {
	s := Build(result(), Options{Top: 1})
	if len(s.Rows) != 1 || s.RestCount != 1 || s.RestShare != 0.1 {
		t.Fatalf("rows %d rest %d %v", len(s.Rows), s.RestCount, s.RestShare)
	}
}

func TestFormatting(t *testing.T) {
	money := func(v float64) string { return Money(&v) }
	cases := map[string]string{
		Bytes(999):                 "999 B",
		Bytes(1_500_000):           "1.5 MB",
		Bytes(250_000_000_000):     "250 GB",
		Count(42):                  "42",
		Count(12_400_000):          "12.4M",
		money(0.004):               "<$0.01",
		money(3.5):                 "$3.50",
		money(1234567):             "$1,234,567",
		Money(nil):                 "-",
		Duration(26 * time.Hour):   "1d 2h",
		Duration(90 * time.Minute): "1h 30m",
		Percent(0.3825):            "38.2%",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}
