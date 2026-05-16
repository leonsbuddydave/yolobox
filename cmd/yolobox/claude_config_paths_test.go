package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConfigFile is a helper that creates a JSON config file at the given
// path with the supplied raw content.
func writeConfigFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir parents: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// ---------- rewriteHostPathStrings ----------

func TestRewriteHostPathStringsEmptyMap(t *testing.T) {
	doc := map[string]any{}
	if rewriteHostPathStrings(doc, "/host/", "/home/yolo/") {
		t.Error("expected no change for empty map")
	}
	if len(doc) != 0 {
		t.Errorf("expected map untouched, got %v", doc)
	}
}

func TestRewriteHostPathStringsNoMatches(t *testing.T) {
	doc := map[string]any{
		"a": "/opt/external/thing",
		"b": "plain string",
		"c": map[string]any{"nested": "/var/data"},
	}
	if rewriteHostPathStrings(doc, "/host/", "/home/yolo/") {
		t.Error("expected no change when no strings start with hostPrefix")
	}
	if doc["a"] != "/opt/external/thing" || doc["b"] != "plain string" {
		t.Errorf("values mutated unexpectedly: %v", doc)
	}
}

func TestRewriteHostPathStringsTopLevelMatch(t *testing.T) {
	doc := map[string]any{
		"path": "/host/sub/file",
		"keep": "/opt/other",
	}
	if !rewriteHostPathStrings(doc, "/host/", "/home/yolo/") {
		t.Fatal("expected changed=true")
	}
	if doc["path"] != "/home/yolo/sub/file" {
		t.Errorf("path = %v, want /home/yolo/sub/file", doc["path"])
	}
	if doc["keep"] != "/opt/other" {
		t.Errorf("non-matching path was modified: %v", doc["keep"])
	}
}

func TestRewriteHostPathStringsNestedMapMatch(t *testing.T) {
	doc := map[string]any{
		"outer": map[string]any{
			"inner": map[string]any{
				"location": "/host/foo/bar",
			},
		},
	}
	if !rewriteHostPathStrings(doc, "/host/", "/home/yolo/") {
		t.Fatal("expected changed=true")
	}
	got := doc["outer"].(map[string]any)["inner"].(map[string]any)["location"]
	if got != "/home/yolo/foo/bar" {
		t.Errorf("nested location = %v, want /home/yolo/foo/bar", got)
	}
}

func TestRewriteHostPathStringsNestedArrayMatch(t *testing.T) {
	doc := map[string]any{
		"items": []any{
			"/host/a",
			"plain",
			map[string]any{"path": "/host/b"},
			[]any{"/host/c", "/opt/keep"},
		},
	}
	if !rewriteHostPathStrings(doc, "/host/", "/home/yolo/") {
		t.Fatal("expected changed=true")
	}
	items := doc["items"].([]any)
	if items[0] != "/home/yolo/a" {
		t.Errorf("items[0] = %v, want /home/yolo/a", items[0])
	}
	if items[1] != "plain" {
		t.Errorf("items[1] mutated: %v", items[1])
	}
	if got := items[2].(map[string]any)["path"]; got != "/home/yolo/b" {
		t.Errorf("items[2].path = %v, want /home/yolo/b", got)
	}
	inner := items[3].([]any)
	if inner[0] != "/home/yolo/c" {
		t.Errorf("inner[0] = %v, want /home/yolo/c", inner[0])
	}
	if inner[1] != "/opt/keep" {
		t.Errorf("inner[1] mutated: %v", inner[1])
	}
}

func TestRewriteHostPathStringsMixedTypesIgnored(t *testing.T) {
	doc := map[string]any{
		"num":  float64(42),
		"bool": true,
		"null": nil,
		"path": "/host/foo",
	}
	if !rewriteHostPathStrings(doc, "/host/", "/home/yolo/") {
		t.Fatal("expected changed=true")
	}
	if doc["num"] != float64(42) {
		t.Errorf("num was mutated: %v", doc["num"])
	}
	if doc["bool"] != true {
		t.Errorf("bool was mutated: %v", doc["bool"])
	}
	if doc["null"] != nil {
		t.Errorf("null was mutated: %v", doc["null"])
	}
	if doc["path"] != "/home/yolo/foo" {
		t.Errorf("path = %v, want /home/yolo/foo", doc["path"])
	}
}

func TestRewriteHostPathStringsChangedReturnValue(t *testing.T) {
	// Document with only a string equal to hostPrefix (without trailing content)
	// — HasPrefix is true, so it should still rewrite.
	doc := map[string]any{"p": "/host/"}
	if !rewriteHostPathStrings(doc, "/host/", "/home/yolo/") {
		t.Fatal("expected changed=true even when match is exactly hostPrefix")
	}
	if doc["p"] != "/home/yolo/" {
		t.Errorf("p = %v, want /home/yolo/", doc["p"])
	}

	// Document where prefix is not matched (different leading directory).
	doc2 := map[string]any{"p": "/hostile/foo"}
	if rewriteHostPathStrings(doc2, "/host/", "/home/yolo/") {
		t.Error("expected changed=false for non-matching prefix")
	}
}

// ---------- preprocessClaudeConfigJSON ----------

func TestPreprocessClaudeConfigJSONMarketplacesShape(t *testing.T) {
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
	writeConfigFile(t, src, content)

	out := preprocessClaudeConfigJSON(src, home)
	if out == "" {
		t.Fatal("expected non-empty result path for rewritable file")
	}

	// Result should land under ~/.yolobox/tmp/.
	expectedPrefix := filepath.Join(home, ".yolobox", "tmp") + string(os.PathSeparator)
	if !strings.HasPrefix(out, expectedPrefix) {
		t.Errorf("expected result path under %s, got %s", expectedPrefix, out)
	}
	base := filepath.Base(out)
	if !strings.HasPrefix(base, "claude-config-rewritten-") || !strings.HasSuffix(base, ".json") {
		t.Errorf("expected temp file matching claude-config-rewritten-*.json, got %s", base)
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

func TestPreprocessClaudeConfigJSONInstalledPluginsShape(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	src := filepath.Join(home, ".claude", "plugins", "installed_plugins.json")
	content := `{
  "plugins": {
    "lab-notebook": [
      {
        "installPath": "` + home + `/.claude/plugins/marketplaces/official/lab-notebook",
        "version": "1.2.3"
      }
    ]
  }
}`
	writeConfigFile(t, src, content)

	out := preprocessClaudeConfigJSON(src, home)
	if out == "" {
		t.Fatal("expected non-empty result path for rewritable file")
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	if strings.Contains(string(data), home) {
		t.Errorf("rewritten file still contains host home %q:\n%s", home, data)
	}
	if !strings.Contains(string(data), "/home/yolo/.claude/plugins/marketplaces/official/lab-notebook") {
		t.Errorf("rewritten file missing /home/yolo path:\n%s", data)
	}
}

func TestPreprocessClaudeConfigJSONSettingsShape(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	src := filepath.Join(home, ".claude", "settings.json")
	content := `{
  "hooks": {
    "PostToolUse": [
      {
        "hooks": [
          {"command": "` + home + `/.claude/lab-notebook/bin/post.sh"}
        ]
      }
    ]
  },
  "model": "claude-opus-4-7"
}`
	writeConfigFile(t, src, content)

	out := preprocessClaudeConfigJSON(src, home)
	if out == "" {
		t.Fatal("expected non-empty result path for rewritable file")
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	if strings.Contains(string(data), home) {
		t.Errorf("rewritten file still contains host home %q:\n%s", home, data)
	}
	if !strings.Contains(string(data), "/home/yolo/.claude/lab-notebook/bin/post.sh") {
		t.Errorf("rewritten file missing /home/yolo command path:\n%s", data)
	}
	if !strings.Contains(string(data), `"claude-opus-4-7"`) {
		t.Errorf("rewritten file dropped non-path fields:\n%s", data)
	}
}

func TestPreprocessClaudeConfigJSONNoOpAlreadyRewritten(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	src := filepath.Join(home, ".claude", "plugins", "known_marketplaces.json")
	content := `{
  "official": {
    "installLocation": "/home/yolo/.claude/plugins/marketplaces/official"
  }
}`
	writeConfigFile(t, src, content)

	out := preprocessClaudeConfigJSON(src, home)
	if out != "" {
		t.Errorf("expected empty result when nothing to rewrite, got %s", out)
	}
}

func TestPreprocessClaudeConfigJSONNoOpForeignPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	src := filepath.Join(home, ".claude", "plugins", "known_marketplaces.json")
	content := `{
  "external": {
    "installLocation": "/opt/some/other/place"
  }
}`
	writeConfigFile(t, src, content)

	out := preprocessClaudeConfigJSON(src, home)
	if out != "" {
		t.Errorf("expected empty result when no entry starts with hostHome, got %s", out)
	}
}

func TestPreprocessClaudeConfigJSONMissingFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	src := filepath.Join(home, ".claude", "plugins", "known_marketplaces.json")
	// Note: do not create the file.

	out := preprocessClaudeConfigJSON(src, home)
	if out != "" {
		t.Errorf("expected empty result when source missing, got %s", out)
	}
}

func TestPreprocessClaudeConfigJSONMalformedJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	src := filepath.Join(home, ".claude", "plugins", "known_marketplaces.json")
	writeConfigFile(t, src, `{"broken": `)

	out := preprocessClaudeConfigJSON(src, home)
	if out != "" {
		t.Errorf("expected empty result when JSON is malformed, got %s", out)
	}
}

func TestPreprocessClaudeConfigJSONPartialRewrite(t *testing.T) {
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
	writeConfigFile(t, src, content)

	out := preprocessClaudeConfigJSON(src, home)
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
