package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunForkCreateRejectsBothNameAndRandom(t *testing.T) {
	err := runForkCreate([]string{"--name", "bruno", "--random", "claude"}, t.TempDir())
	if err == nil {
		t.Fatal("expected error when both --name and --random given")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error should mention mutual exclusion; got %v", err)
	}
}

func TestRunForkCreateRequiresNameOrRandom(t *testing.T) {
	err := runForkCreate([]string{"claude"}, t.TempDir())
	if err == nil {
		t.Fatal("expected error when neither --name nor --random given")
	}
	if !strings.Contains(err.Error(), "--name or --random") {
		t.Errorf("error should suggest --name or --random; got %v", err)
	}
}

func TestParseRunningForks(t *testing.T) {
	out := strings.Join([]string{
		"abc123\tbruno\t/Users/me/proj-a\t2026-05-16 10:00:00 +0000 UTC",
		"def456\tfat-bastard\t/Users/me/proj-b\t2026-05-16 11:30:00 +0000 UTC",
		"",
	}, "\n")
	got := parseRunningForks(out)
	if len(got) != 2 {
		t.Fatalf("expected 2 forks; got %d", len(got))
	}
	if got[0].ID != "abc123" || got[0].Name != "bruno" || got[0].Source != "/Users/me/proj-a" {
		t.Errorf("first fork wrong: %+v", got[0])
	}
	if got[1].Name != "fat-bastard" {
		t.Errorf("second fork wrong: %+v", got[1])
	}
	if got[0].Created.IsZero() {
		t.Errorf("expected parsed CreatedAt; got zero time")
	}
}

func TestParseRunningForksSkipsMalformedLines(t *testing.T) {
	out := "only-three\tfields\there\nabc\tbruno\t/proj\t2026-05-16 10:00:00 +0000 UTC\n"
	got := parseRunningForks(out)
	if len(got) != 1 {
		t.Fatalf("expected 1 valid fork; got %d", len(got))
	}
}

func TestMatchRunning(t *testing.T) {
	forks := []runningFork{
		{ID: "1", Name: "bruno", Source: "/proj-a"},
		{ID: "2", Name: "bruno", Source: "/proj-b"},
	}
	c, ok := matchRunning(forks, "bruno", "/proj-b")
	if !ok || c.ID != "2" {
		t.Errorf("expected match on /proj-b → ID 2; got ok=%v c=%+v", ok, c)
	}
	if _, ok := matchRunning(forks, "bruno", "/proj-c"); ok {
		t.Errorf("expected no match for /proj-c")
	}
}

// stubWhoUI lets tests script the picker/action/confirm responses.
type stubWhoUI struct {
	pickFork       func([]forkRow, bool) (*forkRow, error)
	pickAction     func(forkRow) (whoAction, error)
	confirmDiscard func(forkRow) (bool, error)
}

func (s stubWhoUI) PickFork(rows []forkRow, all bool) (*forkRow, error) {
	return s.pickFork(rows, all)
}
func (s stubWhoUI) PickAction(r forkRow) (whoAction, error)   { return s.pickAction(r) }
func (s stubWhoUI) ConfirmDiscard(r forkRow) (bool, error)    { return s.confirmDiscard(r) }

func TestWhoFlowCancelAtFork(t *testing.T) {
	called := false
	ui := stubWhoUI{
		pickFork:   func([]forkRow, bool) (*forkRow, error) { return nil, nil },
		pickAction: func(forkRow) (whoAction, error) { called = true; return actionCancel, nil },
	}
	err := whoFlow([]forkRow{{Name: "bruno"}}, false, ui)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("pickAction should not be called when no fork selected")
	}
}

func TestWhoFlowCancelAction(t *testing.T) {
	ui := stubWhoUI{
		pickFork:   func(rows []forkRow, _ bool) (*forkRow, error) { return &rows[0], nil },
		pickAction: func(forkRow) (whoAction, error) { return actionCancel, nil },
	}
	err := whoFlow([]forkRow{{Name: "bruno"}}, false, ui)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWhoFlowShellRequiresRunning(t *testing.T) {
	ui := stubWhoUI{
		pickFork:   func(rows []forkRow, _ bool) (*forkRow, error) { return &rows[0], nil },
		pickAction: func(forkRow) (whoAction, error) { return actionShell, nil },
	}
	err := whoFlow([]forkRow{{Name: "bruno", State: stateIdle}}, false, ui)
	if err == nil {
		t.Fatal("expected error when opening shell on idle fork")
	}
	if !strings.Contains(err.Error(), "not running") {
		t.Errorf("error should mention not running; got %v", err)
	}
}

func TestWhoFlowDiscardConfirmDenied(t *testing.T) {
	confirmed := false
	ui := stubWhoUI{
		pickFork:       func(rows []forkRow, _ bool) (*forkRow, error) { return &rows[0], nil },
		pickAction:     func(forkRow) (whoAction, error) { return actionDiscard, nil },
		confirmDiscard: func(forkRow) (bool, error) { confirmed = true; return false, nil },
	}
	err := whoFlow([]forkRow{{Name: "bruno", State: stateIdle, ForkDir: "/tmp/nonexistent-test-fork-dir-12345"}}, false, ui)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !confirmed {
		t.Error("expected confirm to be called")
	}
}

func TestWhoFlowDiscardConfirmedRemovesDir(t *testing.T) {
	dir := t.TempDir()
	forkDir := filepath.Join(dir, "bruno")
	if err := os.MkdirAll(forkDir, 0755); err != nil {
		t.Fatal(err)
	}
	ui := stubWhoUI{
		pickFork:       func(rows []forkRow, _ bool) (*forkRow, error) { return &rows[0], nil },
		pickAction:     func(forkRow) (whoAction, error) { return actionDiscard, nil },
		confirmDiscard: func(forkRow) (bool, error) { return true, nil },
	}
	err := whoFlow([]forkRow{{Name: "bruno", State: stateIdle, ForkDir: forkDir}}, false, ui)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(forkDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected fork dir to be gone; stat err=%v", err)
	}
}

func TestFormatForkRowProjectColumnOnlyWhenAll(t *testing.T) {
	row := forkRow{Name: "bruno", Project: "myrepo", State: stateRunning}
	if strings.Contains(formatForkRow(row, false), "myrepo") {
		t.Error("project column should not appear in default scope")
	}
	if !strings.Contains(formatForkRow(row, true), "myrepo") {
		t.Error("project column should appear in --all scope")
	}
}

func TestForkRunLabelsAreAddedInBuildRunArgs(t *testing.T) {
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
		"com.yolobox.fork=true",
		"com.yolobox.fork.name=bruno",
		"com.yolobox.fork.source=" + fork.Source,
		"com.yolobox.fork.compose_project=folder-123-bruno",
	} {
		if !contains(args, expected) {
			t.Errorf("expected --label %q in run args; got %v", expected, args)
		}
	}
}
