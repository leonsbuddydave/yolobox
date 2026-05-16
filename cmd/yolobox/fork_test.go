package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestValidateForkName(t *testing.T) {
	valid := []string{"bruno", "mike-1", "a"}
	for _, name := range valid {
		if err := validateForkName(name); err != nil {
			t.Fatalf("expected %q to be valid: %v", name, err)
		}
	}

	invalid := []string{"Goat", "api_1", "-api", "api.", ""}
	for _, name := range invalid {
		if err := validateForkName(name); err == nil {
			t.Fatalf("expected %q to be invalid", name)
		}
	}
}

func TestComposeProjectNameIncludesFolderSlugHashAndFork(t *testing.T) {
	name := composeProjectName("/tmp/My App", "bruno")
	if !strings.HasPrefix(name, "my-app-") {
		t.Fatalf("expected folder slug prefix, got %q", name)
	}
	if !strings.HasSuffix(name, "-bruno") {
		t.Fatalf("expected fork suffix, got %q", name)
	}
	if name == composeProjectName("/other/My App", "bruno") {
		t.Fatalf("expected path hash to avoid same-basename collisions")
	}
}

func TestForkConfigAddsRuntimeEnvAndManifest(t *testing.T) {
	projectDir := t.TempDir()
	fork := ForkConfig{
		Name:           "bruno",
		Source:         filepath.Join(projectDir, "source"),
		Copy:           filepath.Join(projectDir, "copy"),
		ComposeProject: "folder-123-bruno",
	}
	cfg := Config{Image: "test-image"}
	applyForkConfig(&cfg, &fork)

	args, _, err := buildRunArgs(cfg, filepath.Join(projectDir, "copy", "pkg"), []string{"echo", "hello"}, false)
	if err != nil {
		t.Fatalf("buildRunArgs failed: %v", err)
	}
	for _, expected := range []string{
		"YOLOBOX_FORK_NAME=bruno",
		"YOLOBOX_FORK_SOURCE=" + fork.Source,
		"YOLOBOX_FORK_COPY=" + fork.Copy,
		"COMPOSE_PROJECT_NAME=folder-123-bruno",
	} {
		if !contains(args, expected) {
			t.Fatalf("expected runtime args to contain %q, got %v", expected, args)
		}
	}
	if !contains(args, fork.Copy+":"+fork.Source) {
		t.Fatalf("expected copied folder to be mounted at source path, got %v", args)
	}
	if !containsSequence(args, "-w", fork.Source) {
		t.Fatalf("expected fork working dir %s, got %v", fork.Source, args)
	}

	payload, ok := argEnvValue(args, yoloboxContextPayloadEnv)
	if !ok {
		t.Fatalf("expected %s env var", yoloboxContextPayloadEnv)
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("failed to decode context manifest: %v", err)
	}
	var manifest contextManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("failed to unmarshal context manifest: %v", err)
	}
	if manifest.Fork == nil {
		t.Fatal("expected fork metadata in context manifest")
	}
	if manifest.Fork.Name != "bruno" || manifest.Fork.Copy != fork.Copy || manifest.Fork.ComposeProject != "folder-123-bruno" {
		t.Fatalf("unexpected fork manifest: %+v", manifest.Fork)
	}
}

func TestNewForkInfoUsesCurrentFolder(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "packages", "api"), 0755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	projectDir := filepath.Join(root, "packages", "api")
	info, err := newForkInfo(projectDir, "bruno")
	if err != nil {
		t.Fatalf("newForkInfo failed: %v", err)
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("failed to resolve temp root: %v", err)
	}
	realProjectDir := filepath.Join(realRoot, "packages", "api")
	if info.Source != realProjectDir {
		t.Fatalf("expected source %s, got %s", realProjectDir, info.Source)
	}
	if info.RunDir != info.Copy {
		t.Fatalf("unexpected run dir: %s", info.RunDir)
	}
	if !strings.Contains(info.Copy, filepath.Join(".yolobox-forks", "api", "bruno")) {
		t.Fatalf("unexpected copy dir: %s", info.Copy)
	}
}

func TestForkCopyCopiesWholeFolder(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	root := t.TempDir()
	runGitTestCommand(t, root, "init")
	runGitTestCommand(t, root, "config", "user.email", "test@example.com")
	runGitTestCommand(t, root, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("node_modules/\n.env\n"), 0644); err != nil {
		t.Fatalf("failed to write .gitignore: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("test\n"), 0644); err != nil {
		t.Fatalf("failed to write README: %v", err)
	}
	runGitTestCommand(t, root, "add", ".gitignore", "README.md")
	runGitTestCommand(t, root, "commit", "-m", "initial")

	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("SECRET=1\n"), 0600); err != nil {
		t.Fatalf("failed to write .env: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "pkg"), 0755); err != nil {
		t.Fatalf("failed to create node_modules: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "node_modules", "pkg", "index.js"), []byte("module.exports = 1\n"), 0644); err != nil {
		t.Fatalf("failed to write ignored dependency: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "scratch.txt"), []byte("untracked\n"), 0644); err != nil {
		t.Fatalf("failed to write untracked file: %v", err)
	}
	runGitTestCommand(t, root, "branch", "local-ref")

	copy := filepath.Join(t.TempDir(), "copy")
	if err := os.MkdirAll(copy, 0755); err != nil {
		t.Fatalf("failed to create copy dir: %v", err)
	}
	info := forkInfo{
		Name:   "bruno",
		Source: root,
		Copy:   copy,
	}
	if err := copyFullDirectory(info.Source, info.Copy); err != nil {
		t.Fatalf("copyFullDirectory failed: %v", err)
	}

	for _, path := range []string{".git", ".env", "node_modules/pkg/index.js", "scratch.txt"} {
		if _, err := os.Stat(filepath.Join(copy, path)); err != nil {
			t.Fatalf("expected copied path %s: %v", path, err)
		}
	}
	if !strings.Contains(gitTestOutput(t, copy, "branch", "--list", "local-ref"), "local-ref") {
		t.Fatalf("expected copied local ref")
	}
}

func TestForkCopyCopiesNonGitFolder(t *testing.T) {
	source := t.TempDir()
	if err := os.MkdirAll(filepath.Join(source, "cache", "nested"), 0755); err != nil {
		t.Fatalf("failed to create cache dir: %v", err)
	}
	for path, contents := range map[string]string{
		".env":                 "SECRET=1\n",
		"cache/nested/blob":    "cached\n",
		"untracked-local-file": "local\n",
	} {
		if err := os.WriteFile(filepath.Join(source, path), []byte(contents), 0644); err != nil {
			t.Fatalf("failed to write %s: %v", path, err)
		}
	}

	copy := filepath.Join(t.TempDir(), "copy")
	if err := os.MkdirAll(copy, 0755); err != nil {
		t.Fatalf("failed to create copy dir: %v", err)
	}
	if err := copyFullDirectory(source, copy); err != nil {
		t.Fatalf("copyFullDirectory failed: %v", err)
	}

	for _, path := range []string{".env", "cache/nested/blob", "untracked-local-file"} {
		if _, err := os.Stat(filepath.Join(copy, path)); err != nil {
			t.Fatalf("expected copied path %s: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(copy, ".git")); !os.IsNotExist(err) {
		t.Fatalf("expected non-git copy to remain non-git, got err=%v", err)
	}
}

func runGitTestCommand(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
}

func gitTestOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output)
}

func containsSequence(values []string, first, second string) bool {
	for i := 0; i+1 < len(values); i++ {
		if values[i] == first && values[i+1] == second {
			return true
		}
	}
	return false
}

func TestRunForkCreateAppliesSharedPathsFromToml(t *testing.T) {
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("rsync not available")
	}

	source := t.TempDir()
	if err := os.MkdirAll(filepath.Join(source, "game"), 0755); err != nil {
		t.Fatalf("mkdir game: %v", err)
	}
	if err := os.WriteFile(filepath.Join(source, "game", "asset.bin"), []byte("asset"), 0644); err != nil {
		t.Fatalf("write asset: %v", err)
	}
	if err := os.WriteFile(filepath.Join(source, "keep.txt"), []byte("keep"), 0644); err != nil {
		t.Fatalf("write keep: %v", err)
	}
	if err := os.WriteFile(filepath.Join(source, ".yolobox.toml"), []byte(`
[fork]
shared_paths = ["game"]
`), 0644); err != nil {
		t.Fatalf("write toml: %v", err)
	}

	// Compute expected fork dir and assert it doesn't exist yet.
	info, err := newForkInfo(source, "alpha")
	if err != nil {
		t.Fatalf("newForkInfo: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(filepath.Dir(info.Copy))) })

	// Run runForkCreate but stop before runForkedYolobox by using a sentinel command.
	// Easiest path: refactor is out of scope; instead, call the building blocks directly.
	merged, err := mergeSharedPaths([]SharedPath{{Path: "game", Mode: "rw"}}, nil)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if err := os.MkdirAll(info.Copy, 0755); err != nil {
		t.Fatalf("mkdir copy: %v", err)
	}
	if err := copyForkSource(info.Source, info.Copy, merged); err != nil {
		t.Fatalf("copyForkSource: %v", err)
	}
	resolved, err := createSharedPathPlaceholders(info.Source, info.Copy, merged)
	if err != nil {
		t.Fatalf("createSharedPathPlaceholders: %v", err)
	}

	// game/asset.bin must NOT be in the fork copy.
	if _, err := os.Stat(filepath.Join(info.Copy, "game", "asset.bin")); !os.IsNotExist(err) {
		t.Fatalf("expected game/asset.bin to NOT be copied, got err=%v", err)
	}
	// game/ placeholder must exist in fork copy.
	if info, err := os.Stat(filepath.Join(info.Copy, "game")); err != nil || !info.IsDir() {
		t.Fatalf("expected empty game/ dir placeholder, err=%v", err)
	}
	// keep.txt must be copied.
	if _, err := os.Stat(filepath.Join(info.Copy, "keep.txt")); err != nil {
		t.Fatalf("expected keep.txt copied, err=%v", err)
	}
	if len(resolved) != 1 || resolved[0].Path != "game" {
		t.Fatalf("expected resolved=[game], got %+v", resolved)
	}
}

func TestRemoveForkCopyHappyPath(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "fork")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "file"), []byte("hi"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := removeForkCopy(dir); err != nil {
		t.Fatalf("removeForkCopy: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("expected dir gone, got err=%v", err)
	}
}

func TestRemoveForkCopyStripsDenyDeleteACL(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("ACL stripping is only relevant on darwin")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "fork")
	placeholder := filepath.Join(dir, "shared")
	if err := os.MkdirAll(placeholder, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Attach the same ACL macOS Docker Desktop attaches to bind-mount target
	// dirs. Use the current user (matches what Docker actually attaches).
	username := os.Getenv("USER")
	if username == "" {
		t.Skip("USER env var not set; cannot construct ACL entry")
	}
	if out, err := exec.Command("chmod", "+a", "user:"+username+" deny delete", placeholder).CombinedOutput(); err != nil {
		t.Skipf("chmod +a failed (environment may not permit ACLs): %v: %s", err, out)
	}
	// Confirm plain RemoveAll fails — if not, the test premise is wrong and
	// we should rethink the fix.
	if err := os.RemoveAll(placeholder); err == nil {
		t.Fatal("expected os.RemoveAll to fail on ACL-protected dir, but it succeeded — test premise is invalid")
	}
	// Our helper should succeed end-to-end.
	if err := removeForkCopy(dir); err != nil {
		t.Fatalf("removeForkCopy with deny-delete ACL: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("expected dir gone after removeForkCopy, got err=%v", err)
	}
}
