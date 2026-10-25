package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const logs = `2026-10-20T00:00:00Z INFO GET /healthz 200 in 3ms
2026-10-20T06:00:00Z INFO GET /healthz 200 in 5ms
2026-10-20T12:00:00Z ERROR login failed password=hunter2 for anna@example.com
2026-10-21T00:00:00Z INFO GET /healthz 200 in 4ms
`

func runCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	stdin, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	code := run(args, stdin, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestTable(t *testing.T) {
	path := write(t, "app.log", logs)
	code, out, errOut := runCLI(t, "-examples", path)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	for _, want := range []string{"2 templates", "covering 1d", "GET /healthz <*> in <*>", "password=<redacted>", "<email>"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "hunter2") {
		t.Error("password leaked")
	}
}

func TestJSONFormat(t *testing.T) {
	path := write(t, "app.log", logs)
	code, out, _ := runCLI(t, "-format", "json", "-price", "1", path)
	if code != 0 {
		t.Fatal("non-zero exit")
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out)
	}
	if parsed["lines"].(float64) != 4 {
		t.Fatalf("lines %v", parsed["lines"])
	}
}

const noisy = `2026-10-20T00:00:00Z INFO GET /healthz 200 in 3ms
2026-10-20T06:00:00Z INFO GET /healthz 200 in 5ms
2026-10-20T08:00:00Z DEBUG cache hit key=7 ttl=30s
2026-10-20T12:00:00Z ERROR login failed password=hunter2 for anna@example.com
2026-10-21T00:00:00Z INFO GET /healthz 200 in 4ms
`

func TestSuggest(t *testing.T) {
	path := write(t, "app.log", noisy)
	code, out, errOut := runCLI(t, "-suggest", path)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	for _, want := range []string{"Suggestions:", "health check", "debug level", "-filter vector"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestFilterConfigs(t *testing.T) {
	path := write(t, "app.log", noisy)
	for tool, want := range map[string]string{"vector": "type: filter", "fluentbit": "[FILTER]", "otel": "filter/logcost"} {
		code, out, errOut := runCLI(t, "-filter", tool, path)
		if code != 0 || !strings.Contains(out, want) {
			t.Errorf("%s: exit %d, stderr %q, out:\n%s", tool, code, errOut, out)
		}
		if strings.Contains(out, "hunter2") || strings.Contains(out, "ERROR") {
			t.Errorf("%s config touches errors or leaks secrets:\n%s", tool, out)
		}
	}
}

func TestBadArguments(t *testing.T) {
	cases := [][]string{
		{"-format", "xml", "x"},
		{"-period", "soon", "x"},
		{"-similarity", "2", "x"},
		{"-price", "-1", "x"},
		{"-filter", "splunk", "x"},
	}
	for _, args := range cases {
		if code, _, _ := runCLI(t, args...); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
}

func TestMissingFile(t *testing.T) {
	code, _, errOut := runCLI(t, filepath.Join(t.TempDir(), "nope.log"))
	if code != 1 || errOut == "" {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
}

func TestDirectoryIsRejected(t *testing.T) {
	code, _, errOut := runCLI(t, t.TempDir())
	if code != 1 || !strings.Contains(errOut, "directory") {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
}

func TestPeriod(t *testing.T) {
	cases := map[string]float64{"90m": 1.5, "24h": 24, "7d": 168, "2w": 336, "0.5d": 12}
	for in, hours := range cases {
		d, err := parsePeriod(in)
		if err != nil || d.Hours() != hours {
			t.Errorf("parsePeriod(%q) = %v, %v", in, d, err)
		}
	}
	for _, bad := range []string{"-5d", "0h", "xd", "d"} {
		if _, err := parsePeriod(bad); err == nil {
			t.Errorf("parsePeriod(%q) should fail", bad)
		}
	}
}

func TestVersion(t *testing.T) {
	code, out, _ := runCLI(t, "-version")
	if code != 0 || !strings.HasPrefix(out, "logcost ") {
		t.Fatalf("exit %d out %q", code, out)
	}
}
