package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestSharedPathTomlDecodeStringForm(t *testing.T) {
	var cfg Config
	in := `
[fork]
shared_paths = ["game", "third_party"]
`
	if _, err := toml.Decode(in, &cfg); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(cfg.Fork.SharedPaths) != 2 {
		t.Fatalf("expected 2 shared paths, got %d", len(cfg.Fork.SharedPaths))
	}
	if cfg.Fork.SharedPaths[0].Path != "game" || cfg.Fork.SharedPaths[0].Mode != "rw" {
		t.Fatalf("expected {game, rw}, got %+v", cfg.Fork.SharedPaths[0])
	}
	if cfg.Fork.SharedPaths[1].Path != "third_party" || cfg.Fork.SharedPaths[1].Mode != "rw" {
		t.Fatalf("expected {third_party, rw}, got %+v", cfg.Fork.SharedPaths[1])
	}
}

func TestSharedPathTomlDecodeTableForm(t *testing.T) {
	var cfg Config
	in := `
[fork]
shared_paths = [
  { path = "game", mode = "rw" },
  { path = "vendored", mode = "ro" },
]
`
	if _, err := toml.Decode(in, &cfg); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if cfg.Fork.SharedPaths[0].Mode != "rw" || cfg.Fork.SharedPaths[1].Mode != "ro" {
		t.Fatalf("unexpected modes: %+v", cfg.Fork.SharedPaths)
	}
}

func TestSharedPathTomlDecodeMixedForm(t *testing.T) {
	var cfg Config
	in := `
[fork]
shared_paths = [
  "game",
  { path = "vendored", mode = "ro" },
]
`
	if _, err := toml.Decode(in, &cfg); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if cfg.Fork.SharedPaths[0].Path != "game" || cfg.Fork.SharedPaths[0].Mode != "rw" {
		t.Fatalf("string entry wrong: %+v", cfg.Fork.SharedPaths[0])
	}
	if cfg.Fork.SharedPaths[1].Path != "vendored" || cfg.Fork.SharedPaths[1].Mode != "ro" {
		t.Fatalf("table entry wrong: %+v", cfg.Fork.SharedPaths[1])
	}
}

func TestSharedPathTomlRejectsInvalidMode(t *testing.T) {
	var cfg Config
	in := `
[fork]
shared_paths = [{ path = "game", mode = "rwx" }]
`
	_, err := toml.Decode(in, &cfg)
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}
	if !strings.Contains(err.Error(), "mode") {
		t.Fatalf("error should mention mode, got: %v", err)
	}
}

func TestSharedPathTomlRejectsEmptyPath(t *testing.T) {
	var cfg Config
	in := `
[fork]
shared_paths = [""]
`
	_, err := toml.Decode(in, &cfg)
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestLoadConfigReturnsSharedPaths(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	toml := `
[fork]
shared_paths = [
  "game",
  { path = "vendored", mode = "ro" },
]
`
	if err := os.WriteFile(filepath.Join(dir, ".yolobox.toml"), []byte(toml), 0644); err != nil {
		t.Fatalf("write toml: %v", err)
	}
	cfg, err := loadConfig(dir)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if len(cfg.Fork.SharedPaths) != 2 {
		t.Fatalf("expected 2 shared paths after loadConfig, got %d: %+v", len(cfg.Fork.SharedPaths), cfg.Fork.SharedPaths)
	}
	if cfg.Fork.SharedPaths[0].Path != "game" || cfg.Fork.SharedPaths[0].Mode != "rw" {
		t.Fatalf("first entry wrong: %+v", cfg.Fork.SharedPaths[0])
	}
	if cfg.Fork.SharedPaths[1].Path != "vendored" || cfg.Fork.SharedPaths[1].Mode != "ro" {
		t.Fatalf("second entry wrong: %+v", cfg.Fork.SharedPaths[1])
	}
}

func TestValidateSharedPathsAcceptsValid(t *testing.T) {
	cases := [][]SharedPath{
		{{Path: "game", Mode: "rw"}},
		{{Path: "vendored", Mode: "ro"}, {Path: "node_modules", Mode: "rw"}},
		{{Path: "dir/sub", Mode: "rw"}},                  // nested ok
		{{Path: ".env", Mode: "rw"}},                     // hidden file ok
	}
	for _, sp := range cases {
		if err := validateSharedPaths(sp); err != nil {
			t.Fatalf("expected %v to be valid: %v", sp, err)
		}
	}
}

func TestValidateSharedPathsRejectsAbsolute(t *testing.T) {
	err := validateSharedPaths([]SharedPath{{Path: "/etc", Mode: "rw"}})
	if err == nil {
		t.Fatal("expected absolute path rejection")
	}
}

func TestValidateSharedPathsRejectsParentEscape(t *testing.T) {
	cases := []string{"..", "../foo", "foo/../bar"}
	for _, p := range cases {
		if err := validateSharedPaths([]SharedPath{{Path: p, Mode: "rw"}}); err == nil {
			t.Fatalf("expected rejection for %q", p)
		}
	}
}

func TestValidateSharedPathsRejectsBadMode(t *testing.T) {
	err := validateSharedPaths([]SharedPath{{Path: "game", Mode: "yes"}})
	if err == nil {
		t.Fatal("expected mode rejection")
	}
}

func TestValidateSharedPathsRejectsNestedOverlap(t *testing.T) {
	err := validateSharedPaths([]SharedPath{
		{Path: "game", Mode: "rw"},
		{Path: "game/decompile", Mode: "ro"},
	})
	if err == nil {
		t.Fatal("expected nested-path rejection")
	}
	if !strings.Contains(err.Error(), "game/decompile") || !strings.Contains(err.Error(), "game") {
		t.Fatalf("error should name both paths, got: %v", err)
	}
}

func TestValidateSharedPathsRejectsDuplicate(t *testing.T) {
	err := validateSharedPaths([]SharedPath{
		{Path: "game", Mode: "rw"},
		{Path: "game", Mode: "ro"},
	})
	if err == nil {
		t.Fatal("expected duplicate-path rejection")
	}
}

func TestParseShareFlag(t *testing.T) {
	cases := []struct {
		in    string
		want  SharedPath
	}{
		{"game", SharedPath{Path: "game", Mode: "rw"}},
		{"game:rw", SharedPath{Path: "game", Mode: "rw"}},
		{"game:ro", SharedPath{Path: "game", Mode: "ro"}},
		{"sub/dir", SharedPath{Path: "sub/dir", Mode: "rw"}},
		{"sub/dir:ro", SharedPath{Path: "sub/dir", Mode: "ro"}},
	}
	for _, c := range cases {
		got, err := parseShareFlag(c.in)
		if err != nil {
			t.Fatalf("%q: unexpected error: %v", c.in, err)
		}
		if got != c.want {
			t.Fatalf("%q: want %+v, got %+v", c.in, c.want, got)
		}
	}
}

func TestParseShareFlagRejectsBadMode(t *testing.T) {
	if _, err := parseShareFlag("game:yes"); err == nil {
		t.Fatal("expected error for invalid mode")
	}
}

func TestParseShareFlagRejectsEmpty(t *testing.T) {
	if _, err := parseShareFlag(""); err == nil {
		t.Fatal("expected error for empty value")
	}
	if _, err := parseShareFlag(":ro"); err == nil {
		t.Fatal("expected error for empty path before colon")
	}
}

func TestParseShareFlagRejectsTooManyColons(t *testing.T) {
	if _, err := parseShareFlag("a:b:c"); err == nil {
		t.Fatal("expected error for too many colons")
	}
}

func TestMergeSharedPathsTomlOnly(t *testing.T) {
	merged, err := mergeSharedPaths(
		[]SharedPath{{Path: "game", Mode: "ro"}},
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(merged) != 1 || merged[0].Path != "game" || merged[0].Mode != "ro" {
		t.Fatalf("unexpected merged: %+v", merged)
	}
}

func TestMergeSharedPathsCliOverridesTomlMode(t *testing.T) {
	merged, err := mergeSharedPaths(
		[]SharedPath{{Path: "game", Mode: "ro"}},
		[]SharedPath{{Path: "game", Mode: "rw"}},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(merged) != 1 {
		t.Fatalf("expected 1 entry after merge, got %d", len(merged))
	}
	if merged[0].Mode != "rw" {
		t.Fatalf("CLI should win: got %+v", merged[0])
	}
}

func TestMergeSharedPathsAppendsDistinct(t *testing.T) {
	merged, err := mergeSharedPaths(
		[]SharedPath{{Path: "game", Mode: "rw"}},
		[]SharedPath{{Path: "vendored", Mode: "ro"}},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(merged) != 2 {
		t.Fatalf("expected 2 entries, got %+v", merged)
	}
}

func TestMergeSharedPathsRunsOverlapCheck(t *testing.T) {
	_, err := mergeSharedPaths(
		[]SharedPath{{Path: "game", Mode: "rw"}},
		[]SharedPath{{Path: "game/decompile", Mode: "ro"}},
	)
	if err == nil {
		t.Fatal("expected nested-path error after merge")
	}
}

func TestMergeSharedPathsCliDuplicateDeduplicated(t *testing.T) {
	merged, err := mergeSharedPaths(
		nil,
		[]SharedPath{
			{Path: "game", Mode: "ro"},
			{Path: "game", Mode: "rw"},
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(merged) != 1 || merged[0].Mode != "rw" {
		t.Fatalf("expected last CLI entry to win: %+v", merged)
	}
}
