package parse

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Level uint8

const (
	Unknown Level = iota
	Trace
	Debug
	Info
	Warn
	Error
	Fatal
	LevelCount
)

var levelNames = [LevelCount]string{"-", "TRACE", "DEBUG", "INFO", "WARN", "ERROR", "FATAL"}

func (l Level) String() string {
	if l >= LevelCount {
		return levelNames[Unknown]
	}
	return levelNames[l]
}

type Record struct {
	Message string
	Level   Level
	Time    time.Time
	HasTime bool
}

var (
	messageKeys = []string{"message", "msg", "log", "text", "event"}
	levelKeys   = []string{"level", "lvl", "severity", "loglevel", "log.level", "levelname", "level_name"}
	timeKeys    = []string{"time", "timestamp", "ts", "@timestamp", "datetime", "date", "t"}
)

const maxJSON = 1 << 20

func Line(line string) Record {
	trimmed := strings.TrimLeft(line, " \t")
	if len(trimmed) > 1 && trimmed[0] == '{' && len(trimmed) <= maxJSON {
		if rec, ok := jsonLine(trimmed); ok {
			return rec
		}
	}
	return textLine(line)
}

func jsonLine(line string) (Record, bool) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &fields); err != nil {
		return Record{}, false
	}
	lower := make(map[string]json.RawMessage, len(fields))
	for k, v := range fields {
		lower[strings.ToLower(k)] = v
	}
	var rec Record
	for _, key := range messageKeys {
		if s, ok := jsonString(lower[key]); ok {
			inner := textLine(s)
			rec.Message, rec.Level = inner.Message, inner.Level
			rec.Time, rec.HasTime = inner.Time, inner.HasTime
			break
		}
	}
	if rec.Message == "" {
		keys := make([]string, 0, len(fields))
		for k := range fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		rec.Message = "{" + strings.Join(keys, ",") + "}"
	}
	for _, key := range levelKeys {
		if raw, ok := lower[key]; ok {
			if s, ok := jsonString(raw); ok {
				rec.Level = levelFromWord(s)
			} else if n, err := strconv.Atoi(string(raw)); err == nil {
				rec.Level = levelFromNumber(n)
			}
			break
		}
	}
	for _, key := range timeKeys {
		raw, ok := lower[key]
		if !ok {
			continue
		}
		if s, ok := jsonString(raw); ok {
			rec.Time, rec.HasTime = Time(s)
		} else if f, err := strconv.ParseFloat(string(raw), 64); err == nil {
			rec.Time, rec.HasTime = epoch(f)
		}
		break
	}
	return rec, true
}

func jsonString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || raw[0] != '"' {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

func textLine(line string) Record {
	rec := Record{Message: line}
	fields := strings.Fields(line)
	if len(fields) > 6 {
		fields = fields[:6]
	}
	for _, f := range fields {
		word := strings.Trim(f, "[]():|<>")
		if len(word) < 3 {
			continue
		}
		if l := levelFromWord(word); l != Unknown {
			rec.Level = l
			break
		}
	}
	var leading int
	rec.Time, rec.HasTime, leading = textTime(fields)
	if leading > 0 && leading < len(fields) {
		rec.Message = skipFields(line, leading)
	}
	return rec
}

func skipFields(s string, n int) string {
	for range n {
		s = strings.TrimLeft(s, " \t")
		i := strings.IndexAny(s, " \t")
		if i < 0 {
			return ""
		}
		s = s[i:]
	}
	return strings.TrimLeft(s, " \t")
}

type candidate struct {
	text    string
	leading int
}

func textTime(fields []string) (time.Time, bool, int) {
	if len(fields) == 0 {
		return time.Time{}, false, 0
	}
	candidates := make([]candidate, 0, 4)
	if len(fields) >= 2 {
		candidates = append(candidates, candidate{fields[0] + " " + fields[1], 2})
	}
	candidates = append(candidates, candidate{strings.Trim(fields[0], "[]"), 1})
	if len(fields) >= 3 {
		candidates = append(candidates, candidate{fields[0] + " " + fields[1] + " " + fields[2], 3})
	}
	for i, f := range fields {
		if strings.HasPrefix(f, "[") && i+1 < len(fields) && strings.HasSuffix(fields[i+1], "]") {
			candidates = append(candidates, candidate{strings.Trim(f+" "+fields[i+1], "[]"), 0})
			break
		}
	}
	first := int(leadingHint.Load())
	for i := range candidates {
		if candidates[i].leading == first {
			candidates[0], candidates[i] = candidates[i], candidates[0]
			break
		}
	}
	for _, c := range candidates {
		if t, ok := Time(c.text); ok {
			if c.leading != first {
				leadingHint.Store(int32(c.leading))
			}
			return t, true, c.leading
		}
	}
	return time.Time{}, false, 0
}

var layouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05,999",
	"2006/01/02 15:04:05.999999999",
	"02/Jan/2006:15:04:05 -0700",
	"Jan _2 15:04:05",
	"Jan _2 15:04:05.000",
}

var (
	minTime     = time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC)
	nowFunc     = time.Now
	now         = sync.OnceValue(func() time.Time { return nowFunc() })
	layoutHint  atomic.Int32
	leadingHint atomic.Int32
)

func plausible(t time.Time) bool {
	return t.After(minTime) && t.Before(now().AddDate(1, 0, 0))
}

func tryLayout(i int, s string) (time.Time, bool) {
	t, err := time.Parse(layouts[i], s)
	if err != nil {
		return time.Time{}, false
	}
	if t.Year() == 0 {
		current := now()
		t = t.AddDate(current.Year(), 0, 0)
		if t.After(current.AddDate(0, 0, 1)) {
			t = t.AddDate(-1, 0, 0)
		}
	}
	return t, plausible(t)
}

func Time(s string) (time.Time, bool) {
	if len(s) < 8 || len(s) > 40 {
		return time.Time{}, false
	}
	c := s[0]
	if !(c >= '0' && c <= '9') && !(c >= 'A' && c <= 'Z') {
		return time.Time{}, false
	}
	hint := int(layoutHint.Load())
	if t, ok := tryLayout(hint, s); ok {
		return t, true
	}
	for i := range layouts {
		if i == hint {
			continue
		}
		if t, ok := tryLayout(i, s); ok {
			layoutHint.Store(int32(i))
			return t, true
		}
	}
	return time.Time{}, false
}

func epoch(f float64) (time.Time, bool) {
	var t time.Time
	switch {
	case f >= 1e9 && f < 1e10:
		t = time.Unix(0, int64(f*1e9))
	case f >= 1e12 && f < 1e13:
		t = time.UnixMilli(int64(f))
	case f >= 1e15 && f < 1e16:
		t = time.UnixMicro(int64(f))
	case f >= 1e18 && f < 1e19:
		t = time.Unix(0, int64(f))
	default:
		return time.Time{}, false
	}
	return t.UTC(), plausible(t)
}

func levelFromWord(s string) Level {
	switch strings.ToUpper(s) {
	case "TRACE", "TRC":
		return Trace
	case "DEBUG", "DBG", "D":
		return Debug
	case "INFO", "INF", "NOTICE", "I":
		return Info
	case "WARN", "WARNING", "WRN", "W":
		return Warn
	case "ERROR", "ERR", "E":
		return Error
	case "FATAL", "CRITICAL", "CRIT", "PANIC", "EMERG", "ALERT", "FTL", "F":
		return Fatal
	}
	return Unknown
}

func levelFromNumber(n int) Level {
	switch {
	case n <= 10:
		return Trace
	case n <= 20:
		return Debug
	case n <= 30:
		return Info
	case n <= 40:
		return Warn
	case n <= 50:
		return Error
	default:
		return Fatal
	}
}
