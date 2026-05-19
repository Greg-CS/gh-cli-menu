package commands

import (
	"os/exec"
	"strings"

	"github.com/gregsvieira/gh-gum/internal/gh"
)

// Kind categorizes the command group.
type Kind int

const (
	Issues Kind = iota
	PullRequests
	Repositories
	Workflows
	Releases
	Local
)

func (k Kind) String() string {
	switch k {
	case Issues:
		return "Issues"
	case PullRequests:
		return "Pull Requests"
	case Repositories:
		return "Repositories"
	case Workflows:
		return "Workflows"
	case Releases:
		return "Releases"
	case Local:
		return "Local"
	default:
		return "Unknown"
	}
}

// Action represents a single menu item that can be executed.
type Action struct {
	Label string
	Kind  Kind
	// Cmd returns an *exec.Cmd wired to stdin/stdout/stderr.
	// The TUI suspends while the command runs and resumes after it exits.
	// If repo is non-empty, the command should target that owner/name.
	Cmd func(repo string) *exec.Cmd
}

// Actions returns the flat list of all available menu actions.
func Actions() []Action {
	return []Action{
		{Label: "List open issues", Kind: Issues, Cmd: func(repo string) *exec.Cmd {
			return withRepo(repo, "issue", "list")
		}},
		{Label: "Create new issue", Kind: Issues, Cmd: func(repo string) *exec.Cmd {
			return withRepo(repo, "issue", "create")
		}},
		{Label: "List open PRs", Kind: PullRequests, Cmd: func(repo string) *exec.Cmd {
			return withRepo(repo, "pr", "list")
		}},
		{Label: "Create new PR", Kind: PullRequests, Cmd: func(repo string) *exec.Cmd {
			return withRepo(repo, "pr", "create")
		}},
		{Label: "Checkout a PR", Kind: PullRequests, Cmd: func(repo string) *exec.Cmd {
			return gh.NewCommand("pr", "checkout")
		}},
		{Label: "View repo summary", Kind: Repositories, Cmd: func(repo string) *exec.Cmd {
			return withRepo(repo, "repo", "view")
		}},
		{Label: "Open in browser", Kind: Repositories, Cmd: func(repo string) *exec.Cmd {
			return withRepo(repo, "browse")
		}},
		{Label: "List workflows", Kind: Workflows, Cmd: func(repo string) *exec.Cmd {
			return withRepo(repo, "workflow", "list")
		}},
		{Label: "List workflow runs", Kind: Workflows, Cmd: func(repo string) *exec.Cmd {
			return withRepo(repo, "run", "list")
		}},
		{Label: "View latest release", Kind: Releases, Cmd: func(repo string) *exec.Cmd {
			return withRepo(repo, "release", "view")
		}},
		{Label: "Create new release", Kind: Releases, Cmd: func(repo string) *exec.Cmd {
			return withRepo(repo, "release", "create")
		}},
		{Label: "Push to main", Kind: Local, Cmd: func(repo string) *exec.Cmd {
			return exec.Command("git", "push", "origin", "main")
		}},
		{Label: "Push current branch", Kind: Local, Cmd: func(repo string) *exec.Cmd {
			return exec.Command("git", "push", "origin", "HEAD")
		}},
		{Label: "List branches", Kind: Local, Cmd: func(repo string) *exec.Cmd {
			return exec.Command("git", "branch", "-a", "--color")
		}},
		{Label: "View git log", Kind: Local, Cmd: func(repo string) *exec.Cmd {
			return exec.Command("git", "log", "--oneline", "--graph", "-20", "--decorate", "--color")
		}},
		{Label: "List tags", Kind: Local, Cmd: func(repo string) *exec.Cmd {
			return exec.Command("git", "tag", "-l", "-n1")
		}},
		{Label: "Checkout new branch", Kind: Local, Cmd: func(repo string) *exec.Cmd {
			return exec.Command("git", "checkout", "-b")
		}},
	}
}

// withRepo appends --repo owner/name when repo is non-empty.
func withRepo(repo string, args ...string) *exec.Cmd {
	if repo != "" {
		args = append(args, "--repo", repo)
	}
	return gh.NewCommand(args...)
}

// Grouped returns actions partitioned by Kind for display grouping.
func Grouped() map[Kind][]Action {
	groups := make(map[Kind][]Action)
	for _, a := range Actions() {
		groups[a.Kind] = append(groups[a.Kind], a)
	}
	return groups
}

// Search returns actions whose label contains the query (case-insensitive).
func Search(query string) []Action {
	q := strings.ToLower(query)
	var out []Action
	for _, a := range Actions() {
		if strings.Contains(strings.ToLower(a.Label), q) {
			out = append(out, a)
		}
	}
	return out
}
