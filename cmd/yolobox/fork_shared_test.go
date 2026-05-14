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
