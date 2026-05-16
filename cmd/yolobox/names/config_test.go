package names

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAllEmbeddedOnly(t *testing.T) {
	patterns, pools, err := loadAll("")
	if err != nil {
		t.Fatalf("loadAll: %v", err)
	}
	if len(patterns) == 0 {
		t.Fatal("expected default patterns; got none")
	}
	for _, pool := range knownPools {
		if len(pools[pool]) == 0 {
			t.Errorf("pool %q is empty", pool)
		}
	}
}

func TestApplyWordOverrideAppend(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "adjective.json"), `{"mode": "append", "items": ["zesty", "abundant"]}`)
	_, pools, err := loadAll(dir)
	if err != nil {
		t.Fatalf("loadAll: %v", err)
	}
	if !contains(pools["adjective"], "zesty") || !contains(pools["adjective"], "abundant") {
		t.Errorf("expected appended words in pool; got %v...", firstN(pools["adjective"], 5))
	}
	if len(pools["adjective"]) < 900 {
		t.Errorf("append should keep defaults; pool size %d looks too small", len(pools["adjective"]))
	}
}

func TestApplyWordOverrideReplace(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "adjective.json"), `{"mode": "replace", "items": ["zesty", "abundant"]}`)
	_, pools, err := loadAll(dir)
	if err != nil {
		t.Fatalf("loadAll: %v", err)
	}
	if len(pools["adjective"]) != 2 {
		t.Fatalf("expected replace to leave 2 items; got %d", len(pools["adjective"]))
	}
}

func TestApplyWordOverrideMissingModeDefaultsToAppend(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "adjective.json"), `{"items": ["zesty"]}`)
	_, pools, err := loadAll(dir)
	if err != nil {
		t.Fatalf("loadAll: %v", err)
	}
	if !contains(pools["adjective"], "zesty") {
		t.Error("missing mode should default to append; zesty not found")
	}
	if len(pools["adjective"]) < 900 {
		t.Errorf("missing mode should default to append; pool size %d looks too small", len(pools["adjective"]))
	}
}

func TestApplyWordOverrideInvalidMode(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "adjective.json"), `{"mode": "bogus", "items": ["zesty"]}`)
	_, _, err := loadAll(dir)
	if err == nil {
		t.Fatal("expected error for invalid mode; got nil")
	}
	if !strings.Contains(err.Error(), "invalid mode") {
		t.Errorf("error should mention invalid mode; got %v", err)
	}
}

func TestApplyWordOverrideInvalidWord(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "adjective.json"), `{"items": ["Capitalized"]}`)
	_, _, err := loadAll(dir)
	if err == nil {
		t.Fatal("expected error for uppercase word; got nil")
	}
	if !strings.Contains(err.Error(), "Capitalized") {
		t.Errorf("error should name the offending word; got %v", err)
	}
}

func TestApplyWordOverrideMalformedJSON(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "adjective.json"), `{not json}`)
	_, _, err := loadAll(dir)
	if err == nil {
		t.Fatal("expected parse error; got nil")
	}
	if !strings.Contains(err.Error(), "adjective.json") {
		t.Errorf("error should reference file path; got %v", err)
	}
}

func TestApplyPatternOverrideReplace(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "patterns.json"), `{"mode": "replace", "items": [{"pattern": "{adjective}-{noun}"}]}`)
	patterns, _, err := loadAll(dir)
	if err != nil {
		t.Fatalf("loadAll: %v", err)
	}
	if len(patterns) != 1 {
		t.Fatalf("expected 1 pattern after replace; got %d", len(patterns))
	}
}

func TestPatternUnknownSlot(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "patterns.json"), `{"mode": "replace", "items": [{"pattern": "{xyz}-{noun}"}]}`)
	_, _, err := loadAll(dir)
	if err == nil {
		t.Fatal("expected error for unknown slot; got nil")
	}
	if !strings.Contains(err.Error(), "{xyz}") {
		t.Errorf("error should name the unknown slot; got %v", err)
	}
}

func TestPatternNegativeWeight(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "patterns.json"), `{"mode": "append", "items": [{"pattern": "{adjective}-{noun}", "weight": -1}]}`)
	_, _, err := loadAll(dir)
	if err == nil {
		t.Fatal("expected error for negative weight; got nil")
	}
}

func TestOverrideDirRespectsXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-test")
	dir, err := OverrideDir()
	if err != nil {
		t.Fatalf("OverrideDir: %v", err)
	}
	if dir != "/tmp/xdg-test/yolobox/names" {
		t.Errorf("expected XDG-prefixed path; got %q", dir)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func contains(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}

func firstN(slice []string, n int) []string {
	if len(slice) < n {
		return slice
	}
	return slice[:n]
}
