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

func headline(s Summary) string {
	if s.Events > 0 && s.Events != s.Lines {
		return fmt.Sprintf("%s lines (%s events)", Count(s.Lines), Count(s.Events))
	}
	return Count(s.Lines) + " lines"
}

func Table(w io.Writer, s Summary, o Options) error {
	st := style{on: o.Color}
	var b strings.Builder

	fmt.Fprintf(&b, "%s in %s, %d templates", st.bold(Bytes(s.Bytes)), headline(s), s.Templates)
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
			pad("SHARE", 6, true), pad("SIZE", 8, true), pad("EVENTS", 7, true),
			pad("$/MONTH", 8, true), pad("LEVEL", 5, false), "TEMPLATE")
		b.WriteString(st.dim(header) + "\n")
		fixed := 6 + 8 + 7 + 8 + 5 + 10
		for _, r := range s.Rows {
			template := textsafe.Truncate(r.Template, o.Width-fixed)
			fmt.Fprintf(&b, "%s  %s  %s  %s  %s  %s\n",
				pad(Percent(r.Share), 6, true), pad(Bytes(r.Bytes), 8, true), pad(Count(r.Events), 7, true),
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

	if o.Suggest {
		b.WriteString("\n")
		writeSuggestions(&b, s, o, st)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func action(r SuggestionRow) string {
	if r.Keep != "" {
		return "sample, keep " + r.Keep
	}
	return r.Action
}

func writeSuggestions(b *strings.Builder, s Summary, o Options, st style) {
	if len(s.Suggestions) == 0 {
		b.WriteString("Nothing obvious to cut: no debug, trace or health check templates, and no single info template above 8%.\n")
		return
	}
	saving := Percent(s.SavedShare) + " of the volume"
	if s.SavedMonthly != nil {
		saving = fmt.Sprintf("about %s/month, %s", Money(s.SavedMonthly), saving)
	}
	fmt.Fprintf(b, "%s\n", st.bold("Suggestions: "+saving))
	header := fmt.Sprintf("%s  %s  %s  %s  %s",
		pad("ACTION", 20, false), pad("SAVES", 6, true), pad("$/MONTH", 8, true), pad("WHY", 12, false), "TEMPLATE")
	b.WriteString(st.dim(header) + "\n")
	fixed := 20 + 6 + 8 + 12 + 8
	for _, r := range s.Suggestions {
		fmt.Fprintf(b, "%s  %s  %s  %s  %s\n",
			pad(action(r), 20, false), pad(Percent(r.Share), 6, true), pad(Money(r.Monthly), 8, true),
			pad(r.Reason, 12, false), textsafe.Truncate(r.Template, o.Width-fixed))
	}
	b.WriteString(st.dim("Warnings and errors are never suggested. Check each rule before using it.") + "\n")
	b.WriteString(st.dim("Get a ready config with -filter vector, -filter fluentbit or -filter otel.") + "\n")
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
	fmt.Fprintf(&b, "%s in %s, %d templates", Bytes(s.Bytes), headline(s), s.Templates)
	if s.PeriodHours != nil {
		fmt.Fprintf(&b, " over %s", Duration(time.Duration(*s.PeriodHours*float64(time.Hour))))
	}
	if s.Monthly != nil {
		fmt.Fprintf(&b, ", about %s/month at $%.2f per GB", Money(s.Monthly), s.PricePerGB)
	}
	b.WriteString(".\n\n")
	b.WriteString("| Share | Size | Events | $/month | Level | Template |\n")
	b.WriteString("|---:|---:|---:|---:|---|---|\n")
	for _, r := range s.Rows {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n",
			Percent(r.Share), Bytes(r.Bytes), Count(r.Events), Money(r.Monthly), r.Level, mdCode(r.Template))
	}
	if s.RestCount > 0 {
		fmt.Fprintf(&b, "| %s | | | | | %d more templates |\n", Percent(s.RestShare), s.RestCount)
	}
	if len(s.Suggestions) > 0 {
		b.WriteString("\n### What to cut\n\n")
		if s.SavedMonthly != nil {
			fmt.Fprintf(&b, "About %s/month, %s of the volume.\n\n", Money(s.SavedMonthly), Percent(s.SavedShare))
		} else {
			fmt.Fprintf(&b, "%s of the volume.\n\n", Percent(s.SavedShare))
		}
		b.WriteString("| Action | Saves | $/month | Why | Template |\n")
		b.WriteString("|---|---:|---:|---|---|\n")
		for _, r := range s.Suggestions {
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", action(r), Percent(r.Share), Money(r.Monthly), r.Reason, mdCode(r.Template))
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}
