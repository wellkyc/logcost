package multiline

import (
	"bytes"
	"regexp"
)

const MaxLines = 1000

var (
	continuationPrefixes = [][]byte{
		[]byte("Caused by:"),
		[]byte("Suppressed:"),
		[]byte("... "),
		[]byte("During handling of the above exception"),
		[]byte("The above exception was the direct cause"),
	}
	tracebackStart = []byte("Traceback (most recent call last):")
	javaException  = regexp.MustCompile(`^(?:[a-z_][a-z0-9_]*\.)+[A-Z][A-Za-z0-9_$]*(?:Exception|Error|Throwable)(?::|$)`)
)

type Splitter struct {
	started   bool
	traceback bool
	lines     int
}

func (s *Splitter) Reset() {
	*s = Splitter{}
}

func (s *Splitter) Continues(line []byte) bool {
	text := bytes.TrimRight(line, "\r\n")
	if !s.started {
		s.started = true
		s.start(text)
		return false
	}
	if s.lines >= MaxLines || !s.continuation(text) {
		s.start(text)
		return false
	}
	s.lines++
	if bytes.HasPrefix(text, tracebackStart) {
		s.traceback = true
	}
	return true
}

func (s *Splitter) start(text []byte) {
	s.lines = 1
	s.traceback = bytes.HasPrefix(text, tracebackStart)
}

func (s *Splitter) continuation(text []byte) bool {
	if len(text) == 0 {
		return true
	}
	switch text[0] {
	case ' ', '\t':
		return true
	case '{':
		s.traceback = false
		return false
	}
	if bytes.HasPrefix(text, tracebackStart) {
		return true
	}
	for _, prefix := range continuationPrefixes {
		if bytes.HasPrefix(text, prefix) {
			return true
		}
	}
	if s.traceback {
		s.traceback = false
		return text[0] < '0' || text[0] > '9'
	}
	if (bytes.Contains(text, []byte("Exception")) || bytes.Contains(text, []byte("Error")) || bytes.Contains(text, []byte("Throwable"))) && javaException.Match(text) {
		return true
	}
	return false
}
