package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/wellkyc/logcost/internal/analyze"
	"github.com/wellkyc/logcost/internal/report"
)

var version = "dev"

const usage = `logcost shows which log lines take up the most space and money.

Usage:
  logcost [flags] [file ...]
  some-command | logcost [flags]

Files can be plain text or gzip. With no files, logcost reads stdin.

Flags:
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func parsePeriod(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}
	unit := s[len(s)-1]
	if unit == 'd' || unit == 'w' {
		n, err := strconv.ParseFloat(s[:len(s)-1], 64)
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("invalid period %q", s)
		}
		days := n
		if unit == 'w' {
			days *= 7
		}
		return time.Duration(days * float64(24*time.Hour)), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("invalid period %q, use something like 90m, 24h, 7d or 2w", s)
	}
	return d, nil
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func colorAllowed() bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	if runtime.GOOS == "windows" {
		return os.Getenv("WT_SESSION") != "" || os.Getenv("TERM_PROGRAM") != "" || os.Getenv("ConEmuANSI") == "ON"
	}
	return true
}

func terminalWidth() int {
	if n, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && n >= 60 {
		return n
	}
	return 120
}

func run(args []string, stdin *os.File, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("logcost", flag.ContinueOnError)
	flags.SetOutput(stderr)
	top := flags.Int("top", 20, "how many templates to show")
	price := flags.Float64("price", 0.50, "price per GB of logs, in dollars")
	periodFlag := flags.String("period", "", "time the logs cover, e.g. 24h or 7d (default: read from timestamps)")
	format := flags.String("format", "table", "output format: table, json or markdown")
	examples := flags.Bool("examples", false, "show an example line for each template")
	noRedact := flags.Bool("no-redact", false, "do not hide passwords, tokens and emails in the output")
	similarity := flags.Float64("similarity", 0.4, "how similar lines must be to share a template, 0.1 to 0.9")
	showVersion := flags.Bool("version", false, "print the version")
	flags.Usage = func() {
		fmt.Fprint(stderr, usage)
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *showVersion {
		fmt.Fprintln(stdout, "logcost", version)
		return 0
	}
	period, err := parsePeriod(*periodFlag)
	if err != nil {
		fmt.Fprintln(stderr, "logcost:", err)
		return 2
	}
	if *similarity < 0.1 || *similarity > 0.9 {
		fmt.Fprintln(stderr, "logcost: -similarity must be between 0.1 and 0.9")
		return 2
	}
	if *price < 0 {
		fmt.Fprintln(stderr, "logcost: -price can't be negative")
		return 2
	}
	switch *format {
	case "table", "json", "markdown", "md":
	default:
		fmt.Fprintf(stderr, "logcost: unknown format %q, use table, json or markdown\n", *format)
		return 2
	}

	var sources []analyze.Source
	files := flags.Args()
	if len(files) == 0 {
		if isTerminal(stdin) {
			flags.Usage()
			return 2
		}
		files = []string{"-"}
	}
	for _, name := range files {
		if name == "-" {
			sources = append(sources, analyze.Source{Name: "stdin", Reader: stdin})
			continue
		}
		info, err := os.Stat(name)
		if err != nil {
			fmt.Fprintln(stderr, "logcost:", err)
			return 1
		}
		if info.IsDir() {
			fmt.Fprintf(stderr, "logcost: %s is a directory\n", name)
			return 1
		}
		f, err := os.Open(name)
		if err != nil {
			fmt.Fprintln(stderr, "logcost:", err)
			return 1
		}
		defer f.Close()
		sources = append(sources, analyze.Source{Name: name, Reader: f})
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	result, err := analyze.Run(ctx, sources, analyze.Options{Similarity: *similarity})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return 130
		}
		fmt.Fprintln(stderr, "logcost:", err)
		return 1
	}

	out, isFile := stdout.(*os.File)
	tty := isFile && isTerminal(out)
	opts := report.Options{
		Top:      *top,
		Price:    *price,
		Period:   period,
		Examples: *examples,
		Redact:   !*noRedact,
		Color:    tty && colorAllowed(),
	}
	opts.Width = 4000
	if tty {
		opts.Width = terminalWidth()
	}
	summary := report.Build(result, opts)
	switch strings.ToLower(*format) {
	case "json":
		err = report.JSON(stdout, summary)
	case "markdown", "md":
		err = report.Markdown(stdout, summary)
	default:
		err = report.Table(stdout, summary, opts)
	}
	if err != nil {
		fmt.Fprintln(stderr, "logcost:", err)
		return 1
	}
	return 0
}
