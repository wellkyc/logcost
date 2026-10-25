package analyze

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wellkyc/logcost/internal/parse"
)

func run(t *testing.T, inputs ...string) *Result {
	t.Helper()
	var sources []Source
	for i, in := range inputs {
		sources = append(sources, Source{Name: fmt.Sprint(i), Reader: strings.NewReader(in)})
	}
	r, err := Run(context.Background(), sources, Options{Workers: 4})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func sample(lines int) string {
	var b strings.Builder
	start := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	for i := range lines {
		at := start.Add(time.Duration(i) * time.Minute).Format(time.RFC3339)
		if i%4 == 0 {
			fmt.Fprintf(&b, "%s ERROR payment %d failed\n", at, i)
		} else {
			fmt.Fprintf(&b, "%s INFO GET /healthz 200 in %dms\n", at, i%7)
		}
	}
	return b.String()
}

func TestCountsBytesLinesAndTime(t *testing.T) {
	input := sample(1000)
	r := run(t, input)
	if r.Lines != 1000 || r.Bytes != int64(len(input)) {
		t.Fatalf("lines %d bytes %d, want 1000 and %d", r.Lines, r.Bytes, len(input))
	}
	if len(r.Clusters) != 2 {
		t.Fatalf("got %d clusters", len(r.Clusters))
	}
	if r.Clusters[0].Lines != 750 || r.Clusters[1].Lines != 250 {
		t.Fatalf("cluster sizes %d and %d", r.Clusters[0].Lines, r.Clusters[1].Lines)
	}
	if !r.HasTime || r.Last.Sub(r.First) != 999*time.Minute {
		t.Fatalf("time range %v - %v", r.First, r.Last)
	}
	if r.LevelEvents[parse.Error] != 250 || r.LevelEvents[parse.Info] != 750 {
		t.Fatalf("levels %v", r.LevelEvents)
	}
}

func TestResultsDoNotDependOnWorkerCount(t *testing.T) {
	input := sample(5000)
	var first []string
	for _, workers := range []int{1, 3, 8} {
		r, err := Run(context.Background(), []Source{{Name: "x", Reader: strings.NewReader(input)}}, Options{Workers: workers})
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, c := range r.Clusters {
			got = append(got, fmt.Sprintf("%s=%d", c.Template(), c.Bytes))
		}
		if first == nil {
			first = got
		} else if strings.Join(got, "\n") != strings.Join(first, "\n") {
			t.Fatalf("workers=%d gave different results:\n%v\n%v", workers, got, first)
		}
	}
}

func TestReadsGzip(t *testing.T) {
	input := sample(200)
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write([]byte(input))
	zw.Close()
	r, err := Run(context.Background(), []Source{{Name: "x.gz", Reader: &buf}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Lines != 200 || r.Bytes != int64(len(input)) {
		t.Fatalf("lines %d bytes %d", r.Lines, r.Bytes)
	}
}

func TestBrokenGzipIsAnError(t *testing.T) {
	_, err := Run(context.Background(), []Source{{Name: "bad.gz", Reader: strings.NewReader("\x1f\x8bnot really gzip")}}, Options{})
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestLongLinesAreCut(t *testing.T) {
	long := strings.Repeat("x", 300_000)
	input := "short line\n" + long + "\nlast line without newline"
	r := run(t, input)
	if r.Lines != 3 || r.Bytes != int64(len(input)) || r.CutLines != 1 {
		t.Fatalf("lines %d bytes %d cut %d", r.Lines, r.Bytes, r.CutLines)
	}
	for _, c := range r.Clusters {
		if len(c.Example) > maxExample {
			t.Fatalf("example kept %d bytes", len(c.Example))
		}
	}
}

func TestHostileLinesStaySmall(t *testing.T) {
	var b strings.Builder
	for i := range 300 {
		for j := range 200 {
			fmt.Fprintf(&b, "%s%d ", strings.Repeat("z", 300), i*1000+j)
		}
		b.WriteByte('\n')
	}
	r := run(t, b.String())
	for _, c := range r.Clusters {
		if len(c.Template()) > 96*130 || len(c.Example) > maxExample {
			t.Fatalf("template %d bytes, example %d bytes", len(c.Template()), len(c.Example))
		}
	}
}

func TestStackTracesAreOneEvent(t *testing.T) {
	input := "2026-10-20T10:00:00Z ERROR sync failed\njava.lang.IllegalStateException: locked\n\tat a.B.c(B.java:1)\n\tat a.B.d(B.java:2)\n2026-10-20T10:00:01Z INFO ok\n"
	r := run(t, input)
	if r.Lines != 5 || r.Events != 2 || r.Bytes != int64(len(input)) {
		t.Fatalf("lines %d events %d bytes %d", r.Lines, r.Events, r.Bytes)
	}
	for _, c := range r.Clusters {
		if strings.HasPrefix(c.Template(), "ERROR") && (c.Lines != 4 || c.Events != 1) {
			t.Fatalf("error event: lines %d events %d", c.Lines, c.Events)
		}
	}
	r, err := Run(context.Background(), []Source{{Name: "x", Reader: strings.NewReader(input)}}, Options{NoMultiline: true})
	if err != nil {
		t.Fatal(err)
	}
	if r.Events != 5 {
		t.Fatalf("with multiline off: events %d", r.Events)
	}
}

func TestEmptyInput(t *testing.T) {
	r := run(t, "")
	if r.Lines != 0 || len(r.Clusters) != 0 {
		t.Fatalf("got %+v", r)
	}
}

func TestManyUniqueLinesStayBounded(t *testing.T) {
	var b strings.Builder
	for i := range 30_000 {
		fmt.Fprintf(&b, "%s %s %s\n", word(i), word(i*7+1), word(i*13+2))
	}
	r, err := Run(context.Background(), []Source{{Name: "x", Reader: strings.NewReader(b.String())}}, Options{MaxClusters: 500})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Clusters) > 501 {
		t.Fatalf("got %d clusters", len(r.Clusters))
	}
	if r.Lines != 30_000 {
		t.Fatalf("lost lines: %d", r.Lines)
	}
}

func word(n int) string {
	letters := "abcdefghijklmnopqrstuvwxyz"
	var b strings.Builder
	for range 6 {
		b.WriteByte(letters[n%26])
		n /= 26
	}
	return b.String()
}

func TestCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Run(ctx, []Source{{Name: "x", Reader: strings.NewReader(sample(100000))}}, Options{})
	if err == nil {
		t.Fatal("expected cancellation error")
	}
}

func BenchmarkRun(b *testing.B) {
	input := sample(200_000)
	b.SetBytes(int64(len(input)))
	for b.Loop() {
		if _, err := Run(context.Background(), []Source{{Name: "x", Reader: strings.NewReader(input)}}, Options{}); err != nil {
			b.Fatal(err)
		}
	}
}
