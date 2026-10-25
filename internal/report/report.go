package report

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/wellkyc/logcost/internal/analyze"
	"github.com/wellkyc/logcost/internal/drain"
	"github.com/wellkyc/logcost/internal/parse"
	"github.com/wellkyc/logcost/internal/redact"
	"github.com/wellkyc/logcost/internal/suggest"
	"github.com/wellkyc/logcost/internal/textsafe"
)

const month = 30 * 24 * time.Hour

type Options struct {
	Top      int
	Price    float64
	Period   time.Duration
	Examples bool
	Redact   bool
	Width    int
	Color    bool
	Suggest  bool
}

type Row struct {
	Share    float64  `json:"share"`
	Bytes    int64    `json:"bytes"`
	Events   int64    `json:"events"`
	Lines    int64    `json:"lines"`
	Monthly  *float64 `json:"monthly_cost,omitempty"`
	Level    string   `json:"level"`
	Template string   `json:"template"`
	Example  string   `json:"example,omitempty"`
}

type LevelRow struct {
	Level   string   `json:"level"`
	Share   float64  `json:"share"`
	Bytes   int64    `json:"bytes"`
	Events  int64    `json:"events"`
	Monthly *float64 `json:"monthly_cost,omitempty"`
}

type SuggestionRow struct {
	Action   string   `json:"action"`
	Keep     string   `json:"keep,omitempty"`
	Reason   string   `json:"reason"`
	Share    float64  `json:"saved_share"`
	Bytes    int64    `json:"saved_bytes"`
	Monthly  *float64 `json:"saved_monthly,omitempty"`
	Template string   `json:"template"`
	Pattern  string   `json:"pattern"`
	Example  string   `json:"example,omitempty"`
}

type Summary struct {
	Bytes        int64                `json:"bytes"`
	Lines        int64                `json:"lines"`
	Events       int64                `json:"events"`
	Templates    int                  `json:"templates"`
	From         *time.Time           `json:"from,omitempty"`
	To           *time.Time           `json:"to,omitempty"`
	PeriodHours  *float64             `json:"period_hours,omitempty"`
	PricePerGB   float64              `json:"price_per_gb"`
	Monthly      *float64             `json:"monthly_cost,omitempty"`
	TruncatedLns int64                `json:"truncated_lines,omitempty"`
	Levels       []LevelRow           `json:"levels"`
	Rows         []Row                `json:"templates_top"`
	RestShare    float64              `json:"rest_share,omitempty"`
	RestCount    int                  `json:"rest_templates,omitempty"`
	Suggestions  []SuggestionRow      `json:"suggestions,omitempty"`
	SavedShare   float64              `json:"saved_share,omitempty"`
	SavedMonthly *float64             `json:"saved_monthly,omitempty"`
	Raw          []suggest.Suggestion `json:"-"`
}

func period(r *analyze.Result, o Options) time.Duration {
	if o.Period > 0 {
		return o.Period
	}
	if r.HasTime {
		if d := r.Last.Sub(r.First); d >= time.Minute {
			return d
		}
	}
	return 0
}

func clean(s string, o Options) string {
	if o.Redact {
		s = redact.String(s)
	}
	return textsafe.Sanitize(s)
}

func Build(r *analyze.Result, o Options) Summary {
	p := period(r, o)
	cost := func(bytes int64) *float64 {
		if p <= 0 || o.Price <= 0 {
			return nil
		}
		v := float64(bytes) / 1e9 * o.Price * float64(month) / float64(p)
		return &v
	}
	share := func(bytes int64) float64 {
		if r.Bytes == 0 {
			return 0
		}
		return float64(bytes) / float64(r.Bytes)
	}

	s := Summary{
		Bytes:        r.Bytes,
		Lines:        r.Lines,
		Events:       r.Events,
		Templates:    len(r.Clusters),
		PricePerGB:   o.Price,
		Monthly:      cost(r.Bytes),
		TruncatedLns: r.CutLines,
	}
	if r.HasTime {
		from, to := r.First.UTC(), r.Last.UTC()
		s.From, s.To = &from, &to
	}
	if p > 0 {
		hours := p.Hours()
		s.PeriodHours = &hours
	}
	for l := parse.Level(0); l < parse.LevelCount; l++ {
		if r.LevelEvents[l] == 0 {
			continue
		}
		s.Levels = append(s.Levels, LevelRow{
			Level:   l.String(),
			Share:   share(r.LevelBytes[l]),
			Bytes:   r.LevelBytes[l],
			Events:  r.LevelEvents[l],
			Monthly: cost(r.LevelBytes[l]),
		})
	}
	top := o.Top
	if top <= 0 || top > len(r.Clusters) {
		top = len(r.Clusters)
	}
	for _, c := range r.Clusters[:top] {
		s.Rows = append(s.Rows, row(c, share(c.Bytes), cost(c.Bytes), o))
	}
	for _, c := range r.Clusters[top:] {
		s.RestShare += share(c.Bytes)
		s.RestCount++
	}
	if o.Suggest {
		s.Raw = suggest.Find(r.Clusters, r.Bytes)
		var saved int64
		for _, sg := range s.Raw {
			saved += sg.Saved
			sr := SuggestionRow{
				Action:   sg.Action,
				Reason:   sg.Reason,
				Share:    share(sg.Saved),
				Bytes:    sg.Saved,
				Monthly:  cost(sg.Saved),
				Template: textsafe.Sanitize(sg.Template),
				Pattern:  sg.Pattern,
			}
			if sg.Action == suggest.Sample {
				sr.Keep = fmt.Sprintf("1 in %d", sg.Rate)
			}
			if o.Examples {
				sr.Example = textsafe.Sanitize(sg.Example)
			}
			s.Suggestions = append(s.Suggestions, sr)
		}
		s.SavedShare = share(saved)
		s.SavedMonthly = cost(saved)
	}
	return s
}

func row(c *drain.Cluster, share float64, monthly *float64, o Options) Row {
	rw := Row{
		Share:    share,
		Bytes:    c.Bytes,
		Events:   c.Events,
		Lines:    c.Lines,
		Monthly:  monthly,
		Level:    c.Level().String(),
		Template: clean(c.Template(), o),
	}
	if o.Examples {
		rw.Example = clean(c.Example, o)
	}
	return rw
}

func JSON(w io.Writer, s Summary) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(s)
}

func Bytes(n int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	v := float64(n)
	i := 0
	for v >= 1000 && i < len(units)-1 {
		v /= 1000
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d B", n)
	}
	if v >= 100 {
		return fmt.Sprintf("%.0f %s", v, units[i])
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}

func Count(n int64) string {
	switch {
	case n >= 1e9:
		return fmt.Sprintf("%.1fB", float64(n)/1e9)
	case n >= 1e6:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1e4:
		return fmt.Sprintf("%.1fK", float64(n)/1e3)
	}
	return fmt.Sprintf("%d", n)
}

func Money(v *float64) string {
	if v == nil {
		return "-"
	}
	x := *v
	switch {
	case x < 0.01:
		return "<$0.01"
	case x < 100:
		return fmt.Sprintf("$%.2f", x)
	}
	whole := fmt.Sprintf("%.0f", math.Round(x))
	var b strings.Builder
	for i, c := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return "$" + b.String()
}

func Percent(v float64) string {
	return fmt.Sprintf("%.1f%%", v*100)
}

func Duration(d time.Duration) string {
	days := int(d / (24 * time.Hour))
	hours := int(d % (24 * time.Hour) / time.Hour)
	minutes := int(d % time.Hour / time.Minute)
	switch {
	case days > 0 && hours > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case days > 0:
		return fmt.Sprintf("%dd", days)
	case hours > 0 && minutes > 0:
		return fmt.Sprintf("%dh %dm", hours, minutes)
	case hours > 0:
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dm", minutes)
}
