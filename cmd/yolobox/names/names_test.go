package names

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

func newSingleWordGenerator(t *testing.T, pools map[string][]string, pattern string) *Generator {
	t.Helper()
	patternList := []patternEntry{{Pattern: pattern}}
	if err := validatePools(pools); err != nil {
		t.Fatalf("invalid test pools: %v", err)
	}
	if err := validatePatterns(patternList, pools); err != nil {
		t.Fatalf("invalid test pattern: %v", err)
	}
	g := &Generator{patterns: patternList, pools: pools, totalWeight: 1}
	return g
}

func TestGenerateHappyPath(t *testing.T) {
	g := newSingleWordGenerator(t, map[string][]string{
		"adjective": {"shiny"},
		"noun":      {"orb"},
		"name":      {"fred"},
		"verb":      {"poke"},
	}, "{adjective}-{noun}")
	name, err := g.Generate(Options{Rand: bytes.NewBuffer(make([]byte, 64))})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if name != "shiny-orb" {
		t.Errorf("expected shiny-orb; got %q", name)
	}
}

func TestGenerateRetryExhaustion(t *testing.T) {
	g := newSingleWordGenerator(t, map[string][]string{
		"adjective": {"shiny"},
		"noun":      {"orb"},
		"name":      {"fred"},
		"verb":      {"poke"},
	}, "{adjective}-{noun}")
	calls := 0
	_, err := g.Generate(Options{
		Rand:        bytes.NewBuffer(make([]byte, 1024)),
		MaxAttempts: 5,
		Collides: func(string) bool {
			calls++
			return true
		},
	})
	if err == nil {
		t.Fatal("expected exhaustion error")
	}
	if !strings.Contains(err.Error(), "after 5 attempts") {
		t.Errorf("error should mention attempts; got %v", err)
	}
	if calls != 5 {
		t.Errorf("expected 5 collision checks; got %d", calls)
	}
}

func TestGenerateRespectsWeights(t *testing.T) {
	// Two patterns, weights 1 and 0. The zero-weight pattern must never be picked.
	zero := 0
	one := 1
	patterns := []patternEntry{
		{Pattern: "{adjective}-{noun}", Weight: &one},
		{Pattern: "{verb}-{name}", Weight: &zero},
	}
	pools := map[string][]string{
		"adjective": {"shiny"},
		"noun":      {"orb"},
		"name":      {"fred"},
		"verb":      {"poke"},
	}
	if err := validatePools(pools); err != nil {
		t.Fatal(err)
	}
	if err := validatePatterns(patterns, pools); err != nil {
		t.Fatal(err)
	}
	g := &Generator{patterns: patterns, pools: pools, totalWeight: 1}

	for i := 0; i < 20; i++ {
		name, err := g.Generate(Options{Rand: bytes.NewBuffer(make([]byte, 1024))})
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		if name != "shiny-orb" {
			t.Fatalf("zero-weight pattern leaked through: got %q", name)
		}
	}
}

func TestGenerateSubstitutionOverrun(t *testing.T) {
	long := strings.Repeat("a", 60)
	g := newSingleWordGenerator(t, map[string][]string{
		"adjective": {long},
		"noun":      {long, "ok"},
		"name":      {"fred"},
		"verb":      {"poke"},
	}, "{adjective}-{noun}")
	// Pattern is always picked (single pattern, weight 1), adjective is always
	// `long`. Noun choice is `long` for even index, `ok` for odd. The generator
	// must reject the (long, long) result (regex fails: > 63 chars) and retry
	// until noun index lands on 1 ("ok").
	r := alternatingReader{}
	name, err := g.Generate(Options{Rand: &r, MaxAttempts: 50})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !forkNamePattern.MatchString(name) {
		t.Errorf("name %q failed fork-name regex", name)
	}
	if !strings.HasSuffix(name, "-ok") {
		t.Errorf("expected eventual retry to land on -ok; got %q", name)
	}
}

// alternatingReader streams bytes that alternate between 0 and 1 so consecutive
// randIntn(_, 2) calls return alternating 0 and 1 outcomes.
type alternatingReader struct {
	counter uint64
}

func (a *alternatingReader) Read(p []byte) (int, error) {
	if len(p) < 8 {
		return 0, nil
	}
	binary.BigEndian.PutUint64(p[:8], a.counter)
	a.counter++
	return 8, nil
}

func TestLoadFromEmbeddedSamplesAreValid(t *testing.T) {
	g, err := loadWithDir("")
	if err != nil {
		t.Fatalf("loadWithDir: %v", err)
	}
	seen := make(map[string]struct{})
	for i := 0; i < 200; i++ {
		name, err := g.Generate(Options{})
		if err != nil {
			t.Fatalf("generate %d: %v", i, err)
		}
		if !forkNamePattern.MatchString(name) {
			t.Errorf("generated invalid name: %q", name)
		}
		seen[name] = struct{}{}
	}
	if len(seen) < 100 {
		t.Errorf("expected high uniqueness across 200 samples; got %d unique", len(seen))
	}
}
