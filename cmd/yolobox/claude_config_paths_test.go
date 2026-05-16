package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeMarketplacesFile is a helper that creates a known_marketplaces.json
// file at the given path with the supplied raw JSON content.
func writeMarketplacesFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir parents: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestPreprocessClaudeMarketplacesRewritesHostPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	src := filepath.Join(home, ".claude", "plugins", "known_marketplaces.json")
	content := `{
  "claude-plugins-official": {
    "source": {"source": "github", "repo": "anthropics/claude-plugins-official"},
    "installLocation": "` + home + `/.claude/plugins/marketplaces/claude-plugins-official",
    "lastUpdated": "2026-05-14T08:20:52.576Z"
  }
}`
	writeMarketplacesFile(t, src, content)

	out := preprocessClaudeMarketplaces(src, home)
	if out == "" {
		t.Fatal("expected non-empty result path for rewritable file")
	}

	// Result should land under ~/.yolobox/tmp/.
	expectedPrefix := filepath.Join(home, ".yolobox", "tmp") + string(os.PathSeparator)
	if !strings.HasPrefix(out, expectedPrefix) {
		t.Errorf("expected result path under %s, got %s", expectedPrefix, out)
	}
	base := filepath.Base(out)
	if !strings.HasPrefix(base, "known_marketplaces-") || !strings.HasSuffix(base, ".json") {
		t.Errorf("expected temp file matching known_marketplaces-*.json, got %s", base)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}

	var parsed map[string]map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v\n%s", err, data)
	}
	entry, ok := parsed["claude-plugins-official"]
	if !ok {
		t.Fatalf("missing claude-plugins-official entry: %s", data)
	}
	loc, _ := entry["installLocation"].(string)
	wantLoc := "/home/yolo/.claude/plugins/marketplaces/claude-plugins-official"
	if loc != wantLoc {
		t.Errorf("installLocation = %q, want %q", loc, wantLoc)
	}
	if strings.Contains(string(data), home) {
		t.Errorf("rewritten file still contains host home %q:\n%s", home, data)
	}
}

func TestPreprocessClaudeMarketplacesNoOpAlreadyRewritten(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	src := filepath.Join(home, ".claude", "plugins", "known_marketplaces.json")
	content := `{
  "official": {
    "installLocation": "/home/yolo/.claude/plugins/marketplaces/official"
  }
}`
	writeMarketplacesFile(t, src, content)

	out := preprocessClaudeMarketplaces(src, home)
	if out != "" {
		t.Errorf("expected empty result when nothing to rewrite, got %s", out)
	}
}

func TestPreprocessClaudeMarketplacesNoOpForeignPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	src := filepath.Join(home, ".claude", "plugins", "known_marketplaces.json")
	// installLocation does NOT begin with the supplied hostHome.
	content := `{
  "external": {
    "installLocation": "/opt/some/other/place"
  }
}`
	writeMarketplacesFile(t, src, content)

	out := preprocessClaudeMarketplaces(src, home)
	if out != "" {
		t.Errorf("expected empty result when no entry starts with hostHome, got %s", out)
	}
}

func TestPreprocessClaudeMarketplacesMissingFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	src := filepath.Join(home, ".claude", "plugins", "known_marketplaces.json")
	// Note: do not create the file.

	out := preprocessClaudeMarketplaces(src, home)
	if out != "" {
		t.Errorf("expected empty result when source missing, got %s", out)
	}
}

func TestPreprocessClaudeMarketplacesMalformedJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	src := filepath.Join(home, ".claude", "plugins", "known_marketplaces.json")
	writeMarketplacesFile(t, src, `{"broken": `)

	out := preprocessClaudeMarketplaces(src, home)
	if out != "" {
		t.Errorf("expected empty result when JSON is malformed, got %s", out)
	}
}

func TestPreprocessClaudeMarketplacesPartialRewrite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	src := filepath.Join(home, ".claude", "plugins", "known_marketplaces.json")
	content := `{
  "official": {
    "installLocation": "` + home + `/.claude/plugins/marketplaces/official"
  },
  "external": {
    "installLocation": "/opt/some/other/place"
  },
  "broken": {
    "source": {"source": "github"}
  }
}`
	writeMarketplacesFile(t, src, content)

	out := preprocessClaudeMarketplaces(src, home)
	if out == "" {
		t.Fatal("expected non-empty result path when at least one entry is rewritable")
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	var parsed map[string]map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid JSON in result: %v", err)
	}

	if got, _ := parsed["official"]["installLocation"].(string); got != "/home/yolo/.claude/plugins/marketplaces/official" {
		t.Errorf("official installLocation = %q, want %q", got, "/home/yolo/.claude/plugins/marketplaces/official")
	}
	if got, _ := parsed["external"]["installLocation"].(string); got != "/opt/some/other/place" {
		t.Errorf("external installLocation = %q, want it left alone", got)
	}
	if _, ok := parsed["broken"]; !ok {
		t.Error("entry without installLocation should be preserved as-is")
	}
}
