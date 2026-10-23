package report

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/wellkyc/logcost/internal/textsafe"
)

type style struct{ on bool }

func (s style) wrap(code, text string) string {
	if !s.on {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func (s style) bold(t string) string { return s.wrap("1", t) }
func (s style) dim(t string) string  { return s.wrap("2", t) }
func (s style) warn(t string) string { return s.wrap("33", t) }

func pad(text string, width int, left bool) string {
	n := width - len([]rune(text))
	if n <= 0 {
		return text
	}
	if left {
		return strings.Repeat(" ", n) + text
	}
	return text + strings.Repeat(" ", n)
}

func Table(w io.Writer, s Summary, o Options) error {
	st := style{on: o.Color}
	var b strings.Builder

	fmt.Fprintf(&b, "%s in %s lines, %d templates", st.bold(Bytes(s.Bytes)), Count(s.Lines), s.Templates)
	if s.PeriodHours != nil {
		fmt.Fprintf(&b, ", covering %s", Duration(time.Duration(*s.PeriodHours*float64(time.Hour))))
	}
	b.WriteString("\n")
	switch {
	case s.Monthly != nil:
		fmt.Fprintf(&b, "about %s/month at $%.2f per GB (set your price with -price)\n", st.bold(Money(s.Monthly)), s.PricePerGB)
	case s.Bytes > 0:
		b.WriteString(st.dim("no timestamps found, pass -period (for example -period 24h) to get a monthly cost") + "\n")
	}
	if s.TruncatedLns > 0 {
		b.WriteString(st.warn(fmt.Sprintf("%d lines were longer than 64 KB, only their beginning was used for templates", s.TruncatedLns)) + "\n")
	}

	if len(s.Levels) > 1 {
		b.WriteString("\n")
		levels := make([]string, 0, len(s.Levels))
		for _, l := range s.Levels {
			levels = append(levels, fmt.Sprintf("%s %s", l.Level, Percent(l.Share)))
		}
		b.WriteString(st.dim("by level: ") + strings.Join(levels, ", ") + "\n")
	}

	if len(s.Rows) > 0 {
		b.WriteString("\n")
		header := fmt.Sprintf("%s  %s  %s  %s  %s  %s",
			pad("SHARE", 6, true), pad("SIZE", 8, true), pad("LINES", 7, true),
			pad("$/MONTH", 8, true), pad("LEVEL", 5, false), "TEMPLATE")
		b.WriteString(st.dim(header) + "\n")
		fixed := 6 + 8 + 7 + 8 + 5 + 10
		for _, r := range s.Rows {
			template := textsafe.Truncate(r.Template, o.Width-fixed)
			fmt.Fprintf(&b, "%s  %s  %s  %s  %s  %s\n",
				pad(Percent(r.Share), 6, true), pad(Bytes(r.Bytes), 8, true), pad(Count(r.Lines), 7, true),
				pad(Money(r.Monthly), 8, true), pad(r.Level, 5, false), template)
			if r.Example != "" {
				example := textsafe.Truncate(r.Example, o.Width-fixed-4)
				fmt.Fprintf(&b, "%s%s\n", strings.Repeat(" ", fixed), st.dim("e.g. "+example))
			}
		}
		if s.RestCount > 0 {
			fmt.Fprintf(&b, "%s\n", st.dim(fmt.Sprintf("%s  ... %d more templates", pad(Percent(s.RestShare), 6, true), s.RestCount)))
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func mdCode(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	fence := "`"
	for strings.Contains(s, fence) {
		fence += "`"
	}
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") {
		return fence + " " + s + " " + fence
	}
	return fence + s + fence
}

func Markdown(w io.Writer, s Summary) error {
	var b strings.Builder
	b.WriteString("## Log volume by template\n\n")
	fmt.Fprintf(&b, "%s in %s lines, %d templates", Bytes(s.Bytes), Count(s.Lines), s.Templates)
	if s.PeriodHours != nil {
		fmt.Fprintf(&b, " over %s", Duration(time.Duration(*s.PeriodHours*float64(time.Hour))))
	}
	if s.Monthly != nil {
		fmt.Fprintf(&b, ", about %s/month at $%.2f per GB", Money(s.Monthly), s.PricePerGB)
	}
	b.WriteString(".\n\n")
	b.WriteString("| Share | Size | Lines | $/month | Level | Template |\n")
	b.WriteString("|---:|---:|---:|---:|---|---|\n")
	for _, r := range s.Rows {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n",
			Percent(r.Share), Bytes(r.Bytes), Count(r.Lines), Money(r.Monthly), r.Level, mdCode(r.Template))
	}
	if s.RestCount > 0 {
		fmt.Fprintf(&b, "| %s | | | | | %d more templates |\n", Percent(s.RestShare), s.RestCount)
	}
	_, err := io.WriteString(w, b.String())
	return err
}
