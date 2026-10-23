package drain

import (
	"fmt"
	"strings"
	"testing"

	"github.com/wellkyc/logcost/internal/mask"
)

func add(m *Miner, line string) *Cluster {
	tokens := mask.Tokens(line)
	return m.Add(tokens, strings.Join(tokens, " "))
}

func TestGroupsSimilarLines(t *testing.T) {
	m := New(0.4, 100)
	a := add(m, "user anna logged in from web")
	b := add(m, "user boris logged in from mobile")
	c := add(m, "disk /dev/sda1 is 91% full")
	if a != b {
		t.Fatal("similar lines ended up in different clusters")
	}
	if a == c {
		t.Fatal("different lines share a cluster")
	}
	if got := a.Template(); got != "user <*> logged in from <*>" {
		t.Fatalf("template %q", got)
	}
}

func TestKeepsKeyNamesWhenMerging(t *testing.T) {
	m := New(0.4, 100)
	add(m, "cache hit key=alpha")
	c := add(m, "cache hit key=beta")
	if got := c.Template(); got != "cache hit key=<*>" {
		t.Fatalf("template %q", got)
	}
}

func TestKeepsPunctuationAroundVariables(t *testing.T) {
	m := New(0.4, 100)
	add(m, "retrying charge abcdef, attempt 1")
	c := add(m, "retrying charge zzzqqq, attempt 2")
	if got := c.Template(); got != "retrying charge <*>, attempt <*>" {
		t.Fatalf("template %q", got)
	}
}

func TestFindsMatchInLargeLeaf(t *testing.T) {
	m := New(0.4, 10_000)
	var target *Cluster
	for i := range 300 {
		c := add(m, fmt.Sprintf("job %s finished step %s", word(i), word(i+1000)))
		if i == 150 {
			target = c
		}
	}
	got := add(m, fmt.Sprintf("job %s finished step %s", word(150), "different"))
	if got != target {
		t.Fatalf("matched %q instead of %q", got.Template(), target.Template())
	}
}

func word(n int) string {
	letters := "abcdefghijklmnopqrstuvwxyz"
	b := make([]byte, 5)
	for i := range b {
		b[i] = letters[n%26]
		n /= 26
	}
	return string(b)
}

func TestDifferentLengthsAreSeparate(t *testing.T) {
	m := New(0.4, 100)
	if add(m, "a b c") == add(m, "a b c d") {
		t.Fatal("lines of different length merged")
	}
}

func TestClusterLimit(t *testing.T) {
	m := New(0.9, 5)
	for i := range 50 {
		add(m, strings.Repeat("x", i+1)+" unique words "+strings.Repeat("y", i+1))
	}
	clusters := m.Clusters()
	if len(clusters) != 6 || clusters[5].Template() != OtherName {
		t.Fatalf("got %d clusters, last %q", len(clusters), clusters[len(clusters)-1].Template())
	}
}

func FuzzAdd(f *testing.F) {
	f.Add("a b c", "a x c")
	f.Add("", "<*>")
	f.Fuzz(func(t *testing.T, first, second string) {
		m := New(0.4, 50)
		add(m, first)
		c := add(m, second)
		if c == nil {
			t.Fatal("nil cluster")
		}
		if len(c.Tokens) != len(mask.Tokens(second)) && c.Template() != OtherName {
			t.Fatalf("template length changed: %q", c.Template())
		}
	})
}
