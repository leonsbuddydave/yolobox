package main

import (
	"fmt"
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
