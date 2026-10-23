package main

import (
	"bufio"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"strconv"
	"strings"
	"time"
)

type template struct {
	weight int
	write  func(w *bufio.Writer, r *rand.Rand, t time.Time)
}

var users = []string{"anna", "boris", "chen", "dana", "egor", "fatima", "gleb", "hana"}
var paths = []string{"/api/orders", "/api/users", "/api/cart", "/api/search", "/api/products"}

func id(r *rand.Rand) string { return strconv.FormatUint(r.Uint64()>>16, 16) }

func stamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

var templates = []template{
	{30, func(w *bufio.Writer, r *rand.Rand, t time.Time) {
		fmt.Fprintf(w, "%s INFO  [http] GET /healthz 200 in %dms from 10.0.%d.%d\n", stamp(t), r.IntN(5), r.IntN(255), r.IntN(255))
	}},
	{18, func(w *bufio.Writer, r *rand.Rand, t time.Time) {
		fmt.Fprintf(w, "%s DEBUG [cache] hit key=product:%d ttl=%ds size=%d\n", stamp(t), r.IntN(90000), 60+r.IntN(240), r.IntN(4096))
	}},
	{14, func(w *bufio.Writer, r *rand.Rand, t time.Time) {
		fmt.Fprintf(w, `{"time":"%s","level":"info","msg":"request finished","method":"GET","path":"%s/%d","status":200,"duration_ms":%d,"request_id":"%s"}`+"\n",
			stamp(t), paths[r.IntN(len(paths))], r.IntN(100000), r.IntN(900), id(r))
	}},
	{9, func(w *bufio.Writer, r *rand.Rand, t time.Time) {
		fmt.Fprintf(w, "%s DEBUG [db] SELECT * FROM orders WHERE user_id = %d AND status = 'open' LIMIT 50 took %dms\n", stamp(t), r.IntN(100000), r.IntN(80))
	}},
	{8, func(w *bufio.Writer, r *rand.Rand, t time.Time) {
		fmt.Fprintf(w, "%s INFO  [kafka] committed offset %d for partition %d of topic orders.events\n", stamp(t), r.Int64N(1e9), r.IntN(12))
	}},
	{6, func(w *bufio.Writer, r *rand.Rand, t time.Time) {
		user := users[r.IntN(len(users))]
		fmt.Fprintf(w, "%s INFO  [auth] user %s@example.com logged in, session_id=%s token=%s\n", stamp(t), user, id(r), id(r)+id(r))
	}},
	{5, func(w *bufio.Writer, r *rand.Rand, t time.Time) {
		fmt.Fprintf(w, "%s WARN  [payments] retrying charge %s, attempt %d of 3: upstream timeout after %dms\n", stamp(t), id(r), 1+r.IntN(3), 1000+r.IntN(4000))
	}},
	{4, func(w *bufio.Writer, r *rand.Rand, t time.Time) {
		fmt.Fprintf(w, "%s TRACE [scheduler] tick worker=%d queue_depth=%d\n", stamp(t), r.IntN(32), r.IntN(500))
	}},
	{3, func(w *bufio.Writer, r *rand.Rand, t time.Time) {
		fmt.Fprintf(w, "%s ERROR [orders] failed to reserve stock for order %s: item %d out of stock\n", stamp(t), id(r), r.IntN(5000))
	}},
	{2, func(w *bufio.Writer, r *rand.Rand, t time.Time) {
		fmt.Fprintf(w, "%s INFO  [mailer] sent receipt to %s@example.com in %dms\n", stamp(t), users[r.IntN(len(users))], 50+r.IntN(400))
	}},
	{1, func(w *bufio.Writer, r *rand.Rand, t time.Time) {
		fmt.Fprintf(w, "%s ERROR [http] POST /api/login 500: db connection refused (postgres://app:%s@db.internal:5432/shop)\n", stamp(t), id(r))
	}},
}

func parseSize(s string) (int64, error) {
	s = strings.ToUpper(strings.TrimSpace(s))
	mult := int64(1)
	for _, u := range []struct {
		suffix string
		mult   int64
	}{{"GB", 1e9}, {"MB", 1e6}, {"KB", 1e3}} {
		if strings.HasSuffix(s, u.suffix) {
			s, mult = strings.TrimSuffix(s, u.suffix), u.mult
			break
		}
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	return int64(n * float64(mult)), nil
}

type counter struct {
	w io.Writer
	n int64
}

func (c *counter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

func main() {
	sizeFlag := flag.String("size", "50MB", "approximate amount of logs to write")
	span := flag.Duration("span", 24*time.Hour, "time range the logs cover")
	seed := flag.Uint64("seed", 1, "random seed")
	gz := flag.Bool("gzip", false, "compress the output")
	flag.Parse()

	size, err := parseSize(*sizeFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, "loggen:", err)
		os.Exit(2)
	}
	var out io.Writer = os.Stdout
	var zw *gzip.Writer
	if *gz {
		zw = gzip.NewWriter(os.Stdout)
		out = zw
	}
	c := &counter{w: out}
	w := bufio.NewWriterSize(c, 1<<20)
	r := rand.New(rand.NewPCG(*seed, *seed^0x9e3779b97f4a7c15))
	total := 0
	for _, t := range templates {
		total += t.weight
	}
	start := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	estimate := size / 120
	var written int64
	for i := int64(0); written < size; i++ {
		pick := r.IntN(total)
		for _, t := range templates {
			if pick < t.weight {
				at := start.Add(time.Duration(float64(*span) * float64(i) / float64(estimate)))
				if at.After(start.Add(*span)) {
					at = start.Add(*span)
				}
				t.write(w, r, at)
				break
			}
			pick -= t.weight
		}
		if i%1024 == 0 {
			written = c.n + int64(w.Buffered())
		}
	}
	if err := w.Flush(); err != nil {
		fmt.Fprintln(os.Stderr, "loggen:", err)
		os.Exit(1)
	}
	if zw != nil {
		if err := zw.Close(); err != nil {
			fmt.Fprintln(os.Stderr, "loggen:", err)
			os.Exit(1)
		}
	}
}
