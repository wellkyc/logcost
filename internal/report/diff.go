package report

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/wellkyc/logcost/internal/analyze"
	"github.com/wellkyc/logcost/internal/textsafe"
)

const (
	New   = "new"
	Gone  = "gone"
	Up    = "up"
	Down  = "down"
	Same  = "same"
	day   = 24 * time.Hour
	minor = 0.005
	shift = 0.25
)

type SideSummary struct {
	Bytes       int64    `json:"bytes"`
	Events      int64    `json:"events"`
	PeriodHours *float64 `json:"period_hours,omitempty"`
	PerDay      *float64 `json:"bytes_per_day,omitempty"`
}

type Change struct {
	Kind          string   `json:"change"`
	Ratio         *float64 `json:"ratio,omitempty"`
	BeforeBytes   int64    `json:"before_bytes"`
	AfterBytes    int64    `json:"after_bytes"`
	BeforePerDay  *float64 `json:"before_bytes_per_day,omitempty"`
	AfterPerDay   *float64 `json:"after_bytes_per_day,omitempty"`
	MonthlyChange *float64 `json:"monthly_change,omitempty"`
	Level         string   `json:"level"`
	Template      string   `json:"template"`
	Example       string   `json:"example,omitempty"`
	weight        float64
}

type Diff struct {
	Before        SideSummary `json:"before"`
	After         SideSummary `json:"after"`
	Rates         bool        `json:"compared_per_day"`
	Growth        *float64    `json:"growth,omitempty"`
	PricePerGB    float64     `json:"price_per_gb"`
	MonthlyChange *float64    `json:"monthly_change,omitempty"`
	Changes       []Change    `json:"changes"`
	Hidden        int         `json:"hidden_templates,omitempty"`
}

func sidePeriod(s analyze.Span, override time.Duration) time.Duration {
	if s.HasTime {
		if d := s.Last.Sub(s.First); d >= time.Minute {
			return d
		}
	}
	return override
}

func ratio(before, after float64) *float64 {
	if before <= 0 {
		return nil
	}
	v := after/before - 1
	return &v
}

func BuildDiff(r *analyze.Result, o Options) Diff {
	periods := [2]time.Duration{sidePeriod(r.Sides[0], o.Period), sidePeriod(r.Sides[1], o.Period)}
	rates := periods[0] > 0 && periods[1] > 0
	scale := func(side int, bytes int64) float64 {
		if rates {
			return float64(bytes) * float64(day) / float64(periods[side])
		}
		return float64(bytes)
	}
	perDay := func(side int, bytes int64) *float64 {
		if !rates {
			return nil
		}
		v := scale(side, bytes)
		return &v
	}
	monthly := func(delta float64) *float64 {
		if !rates || o.Price <= 0 {
			return nil
		}
		v := delta * 30 / 1e9 * o.Price
		return &v
	}

	d := Diff{Rates: rates, PricePerGB: o.Price}
	for side, summary := range []*SideSummary{&d.Before, &d.After} {
		summary.Bytes = r.Sides[side].Bytes
		summary.Events = r.Sides[side].Events
		summary.PerDay = perDay(side, r.Sides[side].Bytes)
		if periods[side] > 0 {
			hours := periods[side].Hours()
			summary.PeriodHours = &hours
		}
	}
	totalBefore, totalAfter := scale(0, r.Sides[0].Bytes), scale(1, r.Sides[1].Bytes)
	d.Growth = ratio(totalBefore, totalAfter)
	d.MonthlyChange = monthly(totalAfter - totalBefore)

	for _, c := range r.Clusters {
		before, after := scale(0, c.Sides[0].Bytes), scale(1, c.Sides[1].Bytes)
		shareBefore, shareAfter := 0.0, 0.0
		if totalBefore > 0 {
			shareBefore = before / totalBefore
		}
		if totalAfter > 0 {
			shareAfter = after / totalAfter
		}
		ch := Change{
			Ratio:         ratio(before, after),
			BeforeBytes:   c.Sides[0].Bytes,
			AfterBytes:    c.Sides[1].Bytes,
			BeforePerDay:  perDay(0, c.Sides[0].Bytes),
			AfterPerDay:   perDay(1, c.Sides[1].Bytes),
			MonthlyChange: monthly(after - before),
			Level:         c.Level().String(),
			weight:        math.Abs(after - before),
		}
		visible := false
		switch {
		case c.Sides[0].Bytes == 0 && c.Sides[1].Bytes == 0:
			continue
		case c.Sides[0].Bytes == 0:
			ch.Kind, visible = New, shareAfter >= minor/5
		case c.Sides[1].Bytes == 0:
			ch.Kind, visible = Gone, shareBefore >= minor/5
		case *ch.Ratio >= shift:
			ch.Kind, visible = Up, max(shareBefore, shareAfter) >= minor
		case *ch.Ratio <= -shift:
			ch.Kind, visible = Down, max(shareBefore, shareAfter) >= minor
		default:
			ch.Kind = Same
		}
		if !visible {
			d.Hidden++
			continue
		}
		ch.Template = clean(c.Template(), o)
		if o.Examples {
			ch.Example = clean(c.Example, o)
		}
		d.Changes = append(d.Changes, ch)
	}
	sort.SliceStable(d.Changes, func(i, j int) bool { return d.Changes[i].weight > d.Changes[j].weight })
	if o.Top > 0 && len(d.Changes) > o.Top {
		d.Hidden += len(d.Changes) - o.Top
		d.Changes = d.Changes[:o.Top]
	}
	return d
}

func signedPercent(v float64) string {
	if v >= 0 {
		return "+" + Percent(v)
	}
	return Percent(v)
}

func signedMoney(v *float64) string {
	if v == nil {
		return "-"
	}
	if *v < 0 {
		neg := -*v
		return "-" + Money(&neg)
	}
	return "+" + Money(v)
}

func (c Change) label() string {
	switch c.Kind {
	case New, Gone:
		return c.Kind
	}
	if c.Ratio == nil {
		return c.Kind
	}
	return fmt.Sprintf("%+.0f%%", *c.Ratio*100)
}

func amount(rates bool, perDay *float64, total int64) string {
	if total == 0 {
		return "-"
	}
	if rates && perDay != nil {
		return Bytes(int64(*perDay))
	}
	return Bytes(total)
}

func sideLine(name string, s SideSummary) string {
	line := fmt.Sprintf("%s: %s", name, Bytes(s.Bytes))
	if s.PeriodHours != nil {
		line += " over " + Duration(time.Duration(*s.PeriodHours*float64(time.Hour)))
	}
	if s.PerDay != nil {
		line += fmt.Sprintf(" (%s/day)", Bytes(int64(*s.PerDay)))
	}
	return line
}

func (d Diff) Exceeds(limit float64) bool {
	return d.Growth != nil && *d.Growth > limit
}

func DiffTable(w io.Writer, d Diff, o Options) error {
	st := style{on: o.Color}
	var b strings.Builder
	fmt.Fprintf(&b, "%s    %s", sideLine("before", d.Before), sideLine("after", d.After))
	if d.Growth != nil {
		fmt.Fprintf(&b, "    %s", st.bold(signedPercent(*d.Growth)))
	}
	b.WriteString("\n")
	switch {
	case d.MonthlyChange != nil:
		fmt.Fprintf(&b, "about %s/month at $%.2f per GB\n", st.bold(signedMoney(d.MonthlyChange)), d.PricePerGB)
	case !d.Rates:
		b.WriteString(st.dim("no timestamps in one of the inputs, comparing totals; pass -period to compare per day") + "\n")
	}

	unit := "BEFORE"
	unitAfter := "AFTER"
	if d.Rates {
		unit, unitAfter = "BEFORE/DAY", "AFTER/DAY"
	}
	b.WriteString("\n")
	if len(d.Changes) == 0 {
		b.WriteString("No template changed by more than 25%.\n")
	} else {
		header := fmt.Sprintf("%s  %s  %s  %s  %s  %s",
			pad("CHANGE", 7, false), pad(unit, 10, true), pad(unitAfter, 10, true),
			pad("$/MONTH", 8, true), pad("LEVEL", 5, false), "TEMPLATE")
		b.WriteString(st.dim(header) + "\n")
		fixed := 7 + 10 + 10 + 8 + 5 + 10
		for _, c := range d.Changes {
			fmt.Fprintf(&b, "%s  %s  %s  %s  %s  %s\n",
				pad(c.label(), 7, false),
				pad(amount(d.Rates, c.BeforePerDay, c.BeforeBytes), 10, true),
				pad(amount(d.Rates, c.AfterPerDay, c.AfterBytes), 10, true),
				pad(signedMoney(c.MonthlyChange), 8, true),
				pad(c.Level, 5, false),
				textsafe.Truncate(c.Template, o.Width-fixed))
			if c.Example != "" {
				fmt.Fprintf(&b, "%s%s\n", strings.Repeat(" ", fixed), st.dim("e.g. "+textsafe.Truncate(c.Example, o.Width-fixed-4)))
			}
		}
	}
	if d.Hidden > 0 {
		b.WriteString(st.dim(fmt.Sprintf("%d more templates changed less or are under 0.5%% of the volume", d.Hidden)) + "\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func DiffMarkdown(w io.Writer, d Diff) error {
	var b strings.Builder
	b.WriteString("## Log volume change\n\n")
	fmt.Fprintf(&b, "%s, %s", sideLine("Before", d.Before), sideLine("after", d.After))
	if d.Growth != nil {
		fmt.Fprintf(&b, ", **%s**", signedPercent(*d.Growth))
	}
	if d.MonthlyChange != nil {
		fmt.Fprintf(&b, ", about %s/month", signedMoney(d.MonthlyChange))
	}
	b.WriteString(".\n\n")
	if len(d.Changes) == 0 {
		b.WriteString("No template changed by more than 25%.\n")
	} else {
		b.WriteString("| Change | Before | After | $/month | Level | Template |\n")
		b.WriteString("|---|---:|---:|---:|---|---|\n")
		for _, c := range d.Changes {
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n", c.label(),
				amount(d.Rates, c.BeforePerDay, c.BeforeBytes), amount(d.Rates, c.AfterPerDay, c.AfterBytes),
				signedMoney(c.MonthlyChange), c.Level, mdCode(c.Template))
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func DiffJSON(w io.Writer, d Diff) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(d)
}
