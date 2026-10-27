package report

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wellkyc/logcost/internal/analyze"
)

func logs(days int, rows map[string]int) string {
	var b strings.Builder
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	step := time.Duration(days) * 24 * time.Hour
	for template, count := range rows {
		for n := range count {
			at := start.Add(step * time.Duration(n) / time.Duration(max(count-1, 1)))
			fmt.Fprintf(&b, "%s %s %d\n", at.Format(time.RFC3339), template, n)
		}
	}
	return b.String()
}

func analyzeSides(t *testing.T, before, after string) *analyze.Result {
	t.Helper()
	r, err := analyze.Run(context.Background(), []analyze.Source{
		{Name: "before", Reader: strings.NewReader(before), Side: 0},
		{Name: "after", Reader: strings.NewReader(after), Side: 1},
	}, analyze.Options{Workers: 3})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func change(t *testing.T, d Diff, prefix string) Change {
	t.Helper()
	for _, c := range d.Changes {
		if strings.HasPrefix(c.Template, prefix) {
			return c
		}
	}
	t.Fatalf("no change for %q in %+v", prefix, d.Changes)
	return Change{}
}

func TestDiffFindsNewGoneAndGrowth(t *testing.T) {
	before := logs(4, map[string]int{
		"INFO GET /healthz 200 took ms":    4000,
		"WARN payment retry for order":     400,
		"INFO mailer sent receipt to user": 400,
		"DEBUG cache hit for key value":    2000,
	})
	after := logs(2, map[string]int{
		"INFO GET /healthz 200 took ms":      2000,
		"WARN payment retry for order":       1000,
		"INFO search query finished results": 600,
		"DEBUG cache hit for key value":      200,
	})
	d := BuildDiff(analyzeSides(t, before, after), Options{Price: 1, Redact: true})
	if !d.Rates {
		t.Fatal("expected per-day comparison")
	}
	if c := change(t, d, "WARN payment"); c.Kind != Up || c.Ratio == nil || *c.Ratio < 3.5 || *c.Ratio > 4.5 {
		t.Fatalf("payment: %+v", c)
	}
	if c := change(t, d, "INFO search"); c.Kind != New || c.BeforeBytes != 0 {
		t.Fatalf("search: %+v", c)
	}
	if c := change(t, d, "INFO mailer"); c.Kind != Gone || c.AfterBytes != 0 {
		t.Fatalf("mailer: %+v", c)
	}
	if c := change(t, d, "DEBUG cache"); c.Kind != Down {
		t.Fatalf("cache: %+v", c)
	}
	for _, c := range d.Changes {
		if strings.HasPrefix(c.Template, "INFO GET /healthz") {
			t.Fatalf("unchanged health check listed: %+v", c)
		}
	}
	if d.Growth == nil || *d.Growth <= 0 {
		t.Fatalf("growth %v", d.Growth)
	}
	if !d.Exceeds(0.1) || d.Exceeds(100) {
		t.Fatal("Exceeds is wrong")
	}
}

func TestDiffWithoutTimestampsComparesTotals(t *testing.T) {
	before := strings.Repeat("INFO job done ok\n", 100)
	after := strings.Repeat("INFO job done ok\n", 300)
	d := BuildDiff(analyzeSides(t, before, after), Options{Price: 1})
	if d.Rates || d.MonthlyChange != nil || d.Growth == nil || *d.Growth != 2 {
		t.Fatalf("got %+v", d)
	}
}

func TestDiffPeriodOverride(t *testing.T) {
	before := strings.Repeat("INFO job done ok\n", 100)
	after := strings.Repeat("INFO job done ok\n", 100)
	d := BuildDiff(analyzeSides(t, before, after), Options{Price: 1, Period: 24 * time.Hour})
	if !d.Rates || *d.Growth != 0 || len(d.Changes) != 0 {
		t.Fatalf("got %+v", d)
	}
}

func TestDiffOutputsAreSafe(t *testing.T) {
	before := logs(1, map[string]int{"INFO ok fine here": 100})
	after := logs(1, map[string]int{"INFO ok fine here": 100, "WARN login password=hunter2 from anna@example.com \x1b[2J": 100})
	d := BuildDiff(analyzeSides(t, before, after), Options{Price: 1, Redact: true, Examples: true})
	var table, md, js bytes.Buffer
	if err := DiffTable(&table, d, Options{Width: 200}); err != nil {
		t.Fatal(err)
	}
	DiffMarkdown(&md, d)
	DiffJSON(&js, d)
	for name, out := range map[string]string{"table": table.String(), "markdown": md.String(), "json": js.String()} {
		if strings.Contains(out, "hunter2") || strings.Contains(out, "anna@example.com") || strings.Contains(out, "\x1b") {
			t.Errorf("%s output is unsafe:\n%s", name, out)
		}
	}
	var decoded Diff
	if err := json.Unmarshal(js.Bytes(), &decoded); err != nil || len(decoded.Changes) != 1 || decoded.Changes[0].Kind != New {
		t.Fatalf("json: %v %+v", err, decoded)
	}
	if !strings.Contains(table.String(), "new") || !strings.Contains(md.String(), "| new |") {
		t.Fatalf("table:\n%s\nmarkdown:\n%s", table.String(), md.String())
	}
}

func TestSignedFormatting(t *testing.T) {
	plus, minus := 12.5, -3.25
	zero := 0.001
	if signedMoney(&plus) != "+$12.50" || signedMoney(&minus) != "-$3.25" || signedMoney(nil) != "-" || signedMoney(&zero) != "$0" {
		t.Fatal("signedMoney is wrong")
	}
	if signedPercent(0.123) != "+12.3%" || signedPercent(-0.5) != "-50.0%" {
		t.Fatal("signedPercent is wrong")
	}
}
