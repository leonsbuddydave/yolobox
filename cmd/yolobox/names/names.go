package names

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
)

const DefaultMaxAttempts = 50

type Generator struct {
	patterns    []patternEntry
	pools       map[string][]string
	totalWeight int
}

type Options struct {
	Collides    func(name string) bool
	Rand        io.Reader
	MaxAttempts int
}

func Load() (*Generator, error) {
	dir, err := OverrideDir()
	if err != nil {
		return nil, err
	}
	return loadWithDir(dir)
}

func loadWithDir(overrideDir string) (*Generator, error) {
	patterns, pools, err := loadAll(overrideDir)
	if err != nil {
		return nil, err
	}
	g := &Generator{patterns: patterns, pools: pools}
	for _, p := range patterns {
		g.totalWeight += weightOf(p)
	}
	if g.totalWeight == 0 {
		return nil, fmt.Errorf("all patterns have zero weight")
	}
	return g, nil
}

func (g *Generator) Generate(opts Options) (string, error) {
	r := opts.Rand
	if r == nil {
		r = rand.Reader
	}
	max := opts.MaxAttempts
	if max <= 0 {
		max = DefaultMaxAttempts
	}

	for attempt := 0; attempt < max; attempt++ {
		pattern, err := g.choosePattern(r)
		if err != nil {
			return "", err
		}
		name, err := g.substitute(pattern, r)
		if err != nil {
			return "", err
		}
		if !forkNamePattern.MatchString(name) {
			continue
		}
		if opts.Collides != nil && opts.Collides(name) {
			continue
		}
		return name, nil
	}
	dir, _ := OverrideDir()
	return "", fmt.Errorf("couldn't generate a unique fork name after %d attempts — try `--name <name>` or expand your word lists at %s", max, dir)
}

func weightOf(p patternEntry) int {
	if p.Weight == nil {
		return 1
	}
	return *p.Weight
}

func (g *Generator) choosePattern(r io.Reader) (patternEntry, error) {
	target, err := randIntn(r, g.totalWeight)
	if err != nil {
		return patternEntry{}, err
	}
	for _, p := range g.patterns {
		w := weightOf(p)
		if target < w {
			return p, nil
		}
		target -= w
	}
	return g.patterns[len(g.patterns)-1], nil
}

func (g *Generator) substitute(p patternEntry, r io.Reader) (string, error) {
	var b strings.Builder
	remaining := p.Pattern
	for {
		m := slotRe.FindStringSubmatchIndex(remaining)
		if m == nil {
			b.WriteString(remaining)
			return b.String(), nil
		}
		b.WriteString(remaining[:m[0]])
		slot := remaining[m[2]:m[3]]
		pool := g.pools[slot]
		if len(pool) == 0 {
			return "", fmt.Errorf("pool %q is empty", slot)
		}
		idx, err := randIntn(r, len(pool))
		if err != nil {
			return "", err
		}
		b.WriteString(pool[idx])
		remaining = remaining[m[1]:]
	}
}

func randIntn(r io.Reader, n int) (int, error) {
	if n <= 0 {
		return 0, fmt.Errorf("randIntn: n must be positive")
	}
	var buf [8]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, err
	}
	v := binary.BigEndian.Uint64(buf[:])
	return int(v % uint64(n)), nil
}
