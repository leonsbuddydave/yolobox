package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// containerHomeDir is where the entrypoint installs the staged claude config.
const containerHomeDir = "/home/yolo"

// rewriteHostPathStrings recursively walks a decoded JSON value, replacing
// every string that begins with hostPrefix with a string rooted at
// containerPrefix. Returns true if any replacement occurred.
//
// Mutates maps and slices in place. Caller passes the parsed JSON (typically
// any/interface{}) and re-marshals after.
func rewriteHostPathStrings(value any, hostPrefix, containerPrefix string) bool {
	changed := false
	switch v := value.(type) {
	case map[string]any:
		for k, child := range v {
			if s, ok := child.(string); ok {
				if strings.HasPrefix(s, hostPrefix) {
					v[k] = containerPrefix + strings.TrimPrefix(s, hostPrefix)
					changed = true
				}
				continue
			}
			if rewriteHostPathStrings(child, hostPrefix, containerPrefix) {
				changed = true
			}
		}
	case []any:
		for i, child := range v {
			if s, ok := child.(string); ok {
				if strings.HasPrefix(s, hostPrefix) {
					v[i] = containerPrefix + strings.TrimPrefix(s, hostPrefix)
					changed = true
				}
				continue
			}
			if rewriteHostPathStrings(child, hostPrefix, containerPrefix) {
				changed = true
			}
		}
	}
	return changed
}

// preprocessClaudeConfigJSON reads srcPath as JSON, rewrites every string
// beginning with hostHome+"/" to start with containerHomeDir+"/", and writes
// the result to a unique temp file under ~/.yolobox/tmp/. Returns the temp
// path, or "" if no rewriting was needed (file missing, unreadable, malformed,
// or no entries required rewriting).
func preprocessClaudeConfigJSON(srcPath, hostHome string) string {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return ""
	}
	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		return ""
	}
	hostPrefix := strings.TrimRight(hostHome, "/") + "/"
	containerPrefix := containerHomeDir + "/"
	if !rewriteHostPathStrings(doc, hostPrefix, containerPrefix) {
		return ""
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	tmpBase := filepath.Join(home, ".yolobox", "tmp")
	if err := os.MkdirAll(tmpBase, 0700); err != nil {
		return ""
	}
	f, err := os.CreateTemp(tmpBase, "claude-config-rewritten-*.json")
	if err != nil {
		return ""
	}
	if _, err := f.Write(out); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return ""
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return ""
	}
	return f.Name()
}

// claudeConfigPathRewriteFiles lists files under ~/.claude/ whose JSON
// contents may contain host paths that need rewriting for the container.
// Each path is relative to the claude config dir. Files not present on disk
// are silently skipped.
//
// Notably excluded: sessions/*.json, projects/*/..., history.jsonl, plans/*.md,
// *.bak.*. Those are historical records, not config — rewriting them risks
// corrupting history that references other host projects.
var claudeConfigPathRewriteFiles = []string{
	"plugins/known_marketplaces.json",
	"plugins/installed_plugins.json",
	"settings.json",
}
