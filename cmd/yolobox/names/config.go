package names

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
)

//go:embed defaults/*.json
var defaultsFS embed.FS

var (
	forkNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	slotRe          = regexp.MustCompile(`\{([a-z]+)\}`)
	knownPools      = []string{"adjective", "noun", "name", "verb"}
)

type patternEntry struct {
	Pattern string `json:"pattern"`
	Weight  *int   `json:"weight,omitempty"`
}

type wordEnvelope struct {
	Mode  string   `json:"mode"`
	Items []string `json:"items"`
}

type patternEnvelope struct {
	Mode  string         `json:"mode"`
	Items []patternEntry `json:"items"`
}

func loadAll(overrideDir string) ([]patternEntry, map[string][]string, error) {
	pools := make(map[string][]string)
	for _, pool := range knownPools {
		defaults, err := loadEmbeddedWords(pool)
		if err != nil {
			return nil, nil, err
		}
		pools[pool] = defaults
	}
	patterns, err := loadEmbeddedPatterns()
	if err != nil {
		return nil, nil, err
	}

	if overrideDir != "" {
		for _, pool := range knownPools {
			extended, err := applyWordOverride(filepath.Join(overrideDir, pool+".json"), pools[pool])
			if err != nil {
				return nil, nil, err
			}
			pools[pool] = extended
		}
		patterns, err = applyPatternOverride(filepath.Join(overrideDir, "patterns.json"), patterns)
		if err != nil {
			return nil, nil, err
		}
	}

	if err := validatePools(pools); err != nil {
		return nil, nil, err
	}
	if err := validatePatterns(patterns, pools); err != nil {
		return nil, nil, err
	}
	return patterns, pools, nil
}

func loadEmbeddedWords(pool string) ([]string, error) {
	data, err := fs.ReadFile(defaultsFS, "defaults/"+pool+".json")
	if err != nil {
		return nil, fmt.Errorf("read embedded %s: %w", pool, err)
	}
	var env wordEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("parse embedded %s.json: %w", pool, err)
	}
	return env.Items, nil
}

func loadEmbeddedPatterns() ([]patternEntry, error) {
	data, err := fs.ReadFile(defaultsFS, "defaults/patterns.json")
	if err != nil {
		return nil, err
	}
	var env patternEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("parse embedded patterns.json: %w", err)
	}
	return env.Items, nil
}

func applyWordOverride(path string, defaults []string) ([]string, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return defaults, nil
		}
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var env wordEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	mode, err := envelopeMode(env.Mode, path)
	if err != nil {
		return nil, err
	}
	switch mode {
	case "append":
		merged := make([]string, 0, len(defaults)+len(env.Items))
		merged = append(merged, defaults...)
		merged = append(merged, env.Items...)
		return merged, nil
	case "replace":
		return append([]string{}, env.Items...), nil
	}
	return defaults, nil
}

func applyPatternOverride(path string, defaults []patternEntry) ([]patternEntry, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return defaults, nil
		}
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var env patternEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	mode, err := envelopeMode(env.Mode, path)
	if err != nil {
		return nil, err
	}
	switch mode {
	case "append":
		merged := make([]patternEntry, 0, len(defaults)+len(env.Items))
		merged = append(merged, defaults...)
		merged = append(merged, env.Items...)
		return merged, nil
	case "replace":
		return append([]patternEntry{}, env.Items...), nil
	}
	return defaults, nil
}

func envelopeMode(mode, path string) (string, error) {
	if mode == "" {
		return "append", nil
	}
	if mode != "append" && mode != "replace" {
		return "", fmt.Errorf("%s: invalid mode %q (expected \"append\" or \"replace\")", path, mode)
	}
	return mode, nil
}

func validatePools(pools map[string][]string) error {
	for name, words := range pools {
		for _, w := range words {
			if !forkNamePattern.MatchString(w) {
				return fmt.Errorf("invalid word in pool %q: %q does not match fork-name regex", name, w)
			}
		}
	}
	return nil
}

func validatePatterns(patterns []patternEntry, pools map[string][]string) error {
	if len(patterns) == 0 {
		return fmt.Errorf("no patterns available")
	}
	for _, p := range patterns {
		if p.Pattern == "" {
			return fmt.Errorf("pattern entry has empty pattern field")
		}
		if p.Weight != nil && *p.Weight < 0 {
			return fmt.Errorf("pattern %q has negative weight", p.Pattern)
		}
		for _, m := range slotRe.FindAllStringSubmatch(p.Pattern, -1) {
			slot := m[1]
			if _, ok := pools[slot]; !ok {
				return fmt.Errorf("pattern %q references unknown slot {%s}", p.Pattern, slot)
			}
		}
	}
	return nil
}

// OverrideDir returns the directory where users can place override JSON files.
// Follows yolobox's existing XDG convention.
func OverrideDir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "yolobox", "names"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "yolobox", "names"), nil
}
