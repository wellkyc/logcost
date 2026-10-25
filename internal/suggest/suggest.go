package suggest

import (
	"regexp"
	"sort"
	"strings"

	"github.com/wellkyc/logcost/internal/drain"
	"github.com/wellkyc/logcost/internal/mask"
	"github.com/wellkyc/logcost/internal/parse"
	"github.com/wellkyc/logcost/internal/redact"
	"github.com/wellkyc/logcost/internal/textsafe"
)

const (
	Drop   = "drop"
	Sample = "sample"

	SampleRate      = 10
	sampleMinShare  = 0.08
	minShare        = 0.002
	maxSuggestions  = 10
	maxPatternBytes = 4096
)

type Suggestion struct {
	Action   string
	Rate     int
	Reason   string
	Template string
	Pattern  string
	JSON     bool
	Bytes    int64
	Saved    int64
	Example  string
}

var (
	healthPath = regexp.MustCompile(`(?i)(^|/)(health|healthz|healthcheck|health-check|ready|readyz|readiness|live|livez|liveness|ping|status)(/|$|\?)`)
	probeAgent = regexp.MustCompile(`(?i)kube-probe|elb-healthchecker|googlehc|consul health`)
	httpMethod = regexp.MustCompile(`^(GET|HEAD|POST|PUT|DELETE|PATCH|OPTIONS)$`)
)

func isHealthCheck(tokens []string) bool {
	method := false
	for _, t := range tokens {
		core := strings.Trim(t, `"'[]()`)
		if httpMethod.MatchString(core) {
			method = true
		}
		if probeAgent.MatchString(t) {
			return true
		}
	}
	if !method {
		return false
	}
	for _, t := range tokens {
		if healthPath.MatchString(strings.Trim(t, `"'[]()`)) {
			return true
		}
	}
	return false
}

func Find(clusters []*drain.Cluster, total int64) []Suggestion {
	if total <= 0 {
		return nil
	}
	var out []Suggestion
	for _, c := range clusters {
		if len(c.Tokens) == 1 && c.Tokens[0] == drain.OtherName {
			continue
		}
		share := float64(c.Bytes) / float64(total)
		if share < minShare {
			continue
		}
		level := c.Level()
		if level >= parse.Warn {
			continue
		}
		template := redact.String(c.Template())
		if !textsafe.Clean(template) {
			continue
		}
		s := Suggestion{
			Template: template,
			JSON:     c.JSON*2 > c.Events,
			Bytes:    c.Bytes,
			Example:  redact.String(c.Example),
		}
		switch {
		case isHealthCheck(c.Tokens):
			s.Action, s.Reason = Drop, "health check"
		case level == parse.Trace || level == parse.Debug:
			s.Action, s.Reason = Drop, strings.ToLower(level.String())+" level"
		case share >= sampleMinShare:
			s.Action, s.Reason, s.Rate = Sample, "high volume", SampleRate
		default:
			continue
		}
		s.Saved = c.Bytes
		if s.Action == Sample {
			s.Saved = c.Bytes - c.Bytes/SampleRate
		}
		s.Pattern = Pattern(template, s.JSON)
		if s.Pattern == "" {
			continue
		}
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Saved > out[j].Saved })
	if len(out) > maxSuggestions {
		out = out[:maxSuggestions]
	}
	return out
}

var placeholders = []string{mask.Wildcard, redact.Placeholder, "<email>", "<card>"}

func tokenPattern(token string) string {
	if token == "<…>" {
		return ""
	}
	var b strings.Builder
	rest := token
	for rest != "" {
		next, which := -1, ""
		for _, p := range placeholders {
			if i := strings.Index(rest, p); i >= 0 && (next < 0 || i < next) {
				next, which = i, p
			}
		}
		if next < 0 {
			b.WriteString(quote(rest))
			break
		}
		b.WriteString(quote(rest[:next]))
		if which == "<card>" {
			b.WriteString(`[0-9][0-9 -]*[0-9]`)
		} else {
			b.WriteString(`\S+`)
		}
		rest = rest[next+len(which):]
	}
	return b.String()
}

func quote(s string) string {
	return strings.ReplaceAll(regexp.QuoteMeta(s), "'", `\x27`)
}

var levelWords = regexp.MustCompile(`(?i)^\W*(trace|debug|info|notice|warn|warning|error|fatal|critical)\W*$`)

func specific(fields []string) bool {
	literals, longest := 0, 0
	for _, f := range fields {
		for _, p := range placeholders {
			f = strings.ReplaceAll(f, p, "")
		}
		f = strings.Trim(f, `"'[](){}<>,.;:=`)
		if f == "" || levelWords.MatchString(f) {
			continue
		}
		literals++
		longest = max(longest, len(f))
	}
	return literals >= 2 || longest >= 8
}

func Pattern(template string, json bool) string {
	trimmed := strings.TrimSpace(template)
	if trimmed == "<empty line>" || trimmed == drain.OtherName {
		return ""
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return ""
	}
	parts := make([]string, 0, len(fields))
	open := false
	for _, f := range fields {
		p := tokenPattern(f)
		if p == "" {
			open = true
			break
		}
		parts = append(parts, p)
	}
	if !specific(fields) {
		return ""
	}
	body := strings.Join(parts, `\s+`)
	var pattern string
	switch {
	case json && open:
		pattern = `(?:^|"(?:msg|message)"\s*:\s*")` + body
	case json:
		pattern = `(?:^|"(?:msg|message)"\s*:\s*")` + body + `(?:$|")`
	case open:
		pattern = body
	default:
		pattern = body + `\s*$`
	}
	if len(pattern) > maxPatternBytes {
		return ""
	}
	if _, err := regexp.Compile(pattern); err != nil {
		return ""
	}
	return pattern
}
