package gh

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Runner abstracts command execution so tests can inject fakes.
type Runner interface {
	Run(name string, args ...string) (string, error)
}

// LiveRunner executes real gh CLI commands.
type LiveRunner struct{}

func (LiveRunner) Run(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return strings.TrimSpace(out.String()), nil
}

var DefaultRunner Runner = LiveRunner{}

// Run executes a gh command with the given args and returns stdout.
func Run(args ...string) (string, error) {
	return DefaultRunner.Run("gh", args...)
}

// MustRun executes a gh command and prints its output directly to stdout/stderr.
// Use this when the command itself has its own interactive TUI (e.g. gh pr create).
func MustRun(args ...string) error {
	return NewCommand(args...).Run()
}

// NewCommand returns an *exec.Cmd for "gh <args...>" wired to the current tty.
// Use with tea.ExecProcess to run interactive gh commands while suspending the TUI.
func NewCommand(args ...string) *exec.Cmd {
	cmd := exec.Command("gh", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}

// repoEntry matches the JSON output of `gh repo list --json nameWithOwner`.
type repoEntry struct {
	NameWithOwner string `json:"nameWithOwner"`
}

// DetectCurrentRepo tries to read the current repo from git remotes via gh.
func DetectCurrentRepo() (string, error) {
	out, err := DefaultRunner.Run("gh", "repo", "view", "--json", "nameWithOwner", "--jq", ".nameWithOwner")
	if err != nil {
		return "", err
	}
	return out, nil
}

// ListMyRepos returns up to limit repos owned by the authenticated user.
func ListMyRepos(limit int) ([]string, error) {
	if limit <= 0 {
		limit = 30
	}
	out, err := DefaultRunner.Run("gh", "repo", "list", "--json", "nameWithOwner", "--limit", fmt.Sprintf("%d", limit))
	if err != nil {
		return nil, err
	}
	var entries []repoEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		return nil, fmt.Errorf("parsing repo list: %w", err)
	}
	repos := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.NameWithOwner != "" {
			repos = append(repos, e.NameWithOwner)
		}
	}
	return repos, nil
}
