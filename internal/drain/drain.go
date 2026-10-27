package drain

import (
	"strings"

	"github.com/wellkyc/logcost/internal/mask"
	"github.com/wellkyc/logcost/internal/parse"
)

const (
	treeDepth     = 1
	maxChildren   = 100
	maxCacheSize  = 200_000
	maxCacheBytes = 32 << 20
	maxCacheKey   = 1024
	linearLimit   = 64
	indexedTokens = 12
	hotPosting    = 256
	OtherName     = "<other lines, too many templates>"
)

type Side struct {
	Bytes  int64
	Events int64
}

type Cluster struct {
	Sides   [2]Side
	Tokens  []string
	Events  int64
	JSON    int64
	Lines   int64
	Bytes   int64
	Levels  [parse.LevelCount]int64
	Example string
	id      int
}

func (c *Cluster) Template() string {
	return strings.Join(c.Tokens, " ")
}

func (c *Cluster) Level() parse.Level {
	best := parse.Unknown
	for l := parse.Level(1); l < parse.LevelCount; l++ {
		if c.Levels[l] > c.Levels[best] {
			best = l
		}
	}
	return best
}

type posting struct {
	pos   int
	token string
}

type node struct {
	children  map[string]*node
	clusters  []*Cluster
	index     map[posting][]*Cluster
	unindexed []*Cluster
}

func newNode() *node {
	return &node{children: make(map[string]*node)}
}

type Miner struct {
	similarity  float64
	maxClusters int
	roots       map[int]*node
	clusters    []*Cluster
	cache       map[string]*Cluster
	cacheBytes  int
	other       *Cluster
	seen        map[*Cluster]struct{}
}

func New(similarity float64, maxClusters int) *Miner {
	return &Miner{
		similarity:  similarity,
		maxClusters: maxClusters,
		roots:       make(map[int]*node),
		cache:       make(map[string]*Cluster),
		seen:        make(map[*Cluster]struct{}),
	}
}

func (m *Miner) Clusters() []*Cluster {
	out := make([]*Cluster, 0, len(m.clusters)+1)
	out = append(out, m.clusters...)
	if m.other != nil {
		out = append(out, m.other)
	}
	return out
}

func treeKey(token string) string {
	for i := 0; i < len(token); i++ {
		if token[i] >= '0' && token[i] <= '9' {
			return mask.Wildcard
		}
	}
	if strings.Contains(token, mask.Wildcard) {
		return mask.Wildcard
	}
	return token
}

func (m *Miner) Add(tokens []string, key string) *Cluster {
	if c, ok := m.cache[key]; ok {
		return c
	}
	c := m.match(tokens)
	if c != nil {
		merge(c, tokens)
	} else if len(m.clusters) >= m.maxClusters {
		if m.other == nil {
			m.other = &Cluster{Tokens: []string{OtherName}, id: -1}
		}
		return m.other
	} else {
		owned := make([]string, len(tokens))
		for i, t := range tokens {
			owned[i] = strings.Clone(t)
		}
		c = &Cluster{Tokens: owned, id: len(m.clusters)}
		m.insert(c)
		m.clusters = append(m.clusters, c)
	}
	if len(key) <= maxCacheKey && len(m.cache) < maxCacheSize && m.cacheBytes+len(key) <= maxCacheBytes {
		m.cache[strings.Clone(key)] = c
		m.cacheBytes += len(key)
	}
	return c
}

func (m *Miner) leaf(tokens []string) *node {
	cur := m.roots[len(tokens)]
	if cur == nil {
		return nil
	}
	for i := 0; i < treeDepth && i < len(tokens); i++ {
		next := cur.children[treeKey(tokens[i])]
		if next == nil {
			next = cur.children[mask.Wildcard]
		}
		if next == nil {
			return nil
		}
		cur = next
	}
	return cur
}

type candidate struct {
	cluster *Cluster
	score   float64
	params  int
}

func (a candidate) better(b candidate) bool {
	if a.score != b.score {
		return a.score > b.score
	}
	if a.params != b.params {
		return a.params > b.params
	}
	return a.cluster.id < b.cluster.id
}

func (m *Miner) match(tokens []string) *Cluster {
	leaf := m.leaf(tokens)
	if leaf == nil {
		return nil
	}
	best := candidate{score: -1}
	consider := func(c *Cluster) {
		score, params := similarity(c.Tokens, tokens)
		next := candidate{cluster: c, score: score, params: params}
		if best.cluster == nil || next.better(best) {
			best = next
		}
	}
	if len(leaf.clusters) <= linearLimit {
		for _, c := range leaf.clusters {
			consider(c)
		}
	} else {
		clear(m.seen)
		for i, t := range tokens {
			if i >= indexedTokens {
				break
			}
			list := leaf.index[posting{i, t}]
			if len(list) > hotPosting {
				continue
			}
			for _, c := range list {
				if _, done := m.seen[c]; !done {
					m.seen[c] = struct{}{}
					consider(c)
				}
			}
		}
		for _, c := range leaf.unindexed {
			consider(c)
		}
	}
	if best.cluster == nil || best.score < m.similarity {
		return nil
	}
	return best.cluster
}

func similarity(template, tokens []string) (float64, int) {
	if len(template) == 0 {
		return 1, 0
	}
	same, params := 0, 0
	for i, t := range template {
		switch {
		case t == mask.Wildcard:
			params++
		case t == tokens[i]:
			same++
		}
	}
	return float64(same) / float64(len(template)), params
}

func merge(c *Cluster, tokens []string) {
	for i, t := range c.Tokens {
		if t == mask.Wildcard || t == tokens[i] {
			continue
		}
		if key := mask.Key(t); key != "" && key == mask.Key(tokens[i]) {
			c.Tokens[i] = key + mask.Wildcard
		} else {
			c.Tokens[i] = mask.Edges(t, tokens[i])
		}
	}
}

func (m *Miner) insert(c *Cluster) {
	size := len(c.Tokens)
	cur := m.roots[size]
	if cur == nil {
		cur = newNode()
		m.roots[size] = cur
	}
	for i := 0; i < treeDepth && i < size; i++ {
		key := treeKey(c.Tokens[i])
		next := cur.children[key]
		if next == nil {
			if len(cur.children) >= maxChildren {
				key = mask.Wildcard
				next = cur.children[key]
			}
			if next == nil {
				next = newNode()
				cur.children[key] = next
			}
		}
		cur = next
	}
	cur.clusters = append(cur.clusters, c)
	if cur.index == nil {
		cur.index = make(map[posting][]*Cluster)
	}
	indexed := false
	for i, t := range c.Tokens {
		if i >= indexedTokens {
			break
		}
		if t == mask.Wildcard {
			continue
		}
		p := posting{i, t}
		if len(cur.index[p]) <= hotPosting {
			cur.index[p] = append(cur.index[p], c)
			indexed = true
		}
	}
	if !indexed {
		cur.unindexed = append(cur.unindexed, c)
	}
}
