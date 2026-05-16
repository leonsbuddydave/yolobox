package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// containerHomeDir is where the entrypoint installs the staged claude config.
const containerHomeDir = "/home/yolo"

// preprocessClaudeMarketplaces reads known_marketplaces.json at srcPath,
// rewrites each installLocation that begins with hostHome to a /home/yolo path,
// and writes the rewritten JSON to a temp file under ~/.yolobox/tmp/. Returns
// the temp path, or "" if no rewriting was needed (file missing, unreadable,
// malformed, or no entries required rewriting).
func preprocessClaudeMarketplaces(srcPath, hostHome string) string {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return ""
	}
	var marketplaces map[string]interface{}
	if err := json.Unmarshal(data, &marketplaces); err != nil {
		return ""
	}
	hostPrefix := strings.TrimRight(hostHome, "/") + "/"

	changed := false
	for _, raw := range marketplaces {
		entry, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		locRaw, ok := entry["installLocation"]
		if !ok {
			continue
		}
		loc, ok := locRaw.(string)
		if !ok {
			continue
		}
		if !strings.HasPrefix(loc, hostPrefix) {
			continue
		}
		entry["installLocation"] = containerHomeDir + "/" + strings.TrimPrefix(loc, hostPrefix)
		changed = true
	}
	if !changed {
		return ""
	}
	out, err := json.MarshalIndent(marketplaces, "", "  ")
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
	f, err := os.CreateTemp(tmpBase, "known_marketplaces-*.json")
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
