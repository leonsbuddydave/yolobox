package main

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
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

// parseShareFlag parses a --share value: "path" or "path:rw" or "path:ro".
// Returned SharedPath is *not yet validated* against project structure — call
// validateSharedPaths after merging with TOML entries.
func parseShareFlag(value string) (SharedPath, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return SharedPath{}, fmt.Errorf("--share value must not be empty")
	}
	parts := strings.Split(value, ":")
	if len(parts) > 2 {
		return SharedPath{}, fmt.Errorf("--share value %q: expected <path>[:rw|:ro]", value)
	}
	pathStr := strings.TrimSpace(parts[0])
	if pathStr == "" {
		return SharedPath{}, fmt.Errorf("--share value %q: path is empty", value)
	}
	mode := "rw"
	if len(parts) == 2 {
		mode = strings.TrimSpace(parts[1])
		if mode != "rw" && mode != "ro" {
			return SharedPath{}, fmt.Errorf("--share value %q: mode must be rw or ro, got %q", value, mode)
		}
	}
	return SharedPath{Path: pathStr, Mode: mode}, nil
}

// mergeSharedPaths combines TOML entries with CLI --share entries.
// CLI entries override TOML entries on path collision (CLI wins).
// Among CLI entries themselves, the last one wins.
// After merge, the full list is run through validateSharedPaths, which
// performs the ".."-escape check on raw input, normalizes paths in place,
// and runs the dedup + prefix-overlap checks. Inputs are passed through
// with only whitespace trimmed so that validate's raw-segment defense remains
// effective.
//
// Returns the merged + validated list; on validation failure returns the error.
func mergeSharedPaths(fromToml, fromCli []SharedPath) ([]SharedPath, error) {
	indexByPath := make(map[string]int)
	var result []SharedPath
	for _, e := range fromToml {
		key := strings.TrimSpace(e.Path)
		indexByPath[key] = len(result)
		result = append(result, SharedPath{Path: key, Mode: e.Mode})
	}
	for _, e := range fromCli {
		key := strings.TrimSpace(e.Path)
		if idx, ok := indexByPath[key]; ok {
			result[idx].Mode = e.Mode
			continue
		}
		indexByPath[key] = len(result)
		result = append(result, SharedPath{Path: key, Mode: e.Mode})
	}
	if err := validateSharedPaths(result); err != nil {
		return nil, err
	}
	return result, nil
}

// copyForkSource copies src/. into dst, skipping every entry in shared. If
// shared is empty it falls back to the existing copyFullDirectory (cp -a) for
// parity with prior behavior. When shared is non-empty, rsync is used so the
// excludes are honored deterministically.
func copyForkSource(src, dst string, shared []SharedPath) error {
	if len(shared) == 0 {
		return copyFullDirectory(src, dst)
	}
	if _, err := exec.LookPath("rsync"); err != nil {
		return fmt.Errorf("shared_paths requires rsync, which was not found in PATH: %w", err)
	}
	rsyncArgs := []string{"-a"}
	for _, s := range shared {
		// rsync excludes anchored to the transfer root are written as "/<path>".
		rsyncArgs = append(rsyncArgs, "--exclude=/"+s.Path)
	}
	rsyncArgs = append(rsyncArgs, src+"/", dst+"/")
	cmd := exec.Command("rsync", rsyncArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed == "" {
			trimmed = err.Error()
		}
		return fmt.Errorf("rsync failed: %s", trimmed)
	}
	return nil
}

// createSharedPathPlaceholders creates the empty mount targets inside the
// fork's copy directory and returns the subset of shared paths whose source
// actually exists in src (the only ones we should mount).
//
// Behavior:
//   - source is a directory: create an empty directory at dst/<path>.
//   - source is a file: create an empty file at dst/<path>.
//   - source does not exist: warn, skip (drop from resolved list).
func createSharedPathPlaceholders(src, dst string, shared []SharedPath) ([]SharedPath, error) {
	resolved := make([]SharedPath, 0, len(shared))
	for _, s := range shared {
		srcPath := filepath.Join(src, filepath.FromSlash(s.Path))
		info, err := os.Stat(srcPath)
		if err != nil {
			if os.IsNotExist(err) {
				warn("shared_paths: skipping %q (does not exist in source)", s.Path)
				continue
			}
			return nil, fmt.Errorf("shared_paths: stat %s: %w", s.Path, err)
		}
		target := filepath.Join(dst, filepath.FromSlash(s.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return nil, fmt.Errorf("shared_paths: prepare placeholder %s: %w", s.Path, err)
		}
		if info.IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return nil, fmt.Errorf("shared_paths: create dir placeholder %s: %w", s.Path, err)
			}
		} else {
			f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
			if err != nil {
				return nil, fmt.Errorf("shared_paths: create file placeholder %s: %w", s.Path, err)
			}
			if err := f.Close(); err != nil {
				return nil, fmt.Errorf("shared_paths: close file placeholder %s: %w", s.Path, err)
			}
		}
		resolved = append(resolved, s)
	}
	return resolved, nil
}

// buildSharedPathMountArgs returns the -v args needed to overlay each shared
// path onto its corresponding location inside the container. sourceRoot is
// the host path of the original (non-fork) project — same path that the fork
// directory is mounted at inside the container.
func buildSharedPathMountArgs(sourceRoot string, shared []SharedPath) []string {
	if len(shared) == 0 {
		return nil
	}
	args := make([]string, 0, 2*len(shared))
	for _, s := range shared {
		host := filepath.Join(sourceRoot, filepath.FromSlash(s.Path))
		spec := host + ":" + host
		if s.Mode == "ro" {
			spec += ":ro"
		}
		args = append(args, "-v", spec)
	}
	return args
}
