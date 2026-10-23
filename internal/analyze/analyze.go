package analyze

import (
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wellkyc/logcost/internal/drain"
	"github.com/wellkyc/logcost/internal/mask"
	"github.com/wellkyc/logcost/internal/parse"
)

const (
	DefaultMaxLine     = 64 * 1024
	DefaultMaxClusters = 10_000
	maxExample         = 2048
	batchLines         = 2048
	batchBytes         = 1 << 20
	readBuffer         = 1 << 20
)

type Options struct {
	Similarity  float64
	Workers     int
	MaxLine     int
	MaxClusters int
}

type Source struct {
	Name   string
	Reader io.Reader
}

type Result struct {
	Clusters   []*drain.Cluster
	Lines      int64
	Bytes      int64
	LevelBytes [parse.LevelCount]int64
	LevelLines [parse.LevelCount]int64
	First      time.Time
	Last       time.Time
	HasTime    bool
	CutLines   int64
}

type line struct {
	text []byte
	size int64
}

type batch struct {
	index int
	lines []line
}

type item struct {
	tokens  []string
	key     string
	size    int64
	level   parse.Level
	time    time.Time
	hasTime bool
	raw     []byte
}

type processed struct {
	index int
	items []item
}

func (o *Options) defaults() {
	if o.Similarity <= 0 || o.Similarity >= 1 {
		o.Similarity = 0.4
	}
	if o.Workers <= 0 {
		o.Workers = runtime.NumCPU()
	}
	if o.MaxLine <= 0 {
		o.MaxLine = DefaultMaxLine
	}
	if o.MaxClusters <= 0 {
		o.MaxClusters = DefaultMaxClusters
	}
}

func Run(ctx context.Context, sources []Source, opts Options) (*Result, error) {
	opts.defaults()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	batches := make(chan batch, opts.Workers*2)
	results := make(chan processed, opts.Workers*2)
	slots := make(chan struct{}, opts.Workers*4)
	result := &Result{}
	var readErr error

	go func() {
		defer close(batches)
		index := 0
		for _, src := range sources {
			cut, err := read(ctx, src, opts.MaxLine, &index, slots, batches)
			result.CutLines += cut
			if err != nil {
				readErr = err
				cancel()
				return
			}
		}
	}()

	var wg sync.WaitGroup
	for range opts.Workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for b := range batches {
				select {
				case results <- process(b):
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	miner := drain.New(opts.Similarity, opts.MaxClusters)
	pending := make(map[int]processed)
	next := 0
	for p := range results {
		pending[p.index] = p
		for {
			ready, ok := pending[next]
			if !ok {
				break
			}
			delete(pending, next)
			collect(miner, result, ready.items)
			<-slots
			next++
		}
	}
	if readErr != nil {
		return nil, readErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result.Clusters = miner.Clusters()
	sort.SliceStable(result.Clusters, func(i, j int) bool {
		return result.Clusters[i].Bytes > result.Clusters[j].Bytes
	})
	return result, nil
}

func collect(miner *drain.Miner, r *Result, items []item) {
	for i := range items {
		it := &items[i]
		c := miner.Add(it.tokens, it.key)
		c.Lines++
		c.Bytes += it.size
		c.Levels[it.level]++
		if c.Example == "" {
			raw := it.raw
			if len(raw) > maxExample {
				raw = raw[:maxExample]
			}
			c.Example = strings.ToValidUTF8(strings.TrimRight(string(raw), "\r\n"), "")
		}
		r.Lines++
		r.Bytes += it.size
		r.LevelBytes[it.level] += it.size
		r.LevelLines[it.level]++
		if it.hasTime {
			if !r.HasTime || it.time.Before(r.First) {
				r.First = it.time
			}
			if !r.HasTime || it.time.After(r.Last) {
				r.Last = it.time
			}
			r.HasTime = true
		}
	}
}

func process(b batch) processed {
	items := make([]item, len(b.lines))
	for i, l := range b.lines {
		text := strings.TrimRight(string(l.text), "\r\n")
		rec := parse.Line(text)
		tokens := mask.Tokens(rec.Message)
		if len(tokens) == 0 {
			tokens = []string{"<empty line>"}
		}
		items[i] = item{
			tokens:  tokens,
			key:     strings.Join(tokens, " "),
			size:    l.size,
			level:   rec.Level,
			time:    rec.Time,
			hasTime: rec.HasTime,
			raw:     l.text,
		}
	}
	return processed{index: b.index, items: items}
}

func open(r io.Reader) (io.Reader, error) {
	br := bufio.NewReaderSize(r, readBuffer)
	head, err := br.Peek(2)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, bufio.ErrBufferFull) {
		return nil, err
	}
	if len(head) == 2 && head[0] == 0x1f && head[1] == 0x8b {
		gz, err := gzip.NewReader(br)
		if err != nil {
			return nil, err
		}
		return bufio.NewReaderSize(gz, readBuffer), nil
	}
	return br, nil
}

func read(ctx context.Context, src Source, maxLine int, index *int, slots chan<- struct{}, out chan<- batch) (int64, error) {
	r, err := open(src.Reader)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", src.Name, err)
	}
	br, ok := r.(*bufio.Reader)
	if !ok {
		br = bufio.NewReaderSize(r, readBuffer)
	}
	var cut, pendingBytes int64
	current := batch{index: *index, lines: make([]line, 0, batchLines)}
	flush := func() bool {
		if len(current.lines) == 0 {
			return true
		}
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			return false
		}
		select {
		case out <- current:
		case <-ctx.Done():
			return false
		}
		*index++
		pendingBytes = 0
		current = batch{index: *index, lines: make([]line, 0, batchLines)}
		return true
	}
	for {
		text, size, truncated, err := readLine(br, maxLine)
		if size > 0 {
			current.lines = append(current.lines, line{text: text, size: size})
			pendingBytes += int64(len(text))
			if truncated {
				cut++
			}
			if (len(current.lines) == batchLines || pendingBytes >= batchBytes) && !flush() {
				return cut, ctx.Err()
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return cut, fmt.Errorf("%s: %w", src.Name, err)
		}
	}
	if !flush() {
		return cut, ctx.Err()
	}
	return cut, nil
}

func readLine(br *bufio.Reader, maxLine int) ([]byte, int64, bool, error) {
	var buf []byte
	var size int64
	truncated := false
	for {
		chunk, err := br.ReadSlice('\n')
		size += int64(len(chunk))
		if room := maxLine - len(buf); room > 0 {
			if len(chunk) > room {
				buf = append(buf, chunk[:room]...)
				truncated = true
			} else {
				buf = append(buf, chunk...)
			}
		} else if len(chunk) > 0 {
			truncated = true
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return buf, size, truncated, err
	}
}
