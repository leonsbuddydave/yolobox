package main

import (
	"fmt"
	"path"
	"strings"
)

// SharedPath represents a project subpath that, in fork mode, is bind-mounted
// from the original repo into the container instead of being physically copied.
type SharedPath struct {
	Path string `toml:"path"`
	Mode string `toml:"mode"` // "rw" or "ro"
}

// UnmarshalTOML accepts either a string (defaults to mode "rw") or a table
// with explicit { path, mode } fields.
func (s *SharedPath) UnmarshalTOML(data any) error {
	switch v := data.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("shared_paths: path must not be empty")
		}
		s.Path = v
		s.Mode = "rw"
		return nil
	case map[string]any:
		pathRaw, ok := v["path"]
		if !ok {
			return fmt.Errorf("shared_paths entry missing 'path' field")
		}
		pathStr, ok := pathRaw.(string)
		if !ok || strings.TrimSpace(pathStr) == "" {
			return fmt.Errorf("shared_paths entry 'path' must be a non-empty string")
		}
		s.Path = pathStr
		mode := "rw"
		if modeRaw, ok := v["mode"]; ok {
			modeStr, ok := modeRaw.(string)
			if !ok {
				return fmt.Errorf("shared_paths entry %q: mode must be a string", pathStr)
			}
			if modeStr != "rw" && modeStr != "ro" {
				return fmt.Errorf("shared_paths entry %q: mode must be \"rw\" or \"ro\", got %q", pathStr, modeStr)
			}
			mode = modeStr
		}
		s.Mode = mode
		return nil
	default:
		return fmt.Errorf("shared_paths entry must be a string or { path, mode } table, got %T", data)
	}
}

// ForkSettings holds the parsed [fork] TOML table.
type ForkSettings struct {
	SharedPaths []SharedPath `toml:"shared_paths"`
}

// validateSharedPaths enforces the rules from the design doc:
//   - path must be relative (no absolute, no leading "/")
//   - path must not escape the project root (no "..", no "foo/../bar")
//   - mode must be "rw" or "ro"
//   - no two entries may have the same path
//   - no entry's path may be a prefix of another entry's path
//
// On success, each entry's Path is normalized in place to its cleaned form
// (e.g. "./game/" becomes "game"). Callers should rely on the post-call
// values when comparing or printing entries.
func validateSharedPaths(entries []SharedPath) error {
	seen := make(map[string]struct{}, len(entries))
	for i := range entries {
		e := &entries[i]
		if e.Mode != "rw" && e.Mode != "ro" {
			return fmt.Errorf("shared_paths entry %q: mode must be \"rw\" or \"ro\", got %q", e.Path, e.Mode)
		}
		raw := strings.TrimSpace(e.Path)
		if raw == "" {
			return fmt.Errorf("shared_paths entry has empty path")
		}
		if path.IsAbs(raw) || strings.HasPrefix(raw, "/") {
			return fmt.Errorf("shared_paths entry %q must be relative to the project root", raw)
		}
		// Reject any ".." segment in the raw path before cleaning, since
		// path.Clean would collapse e.g. "foo/../bar" to "bar" and hide the escape.
		for _, seg := range strings.Split(raw, "/") {
			if seg == ".." {
				return fmt.Errorf("shared_paths entry %q escapes the project root", raw)
			}
		}
		clean := path.Clean(strings.TrimPrefix(raw, "./"))
		// path.Clean cannot yield ".." or a "../" prefix because the raw-segment
		// scan above rejected any literal ".." segment. The only remaining
		// degenerate result is ".", which comes from inputs like "." or "./".
		if clean == "." {
			return fmt.Errorf("shared_paths entry %q escapes the project root", raw)
		}
		e.Path = clean
		if _, dup := seen[clean]; dup {
			return fmt.Errorf("shared_paths contains duplicate entry %q", clean)
		}
		seen[clean] = struct{}{}
	}
	// Nested-overlap check (after dedup so error messages are clean).
	for i := range entries {
		for j := range entries {
			if i == j {
				continue
			}
			if isPrefixPath(entries[i].Path, entries[j].Path) {
				return fmt.Errorf("shared_paths entries overlap: %q is a prefix of %q (consolidate to one entry)",
					entries[i].Path, entries[j].Path)
			}
		}
	}
	return nil
}

// isPrefixPath returns true if parent is a strict path-segment prefix of child.
// "game" is a prefix of "game/decompile" but not of "gameplay".
func isPrefixPath(parent, child string) bool {
	if parent == child {
		return false
	}
	return strings.HasPrefix(child, parent+"/")
}
