package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"golang.org/x/term"
)

type forkState string

const (
	stateRunning forkState = "running"
	stateIdle    forkState = "idle"
)

type forkRow struct {
	Name        string
	Project     string
	Source      string
	State       forkState
	ContainerID string
	ForkDir     string
	Age         time.Time
}

type whoAction string

const (
	actionShell   whoAction = "shell"
	actionStop    whoAction = "stop"
	actionDiscard whoAction = "discard"
	actionCancel  whoAction = "cancel"
)

type whoUI interface {
	PickFork(rows []forkRow, all bool) (*forkRow, error)
	PickAction(row forkRow) (whoAction, error)
	ConfirmDiscard(row forkRow) (bool, error)
}

func runWho(args []string, projectDir string) error {
	fs := flag.NewFlagSet("who", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var all bool
	fs.BoolVar(&all, "all", false, "list every yolobox fork on this host")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printWhoUsage()
			return errHelp
		}
		return err
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("yolobox who requires an interactive terminal")
	}

	rows, err := listForks(projectDir, all)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		if all {
			info("No running yolobox forks found on this host.")
		} else {
			info("No forks found here. Try `yolobox who --all` for machine-wide.")
		}
		return nil
	}

	return whoFlow(rows, all, newHuhWhoUI())
}

func printWhoUsage() {
	fmt.Fprintln(os.Stderr, "USAGE:")
	fmt.Fprintln(os.Stderr, "  yolobox who           Pick from forks of this project")
	fmt.Fprintln(os.Stderr, "  yolobox who --all     Pick from every running yolobox fork on this host")
}

func whoFlow(rows []forkRow, all bool, ui whoUI) error {
	selected, err := ui.PickFork(rows, all)
	if err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return nil
		}
		return err
	}
	if selected == nil {
		return nil
	}
	action, err := ui.PickAction(*selected)
	if err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return nil
		}
		return err
	}
	switch action {
	case actionShell:
		if selected.State != stateRunning {
			return fmt.Errorf("cannot open shell: %s is not running", selected.Name)
		}
		return doShell(*selected)
	case actionStop:
		if selected.State != stateRunning {
			return fmt.Errorf("cannot stop: %s is not running", selected.Name)
		}
		return doStop(*selected)
	case actionDiscard:
		confirm, err := ui.ConfirmDiscard(*selected)
		if err != nil {
			if errors.Is(err, huh.ErrUserAborted) {
				return nil
			}
			return err
		}
		if !confirm {
			return nil
		}
		return doDiscard(*selected)
	case actionCancel:
		return nil
	}
	return nil
}

func listForks(projectDir string, all bool) ([]forkRow, error) {
	if all {
		return listForksAll()
	}
	return listForksProject(projectDir)
}

func listForksProject(projectDir string) ([]forkRow, error) {
	source, err := filepath.Abs(projectDir)
	if err != nil {
		return nil, err
	}
	source, err = filepath.EvalSymlinks(source)
	if err != nil {
		return nil, err
	}
	forkBase := filepath.Join(filepath.Dir(source), ".yolobox-forks", slugify(filepath.Base(source), "folder"))

	entries, err := os.ReadDir(forkBase)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	runtimePath, err := resolveRuntime("")
	if err != nil {
		return nil, err
	}
	running, err := queryRunningForks(runtimePath)
	if err != nil {
		return nil, err
	}

	rows := make([]forkRow, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		forkDir := filepath.Join(forkBase, name)
		info, _ := e.Info()
		row := forkRow{
			Name:    name,
			Project: filepath.Base(source),
			Source:  source,
			State:   stateIdle,
			ForkDir: forkDir,
			Age:     mtimeOr(info, forkDir),
		}
		if c, ok := matchRunning(running, name, source); ok {
			row.State = stateRunning
			row.ContainerID = c.ID
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	return rows, nil
}

func listForksAll() ([]forkRow, error) {
	runtimePath, err := resolveRuntime("")
	if err != nil {
		return nil, err
	}
	running, err := queryRunningForks(runtimePath)
	if err != nil {
		return nil, err
	}
	rows := make([]forkRow, 0, len(running))
	for _, c := range running {
		project := filepath.Base(c.Source)
		row := forkRow{
			Name:        c.Name,
			Project:     project,
			Source:      c.Source,
			State:       stateRunning,
			ContainerID: c.ID,
			Age:         c.Created,
		}
		// Best-effort: locate the fork dir on disk so Discard works.
		if c.Source != "" {
			row.ForkDir = filepath.Join(filepath.Dir(c.Source), ".yolobox-forks", slugify(project, "folder"), c.Name)
			if _, err := os.Stat(row.ForkDir); err != nil {
				row.ForkDir = ""
			}
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Project != rows[j].Project {
			return rows[i].Project < rows[j].Project
		}
		return rows[i].Name < rows[j].Name
	})
	return rows, nil
}

type runningFork struct {
	ID      string
	Name    string
	Source  string
	Created time.Time
}

func queryRunningForks(runtimePath string) ([]runningFork, error) {
	cmd := exec.Command(runtimePath, "ps",
		"--filter", "label=com.yolobox.fork=true",
		"--format", "{{.ID}}\t{{.Label \"com.yolobox.fork.name\"}}\t{{.Label \"com.yolobox.fork.source\"}}\t{{.CreatedAt}}",
	)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("query running forks: %w", err)
	}
	return parseRunningForks(string(output)), nil
}

func parseRunningForks(output string) []runningFork {
	var forks []runningFork
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 4)
		if len(fields) < 4 {
			continue
		}
		f := runningFork{ID: fields[0], Name: fields[1], Source: fields[2]}
		if t, err := time.Parse("2006-01-02 15:04:05 -0700 MST", fields[3]); err == nil {
			f.Created = t
		}
		forks = append(forks, f)
	}
	return forks
}

func matchRunning(running []runningFork, name, source string) (runningFork, bool) {
	for _, c := range running {
		if c.Name == name && c.Source == source {
			return c, true
		}
	}
	return runningFork{}, false
}

func mtimeOr(info os.FileInfo, path string) time.Time {
	if info != nil {
		return info.ModTime()
	}
	if st, err := os.Stat(path); err == nil {
		return st.ModTime()
	}
	return time.Time{}
}

func doShell(row forkRow) error {
	runtimePath, err := resolveRuntime("")
	if err != nil {
		return err
	}
	cmd := exec.Command(runtimePath, "exec", "-it", row.ContainerID, "bash")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// Fall back to sh if bash is absent.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			shCmd := exec.Command(runtimePath, "exec", "-it", row.ContainerID, "sh")
			shCmd.Stdin = os.Stdin
			shCmd.Stdout = os.Stdout
			shCmd.Stderr = os.Stderr
			return shCmd.Run()
		}
		return err
	}
	return nil
}

func doStop(row forkRow) error {
	runtimePath, err := resolveRuntime("")
	if err != nil {
		return err
	}
	cmd := exec.Command(runtimePath, "stop", row.ContainerID)
	output, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed == "" {
			return fmt.Errorf("stop %s: %w", row.Name, err)
		}
		return fmt.Errorf("stop %s: %s", row.Name, trimmed)
	}
	success("Stopped %s", row.Name)
	return nil
}

func doDiscard(row forkRow) error {
	if row.State == stateRunning && row.ContainerID != "" {
		if err := doStop(row); err != nil {
			warn("Stop failed during discard: %v", err)
		}
	}
	if row.ForkDir == "" {
		return fmt.Errorf("discard %s: fork dir unknown", row.Name)
	}
	if err := os.RemoveAll(row.ForkDir); err != nil {
		return fmt.Errorf("remove fork dir %s: %w", row.ForkDir, err)
	}
	success("Discarded %s", row.Name)
	return nil
}

// huhWhoUI is the production huh-backed implementation of whoUI.
type huhWhoUI struct{}

func newHuhWhoUI() whoUI { return huhWhoUI{} }

func (huhWhoUI) PickFork(rows []forkRow, all bool) (*forkRow, error) {
	options := make([]huh.Option[int], 0, len(rows))
	for i, r := range rows {
		options = append(options, huh.NewOption(formatForkRow(r, all), i))
	}
	var idx int
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[int]().
				Title("Pick a fork").
				Options(options...).
				Value(&idx),
		),
	).WithTheme(yoloboxTheme())
	if err := form.Run(); err != nil {
		return nil, err
	}
	return &rows[idx], nil
}

func (huhWhoUI) PickAction(row forkRow) (whoAction, error) {
	shellOpt := huh.NewOption("Open shell", string(actionShell))
	stopOpt := huh.NewOption("Stop", string(actionStop))
	if row.State != stateRunning {
		shellOpt = huh.NewOption("Open shell (unavailable: not running)", string(actionShell))
		stopOpt = huh.NewOption("Stop (unavailable: not running)", string(actionStop))
	}
	var choice string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Action for " + row.Name).
				Options(
					shellOpt,
					stopOpt,
					huh.NewOption("Discard", string(actionDiscard)),
					huh.NewOption("Cancel", string(actionCancel)),
				).
				Value(&choice),
		),
	).WithTheme(yoloboxTheme())
	if err := form.Run(); err != nil {
		return "", err
	}
	return whoAction(choice), nil
}

func (huhWhoUI) ConfirmDiscard(row forkRow) (bool, error) {
	target := row.ForkDir
	if target == "" {
		target = "(fork dir unknown)"
	}
	var confirm bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Discard " + row.Name + "?").
				Description("Will delete: " + target).
				Affirmative("Discard").
				Negative("Cancel").
				Value(&confirm),
		),
	).WithTheme(yoloboxTheme())
	if err := form.Run(); err != nil {
		return false, err
	}
	return confirm, nil
}

func formatForkRow(r forkRow, all bool) string {
	state := string(r.State)
	age := humanizeAge(r.Age)
	if all {
		return fmt.Sprintf("%-30s  %-20s  %-9s  %s", r.Name, r.Project, state, age)
	}
	return fmt.Sprintf("%-30s  %-9s  %s", r.Name, state, age)
}

func humanizeAge(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("2006-01-02")
	}
}

