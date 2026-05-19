package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/gregsvieira/gh-gum/internal/commands"
	"github.com/gregsvieira/gh-gum/internal/gh"
	"github.com/gregsvieira/gh-gum/internal/ui"
)

type screen int

const (
	screenLoading screen = iota
	screenRepoSelector
	screenManualRepo
	screenMainMenu
	screenBranchInput
	screenCommitInput
	screenFetchRepoList
	screenFetchCommitList
	screenFetchFileList
	screenOutput
)

// ---------- command list items ----------

type cmdItem struct{ action commands.Action }

func (i cmdItem) FilterValue() string { return i.action.Label }

type cmdDelegate struct{ repo string }

func (d cmdDelegate) Height() int                             { return 1 }
func (d cmdDelegate) Spacing() int                            { return 0 }
func (d cmdDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d cmdDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(cmdItem)
	if !ok {
		return
	}
	str := fmt.Sprintf("%s  %s", ui.MenuItemStyle.Render(i.action.Kind.String()), i.action.Label)
	if index == m.Index() {
		str = ui.SelectedMenuItemStyle.Render(str)
	}
	fmt.Fprint(w, str)
}

// ---------- home screen items (repos + global actions) ----------

type homeItem struct {
	label  string
	isRepo bool
	repo   string
	cmd    func() *exec.Cmd
}

func (i homeItem) FilterValue() string { return i.label }

type homeDelegate struct{}

func (d homeDelegate) Height() int                             { return 1 }
func (d homeDelegate) Spacing() int                            { return 0 }
func (d homeDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d homeDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(homeItem)
	if !ok {
		return
	}
	var str string
	if i.isRepo {
		str = ui.MenuItemStyle.Render(i.label)
	} else {
		str = ui.HelpKeyStyle.Render("+ ") + lipgloss.NewStyle().Foreground(ui.Secondary).Render(i.label)
	}
	if index == m.Index() {
		str = ui.SelectedMenuItemStyle.Render(str)
	}
	fmt.Fprint(w, str)
}

// ---------- messages ----------

type reposLoadedMsg struct {
	repos []string
	err   error
}

type execFinishedMsg struct{ err error }

type cmdOutputMsg struct {
	output string
	err    error
	label  string
}

type prePushStatusMsg struct {
	hasChanges bool
}

type commitPushMsg struct {
	output string
	err    error
}

type checksPassedMsg struct {
	output string
	err    error
}

type commitsLoadedMsg struct {
	commits []string
	err     error
}

type filesLoadedMsg struct {
	files []string
	err   error
}

// ---------- model ----------

type AppModel struct {
	screen      screen
	repo        string
	repoList    list.Model
	cmdList     list.Model
	pickerList  list.Model
	textInput   textinput.Model
	outputVP    viewport.Model
	outputTitle string
	width       int
	height      int
	quitting    bool
	lastErr     string
	// fetchState holds inputs for the "fetch file/folder from commit" flow.
	fetchSourceRepo string
	fetchCommit     string
	fetchPath       string
	fetchTempDir    string
	fetchFlowActive bool
	// smart push state.
	pendingSmartPush bool
	// loading indicator.
	loading     bool
	loadingText string
	spinner     spinner.Model
}

func NewAppModel() AppModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = ui.SubtitleStyle
	return AppModel{
		screen:  screenLoading,
		spinner: s,
	}
}

func newTextInput() textinput.Model {
	ti := textinput.New()
	ti.Placeholder = "owner/repo"
	ti.Focus()
	ti.CharLimit = 60
	ti.Width = 40
	return ti
}

func newBranchInput() textinput.Model {
	ti := textinput.New()
	ti.Placeholder = "feature/name"
	ti.Focus()
	ti.CharLimit = 60
	ti.Width = 40
	return ti
}

func newCommitInput() textinput.Model {
	ti := textinput.New()
	ti.Placeholder = "Your commit message"
	ti.Focus()
	ti.CharLimit = 120
	ti.Width = 50
	return ti
}

func (m AppModel) Init() tea.Cmd {
	return tea.Batch(detectRepoCmd(), m.spinner.Tick)
}

// detectRepoCmd tries the current directory first; if that fails it fetches the user's repos.
func detectRepoCmd() tea.Cmd {
	return func() tea.Msg {
		repo, err := gh.DetectCurrentRepo()
		if err == nil && repo != "" {
			return reposLoadedMsg{repos: []string{repo}, err: nil}
		}
		repos, err := gh.ListMyRepos(30)
		return reposLoadedMsg{repos: repos, err: err}
	}
}

// listAllReposCmd fetches every repo the user owns.
func listAllReposCmd() tea.Cmd {
	return func() tea.Msg {
		repos, err := gh.ListMyRepos(30)
		return reposLoadedMsg{repos: repos, err: err}
	}
}

func gitStatusCheckCmd() tea.Cmd {
	return func() tea.Msg {
		out, err := exec.Command("git", "status", "--short").CombinedOutput()
		if err != nil {
			// git status may error in non-git dirs; treat as no changes.
			return prePushStatusMsg{hasChanges: false}
		}
		return prePushStatusMsg{hasChanges: strings.TrimSpace(string(out)) != ""}
	}
}

func commitAndPushCmd(msg string) tea.Cmd {
	return func() tea.Msg {
		var out strings.Builder
		// Stage all changes.
		addOut, err := exec.Command("git", "add", ".").CombinedOutput()
		out.Write(addOut)
		if err != nil {
			return commitPushMsg{output: out.String(), err: err}
		}
		// Commit.
		commitOut, err := exec.Command("git", "commit", "-m", msg).CombinedOutput()
		out.Write(commitOut)
		if err != nil {
			return commitPushMsg{output: out.String(), err: err}
		}
		// Push.
		pushOut, err := exec.Command("git", "push", "origin", "HEAD").CombinedOutput()
		out.Write(pushOut)
		return commitPushMsg{output: out.String(), err: err}
	}
}

type projectChecks struct {
	buildCmd *exec.Cmd
	testCmd  *exec.Cmd
}

func hasNPMScript(name string) bool {
	data, err := os.ReadFile("package.json")
	if err != nil {
		return false
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return false
	}
	_, ok := pkg.Scripts[name]
	return ok
}

func detectProjectChecks() *projectChecks {
	if _, err := os.Stat("package.json"); err == nil {
		var buildCmd, testCmd *exec.Cmd
		if hasNPMScript("build") {
			buildCmd = exec.Command("npm", "run", "build")
		}
		if hasNPMScript("test") {
			testCmd = exec.Command("npm", "test")
		}
		if buildCmd != nil || testCmd != nil {
			return &projectChecks{buildCmd: buildCmd, testCmd: testCmd}
		}
		return nil
	}
	if _, err := os.Stat("go.mod"); err == nil {
		return &projectChecks{
			buildCmd: exec.Command("go", "build", "./..."),
			testCmd:  exec.Command("go", "test", "./..."),
		}
	}
	if _, err := os.Stat("Cargo.toml"); err == nil {
		return &projectChecks{
			buildCmd: exec.Command("cargo", "build"),
			testCmd:  exec.Command("cargo", "test"),
		}
	}
	return nil
}

func runChecksCmd() tea.Cmd {
	return func() tea.Msg {
		var out strings.Builder
		checks := detectProjectChecks()
		if checks == nil {
			return checksPassedMsg{output: "No build/test checks detected for this project type.", err: nil}
		}
		if checks.buildCmd != nil {
			buildOut, err := checks.buildCmd.CombinedOutput()
			out.WriteString(fmt.Sprintf("=== Build ===\n%s\n", string(buildOut)))
			if err != nil {
				return checksPassedMsg{output: out.String(), err: fmt.Errorf("build failed: %w", err)}
			}
		}
		if checks.testCmd != nil {
			testOut, err := checks.testCmd.CombinedOutput()
			out.WriteString(fmt.Sprintf("=== Tests ===\n%s\n", string(testOut)))
			if err != nil {
				return checksPassedMsg{output: out.String(), err: fmt.Errorf("tests failed: %w", err)}
			}
		}
		out.WriteString("All checks passed.\n")
		return checksPassedMsg{output: out.String(), err: nil}
	}
}

func loadCommitsCmd(tmpDir string) tea.Cmd {
	return func() tea.Msg {
		out, err := exec.Command("git", "-C", tmpDir, "log", "--oneline", "--all", "-n", "100").CombinedOutput()
		if err != nil {
			return commitsLoadedMsg{commits: nil, err: err}
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		var commits []string
		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				commits = append(commits, line)
			}
		}
		return commitsLoadedMsg{commits: commits, err: nil}
	}
}

func loadFilesCmd(tmpDir, commit string) tea.Cmd {
	return func() tea.Msg {
		out, err := exec.Command("git", "-C", tmpDir, "ls-tree", "-r", "--name-only", commit).CombinedOutput()
		if err != nil {
			return filesLoadedMsg{files: nil, err: err}
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		var files []string
		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				files = append(files, line)
			}
		}
		return filesLoadedMsg{files: files, err: nil}
	}
}

func fetchFromCommitCmd(sourceRepo, commit, path string) tea.Cmd {
	return func() tea.Msg {
		var out strings.Builder
		// Create temp directory.
		tmpDir, err := os.MkdirTemp("", "gh-gum-fetch-*")
		if err != nil {
			return cmdOutputMsg{output: out.String(), err: err, label: "Fetch from commit"}
		}
		defer os.RemoveAll(tmpDir)

		// Clone source repo.
		cloneOut, err := exec.Command("gh", "repo", "clone", sourceRepo, tmpDir).CombinedOutput()
		out.Write(cloneOut)
		if err != nil {
			return cmdOutputMsg{output: out.String(), err: err, label: "Fetch from commit"}
		}

		// Extract the specific path from the commit.
		checkoutOut, err := exec.Command("git", "-C", tmpDir, "checkout", commit, "--", path).CombinedOutput()
		out.Write(checkoutOut)
		if err != nil {
			return cmdOutputMsg{output: out.String(), err: err, label: "Fetch from commit"}
		}

		// Copy path to current working directory.
		srcPath := filepath.Join(tmpDir, path)
		dstPath := filepath.Base(path)

		info, err := os.Stat(srcPath)
		if err != nil {
			return cmdOutputMsg{output: out.String(), err: err, label: "Fetch from commit"}
		}

		if info.IsDir() {
			err = copyDir(srcPath, dstPath)
		} else {
			err = copyFile(srcPath, dstPath)
		}
		if err != nil {
			return cmdOutputMsg{output: out.String(), err: err, label: "Fetch from commit"}
		}

		out.WriteString(fmt.Sprintf("\nSuccessfully fetched %s to %s", path, dstPath))
		return cmdOutputMsg{output: out.String(), err: nil, label: "Fetch from commit"}
	}
}

type pickerItem struct{ label string }

func (i pickerItem) FilterValue() string { return i.label }

type pickerDelegate struct{}

func (d pickerDelegate) Height() int                             { return 1 }
func (d pickerDelegate) Spacing() int                            { return 0 }
func (d pickerDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d pickerDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	var label string
	switch i := listItem.(type) {
	case pickerItem:
		label = i.label
	case homeItem:
		label = i.label
	default:
		return
	}
	str := ui.MenuItemStyle.Render(label)
	if index == m.Index() {
		str = ui.SelectedMenuItemStyle.Render(str)
	}
	fmt.Fprint(w, str)
}

func buildPickerList(items []list.Item, title string, width, height int) list.Model {
	w, h := width, height
	if w == 0 {
		w = 60
	}
	if h == 0 {
		h = 20
	}
	l := list.New(items, pickerDelegate{}, w, h)
	l.Title = title
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(false)
	l.KeyMap.Quit = key.NewBinding(key.WithKeys("q", "ctrl+c"))
	l.KeyMap.CancelWhileFiltering = key.NewBinding(key.WithKeys("esc"))
	l.Styles.Title = ui.TitleStyle
	l.Styles.FilterPrompt = ui.CursorStyle
	l.Styles.FilterCursor = ui.CursorStyle
	return l
}

func (m *AppModel) cleanupFetchTemp() {
	if m.fetchTempDir != "" {
		os.RemoveAll(m.fetchTempDir)
		m.fetchTempDir = ""
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(dst, relPath)
		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}
		return copyFile(path, dstPath)
	})
}

func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height - 4
		switch m.screen {
		case screenRepoSelector:
			m.repoList.SetWidth(m.width)
			m.repoList.SetHeight(m.height)
		case screenMainMenu:
			m.cmdList.SetWidth(m.width)
			m.cmdList.SetHeight(m.height)
		case screenOutput:
			m.outputVP.Width = m.width
			m.outputVP.Height = m.height
		case screenFetchRepoList, screenFetchCommitList, screenFetchFileList:
		}
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case cmdOutputMsg:
		m.loading = false
		m.outputTitle = msg.label
		content := msg.output
		if msg.err != nil {
			content += "\n\n" + lipgloss.NewStyle().Foreground(ui.Danger).Render("Error: "+msg.err.Error())
		}
		m.outputVP = viewport.New(m.width, m.height)
		m.outputVP.SetContent(content)
		m.screen = screenOutput
		return m, nil

	case reposLoadedMsg:
		m.loading = false
		if msg.err != nil {
			m.lastErr = fmt.Sprintf("Could not load repos: %v", msg.err)
			// If we were loading repos for the fetch flow, go back to main menu.
			if m.fetchFlowActive {
				m.fetchFlowActive = false
				m.screen = screenMainMenu
				return m, nil
			}
			m.screen = screenRepoSelector
			m.repoList = buildRepoList(nil, m.width, m.height)
			return m, nil
		}
		// Fetch flow: populate picker with repos.
		if m.fetchFlowActive {
			m.fetchFlowActive = false
			var items []list.Item
			for _, r := range msg.repos {
				items = append(items, homeItem{label: r, isRepo: true, repo: r})
			}
			if len(items) == 0 {
				items = append(items, homeItem{label: "(no repos found)"})
			}
			m.pickerList = buildPickerList(items, "Select source repository", m.width, m.height)
			m.screen = screenFetchRepoList
			return m, nil
		}
		if len(msg.repos) == 1 && m.repo == "" {
			m.repo = msg.repos[0]
			m.screen = screenMainMenu
			m.cmdList = buildCmdList(m.repo, m.width, m.height)
			// Also build repoList so pressing 'b' later doesn't panic on a zero-value list.
			m.repoList = buildRepoList(msg.repos, m.width, m.height)
			return m, nil
		}
		if len(msg.repos) == 0 {
			m.textInput = newTextInput()
			m.screen = screenManualRepo
			return m, nil
		}
		m.screen = screenRepoSelector
		m.repoList = buildRepoList(msg.repos, m.width, m.height)
		return m, nil

	case tea.KeyMsg:
		switch m.screen {
		case screenRepoSelector:
			if key.Matches(msg, m.repoList.KeyMap.Quit) && m.repoList.FilterState() != list.Filtering {
				m.quitting = true
				return m, tea.Quit
			}
			if msg.String() == "esc" && m.repo != "" && m.repoList.FilterState() != list.Filtering {
				m.screen = screenMainMenu
				return m, nil
			}
			if msg.String() == "n" && m.repoList.FilterState() != list.Filtering {
				m.textInput = newTextInput()
				m.screen = screenManualRepo
				return m, nil
			}
			if msg.String() == "enter" && m.repoList.FilterState() != list.Filtering {
				if i, ok := m.repoList.SelectedItem().(homeItem); ok {
					if i.isRepo && i.repo != "" {
						m.repo = i.repo
						m.screen = screenMainMenu
						m.cmdList = buildCmdList(m.repo, m.width, m.height)
						return m, nil
					}
					if i.cmd != nil {
						return m, tea.ExecProcess(i.cmd(), func(err error) tea.Msg {
							return execFinishedMsg{err: err}
						})
					}
				}
			}
			var cmd tea.Cmd
			m.repoList, cmd = m.repoList.Update(msg)
			return m, cmd

		case screenManualRepo:
			if msg.String() == "ctrl+c" || msg.String() == "q" {
				m.quitting = true
				return m, tea.Quit
			}
			if msg.String() == "esc" {
				m.screen = screenRepoSelector
				return m, nil
			}
			if msg.String() == "enter" {
				repo := strings.TrimSpace(m.textInput.Value())
				if repo == "" {
					repo = m.textInput.Placeholder
				}
				if !strings.Contains(repo, "/") {
					m.lastErr = "Repository must be in 'owner/repo' format"
					return m, nil
				}
				m.repo = repo
				m.screen = screenMainMenu
				m.cmdList = buildCmdList(m.repo, m.width, m.height)
				return m, nil
			}
			var cmd tea.Cmd
			m.textInput, cmd = m.textInput.Update(msg)
			return m, cmd

		case screenBranchInput:
			if msg.String() == "ctrl+c" || msg.String() == "q" {
				m.quitting = true
				return m, tea.Quit
			}
			if msg.String() == "esc" {
				m.screen = screenMainMenu
				return m, nil
			}
			if msg.String() == "enter" {
				branch := strings.TrimSpace(m.textInput.Value())
				if branch == "" {
					branch = m.textInput.Placeholder
				}
				if branch == "" || strings.Contains(branch, " ") {
					m.lastErr = "Branch name cannot be empty or contain spaces"
					return m, nil
				}
				return m, tea.ExecProcess(exec.Command("git", "checkout", "-b", branch), func(err error) tea.Msg {
					return execFinishedMsg{err: err}
				})
			}
			var cmd tea.Cmd
			m.textInput, cmd = m.textInput.Update(msg)
			return m, cmd

		case screenCommitInput:
			if msg.String() == "ctrl+c" || msg.String() == "q" {
				m.quitting = true
				return m, tea.Quit
			}
			if msg.String() == "esc" {
				m.screen = screenMainMenu
				return m, nil
			}
			if msg.String() == "enter" {
				msgText := strings.TrimSpace(m.textInput.Value())
				if msgText == "" {
					msgText = m.textInput.Placeholder
				}
				if msgText == "" {
					m.lastErr = "Commit message cannot be empty"
					return m, nil
				}
				return m, commitAndPushCmd(msgText)
			}
			var cmd tea.Cmd
			m.textInput, cmd = m.textInput.Update(msg)
			return m, cmd

		case screenFetchRepoList:
			if key.Matches(msg, m.pickerList.KeyMap.Quit) && m.pickerList.FilterState() != list.Filtering {
				m.quitting = true
				return m, tea.Quit
			}
			if msg.String() == "esc" && m.pickerList.FilterState() != list.Filtering {
				m.cleanupFetchTemp()
				m.fetchFlowActive = false
				m.screen = screenMainMenu
				return m, nil
			}
			if msg.String() == "enter" && m.pickerList.FilterState() != list.Filtering {
				if i, ok := m.pickerList.SelectedItem().(homeItem); ok && i.isRepo {
					m.fetchSourceRepo = i.repo
					// Clone source repo into temp dir to inspect commits.
					m.loading = true
					m.loadingText = fmt.Sprintf("Cloning %s...", i.repo)
					return m, tea.Batch(func() tea.Msg {
						tmpDir, err := os.MkdirTemp("", "gh-gum-fetch-*")
						if err != nil {
							return commitsLoadedMsg{commits: nil, err: err}
						}
						_, err = exec.Command("gh", "repo", "clone", i.repo, tmpDir).CombinedOutput()
						if err != nil {
							os.RemoveAll(tmpDir)
							return commitsLoadedMsg{commits: nil, err: err}
						}
						m.fetchTempDir = tmpDir
						return loadCommitsCmd(tmpDir)()
					}, m.spinner.Tick)
				}
			}
			var cmd tea.Cmd
			m.pickerList, cmd = m.pickerList.Update(msg)
			return m, cmd

		case screenFetchCommitList:
			if key.Matches(msg, m.pickerList.KeyMap.Quit) && m.pickerList.FilterState() != list.Filtering {
				m.quitting = true
				return m, tea.Quit
			}
			if msg.String() == "esc" && m.pickerList.FilterState() != list.Filtering {
				m.fetchFlowActive = true
				m.screen = screenFetchRepoList
				return m, nil
			}
			if msg.String() == "enter" && m.pickerList.FilterState() != list.Filtering {
				if i, ok := m.pickerList.SelectedItem().(pickerItem); ok {
					// Extract commit hash from "abc123 message...".
					parts := strings.SplitN(i.label, " ", 2)
					if len(parts) > 0 && parts[0] != "" {
						m.fetchCommit = parts[0]
						return m, loadFilesCmd(m.fetchTempDir, m.fetchCommit)
					}
				}
			}
			var cmd tea.Cmd
			m.pickerList, cmd = m.pickerList.Update(msg)
			return m, cmd

		case screenFetchFileList:
			if key.Matches(msg, m.pickerList.KeyMap.Quit) && m.pickerList.FilterState() != list.Filtering {
				m.quitting = true
				return m, tea.Quit
			}
			if msg.String() == "esc" && m.pickerList.FilterState() != list.Filtering {
				m.fetchFlowActive = true
				m.screen = screenFetchCommitList
				return m, nil
			}
			if msg.String() == "enter" && m.pickerList.FilterState() != list.Filtering {
				if i, ok := m.pickerList.SelectedItem().(pickerItem); ok {
					m.fetchPath = i.label
					return m, fetchFromCommitCmd(m.fetchSourceRepo, m.fetchCommit, m.fetchPath)
				}
			}
			var cmd tea.Cmd
			m.pickerList, cmd = m.pickerList.Update(msg)
			return m, cmd

		case screenMainMenu:
			if key.Matches(msg, m.cmdList.KeyMap.Quit) && m.cmdList.FilterState() != list.Filtering {
				m.quitting = true
				return m, tea.Quit
			}
			if msg.String() == "b" && m.cmdList.FilterState() != list.Filtering {
				// Fetch all repos so the user can switch to a different one.
				m.loading = true
				m.loadingText = "Loading repositories..."
				return m, tea.Batch(listAllReposCmd(), m.spinner.Tick)
			}
			if msg.String() == "enter" && m.cmdList.FilterState() != list.Filtering {
				if i, ok := m.cmdList.SelectedItem().(cmdItem); ok {
					if i.action.Label == "Checkout new branch" {
						m.textInput = newBranchInput()
						m.screen = screenBranchInput
						return m, nil
					}
					if i.action.Label == "Push current branch" {
						return m, gitStatusCheckCmd()
					}
					if i.action.Label == "Smart push (checks + push)" {
						m.loading = true
						m.loadingText = "Running build & test checks..."
						return m, tea.Batch(runChecksCmd(), m.spinner.Tick)
					}
					if i.action.Label == "Fetch file/folder from commit" {
						m.cleanupFetchTemp()
						m.fetchSourceRepo = ""
						m.fetchCommit = ""
						m.fetchPath = ""
						m.fetchFlowActive = true
						// Show repo picker; preload current repo if known.
						m.loading = true
						m.loadingText = "Loading repositories..."
						return m, tea.Batch(listAllReposCmd(), m.spinner.Tick)
					}
					if i.action.Interactive {
						return m, tea.ExecProcess(i.action.Cmd(m.repo), func(err error) tea.Msg {
							return execFinishedMsg{err: err}
						})
					}
					return m, captureOutputCmd(i.action.Cmd(m.repo), i.action.Label)
				}
			}
			var cmd tea.Cmd
			m.cmdList, cmd = m.cmdList.Update(msg)
			return m, cmd

		case screenOutput:
			if msg.String() == "q" || msg.String() == "esc" || msg.String() == "b" {
				if m.pendingSmartPush {
					m.pendingSmartPush = false
					return m, gitStatusCheckCmd()
				}
				m.fetchFlowActive = false
				m.fetchSourceRepo = ""
				m.fetchCommit = ""
				m.fetchPath = ""
				m.screen = screenMainMenu
				return m, nil
			}
			var cmd tea.Cmd
			m.outputVP, cmd = m.outputVP.Update(msg)
			return m, cmd
		}

	case prePushStatusMsg:
		if !msg.hasChanges {
			return m, captureOutputCmd(exec.Command("git", "push", "origin", "HEAD"), "Push current branch")
		}
		m.textInput = newCommitInput()
		m.screen = screenCommitInput
		return m, nil

	case commitPushMsg:
		m.loading = false
		m.outputTitle = "Commit & Push"
		content := msg.output
		if msg.err != nil {
			content += "\n\n" + lipgloss.NewStyle().Foreground(ui.Danger).Render("Error: "+msg.err.Error())
		}
		m.outputVP = viewport.New(m.width, m.height)
		m.outputVP.SetContent(content)
		m.screen = screenOutput
		return m, nil

	case checksPassedMsg:
		m.loading = false
		if msg.err != nil {
			m.outputTitle = "Pre-push checks failed"
			content := msg.output + "\n\n" + lipgloss.NewStyle().Foreground(ui.Danger).Render("Error: "+msg.err.Error())
			m.outputVP = viewport.New(m.width, m.height)
			m.outputVP.SetContent(content)
			m.screen = screenOutput
			return m, nil
		}
		m.pendingSmartPush = true
		m.outputTitle = "Pre-push checks passed"
		m.outputVP = viewport.New(m.width, m.height)
		m.outputVP.SetContent(msg.output + "\n\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00")).Render("All checks passed. Press any key to continue to push."))
		m.screen = screenOutput
		return m, nil

	case commitsLoadedMsg:
		m.loading = false
		if msg.err != nil {
			m.lastErr = fmt.Sprintf("Could not load commits: %v", msg.err)
			m.cleanupFetchTemp()
			m.screen = screenMainMenu
			return m, nil
		}
		var items []list.Item
		for _, c := range msg.commits {
			items = append(items, pickerItem{label: c})
		}
		if len(items) == 0 {
			items = append(items, pickerItem{label: "(no commits found)"})
		}
		m.pickerList = buildPickerList(items, fmt.Sprintf("Select commit from %s", m.fetchSourceRepo), m.width, m.height)
		m.screen = screenFetchCommitList
		return m, nil

	case filesLoadedMsg:
		m.loading = false
		if msg.err != nil {
			m.lastErr = fmt.Sprintf("Could not load files: %v", msg.err)
			m.cleanupFetchTemp()
			m.screen = screenMainMenu
			return m, nil
		}
		var items []list.Item
		for _, f := range msg.files {
			items = append(items, pickerItem{label: f})
		}
		if len(items) == 0 {
			items = append(items, pickerItem{label: "(no files found)"})
		}
		m.pickerList = buildPickerList(items, fmt.Sprintf("Select file/folder from %s", m.fetchCommit), m.width, m.height)
		m.screen = screenFetchFileList
		return m, nil

	case execFinishedMsg:
		if msg.err != nil {
			m.lastErr = fmt.Sprintf("Command failed: %v", msg.err)
		}
		return m, nil
	}

	return m, nil
}

func (m AppModel) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	switch m.screen {
	case screenLoading:
		b.WriteString(ui.TitleStyle.Render("gh-gum"))
		b.WriteString("\n\n")
		if m.loading {
			b.WriteString(m.spinner.View())
			b.WriteString(" " + ui.SubtitleStyle.Render(m.loadingText))
		} else {
			b.WriteString(ui.SubtitleStyle.Render("Detecting repository..."))
		}

	case screenRepoSelector:
		if m.loading {
			b.WriteString("\n\n")
			b.WriteString(m.spinner.View())
			b.WriteString(" " + ui.SubtitleStyle.Render(m.loadingText))
			break
		}
		b.WriteString(m.repoList.View())
		b.WriteString("\n")
		if m.repo != "" {
			help := fmt.Sprintf(
				"%s %s · %s %s · %s %s · %s %s · %s %s",
				ui.HelpKeyStyle.Render("↑/k"), ui.HelpDescStyle.Render("up"),
				ui.HelpKeyStyle.Render("↓/j"), ui.HelpDescStyle.Render("down"),
				ui.HelpKeyStyle.Render("/"), ui.HelpDescStyle.Render("filter"),
				ui.HelpKeyStyle.Render("n"), ui.HelpDescStyle.Render("manual"),
				ui.HelpKeyStyle.Render("esc"), ui.HelpDescStyle.Render("back to menu"),
			)
			b.WriteString(ui.StatusBarStyle.Render(help))
		} else {
			help := fmt.Sprintf(
				"%s %s · %s %s · %s %s · %s %s",
				ui.HelpKeyStyle.Render("↑/k"), ui.HelpDescStyle.Render("up"),
				ui.HelpKeyStyle.Render("↓/j"), ui.HelpDescStyle.Render("down"),
				ui.HelpKeyStyle.Render("/"), ui.HelpDescStyle.Render("filter"),
				ui.HelpKeyStyle.Render("n"), ui.HelpDescStyle.Render("manual"),
			)
			b.WriteString(ui.StatusBarStyle.Render(help))
		}

	case screenManualRepo:
		b.WriteString(ui.TitleStyle.Render("Enter repository"))
		b.WriteString("\n\n")
		b.WriteString(ui.SubtitleStyle.Render("Type the repository as owner/repo and press Enter"))
		b.WriteString("\n\n")
		b.WriteString(m.textInput.View())
		b.WriteString("\n")
		help := fmt.Sprintf(
			"%s %s · %s %s",
			ui.HelpKeyStyle.Render("enter"), ui.HelpDescStyle.Render("confirm"),
			ui.HelpKeyStyle.Render("esc"), ui.HelpDescStyle.Render("back"),
		)
		b.WriteString(ui.StatusBarStyle.Render(help))

	case screenMainMenu:
		if m.loading {
			b.WriteString("\n\n")
			b.WriteString(m.spinner.View())
			b.WriteString(" " + ui.SubtitleStyle.Render(m.loadingText))
			break
		}
		b.WriteString(m.cmdList.View())
		b.WriteString("\n")
		header := ui.SubtitleStyle.Render(fmt.Sprintf("Repository: %s", m.repo))
		help := fmt.Sprintf(
			"%s %s · %s %s · %s %s · %s %s · %s %s",
			ui.HelpKeyStyle.Render("↑/k"), ui.HelpDescStyle.Render("up"),
			ui.HelpKeyStyle.Render("↓/j"), ui.HelpDescStyle.Render("down"),
			ui.HelpKeyStyle.Render("/"), ui.HelpDescStyle.Render("filter"),
			ui.HelpKeyStyle.Render("b"), ui.HelpDescStyle.Render("back"),
			ui.HelpKeyStyle.Render("q"), ui.HelpDescStyle.Render("quit"),
		)
		b.WriteString(ui.StatusBarStyle.Render(header + "  " + help))

	case screenBranchInput:
		b.WriteString(ui.TitleStyle.Render("Checkout new branch"))
		b.WriteString("\n\n")
		b.WriteString(ui.SubtitleStyle.Render("Type the new branch name and press Enter"))
		b.WriteString("\n\n")
		b.WriteString(m.textInput.View())
		b.WriteString("\n")
		help := fmt.Sprintf(
			"%s %s · %s %s",
			ui.HelpKeyStyle.Render("enter"), ui.HelpDescStyle.Render("confirm"),
			ui.HelpKeyStyle.Render("esc"), ui.HelpDescStyle.Render("back"),
		)
		b.WriteString(ui.StatusBarStyle.Render(help))

	case screenCommitInput:
		b.WriteString(ui.TitleStyle.Render("Uncommitted changes detected"))
		b.WriteString("\n\n")
		b.WriteString(ui.SubtitleStyle.Render("Enter a commit message to stage, commit, and push"))
		b.WriteString("\n\n")
		b.WriteString(m.textInput.View())
		b.WriteString("\n")
		help := fmt.Sprintf(
			"%s %s · %s %s",
			ui.HelpKeyStyle.Render("enter"), ui.HelpDescStyle.Render("commit & push"),
			ui.HelpKeyStyle.Render("esc"), ui.HelpDescStyle.Render("cancel"),
		)
		b.WriteString(ui.StatusBarStyle.Render(help))

	case screenFetchRepoList:
		if m.loading {
			b.WriteString("\n\n")
			b.WriteString(m.spinner.View())
			b.WriteString(" " + ui.SubtitleStyle.Render(m.loadingText))
			break
		}
		b.WriteString(m.pickerList.View())
		b.WriteString("\n")
		help := fmt.Sprintf(
			"%s %s · %s %s · %s %s · %s %s",
			ui.HelpKeyStyle.Render("↑/k"), ui.HelpDescStyle.Render("up"),
			ui.HelpKeyStyle.Render("↓/j"), ui.HelpDescStyle.Render("down"),
			ui.HelpKeyStyle.Render("/"), ui.HelpDescStyle.Render("filter"),
			ui.HelpKeyStyle.Render("esc"), ui.HelpDescStyle.Render("cancel"),
		)
		b.WriteString(ui.StatusBarStyle.Render(help))

	case screenFetchCommitList:
		if m.loading {
			b.WriteString("\n\n")
			b.WriteString(m.spinner.View())
			b.WriteString(" " + ui.SubtitleStyle.Render(m.loadingText))
			break
		}
		b.WriteString(m.pickerList.View())
		b.WriteString("\n")
		help := fmt.Sprintf(
			"%s %s · %s %s · %s %s · %s %s",
			ui.HelpKeyStyle.Render("↑/k"), ui.HelpDescStyle.Render("up"),
			ui.HelpKeyStyle.Render("↓/j"), ui.HelpDescStyle.Render("down"),
			ui.HelpKeyStyle.Render("/"), ui.HelpDescStyle.Render("filter"),
			ui.HelpKeyStyle.Render("esc"), ui.HelpDescStyle.Render("back"),
		)
		b.WriteString(ui.StatusBarStyle.Render(help))

	case screenFetchFileList:
		if m.loading {
			b.WriteString("\n\n")
			b.WriteString(m.spinner.View())
			b.WriteString(" " + ui.SubtitleStyle.Render(m.loadingText))
			break
		}
		b.WriteString(m.pickerList.View())
		b.WriteString("\n")
		help := fmt.Sprintf(
			"%s %s · %s %s · %s %s · %s %s",
			ui.HelpKeyStyle.Render("↑/k"), ui.HelpDescStyle.Render("up"),
			ui.HelpKeyStyle.Render("↓/j"), ui.HelpDescStyle.Render("down"),
			ui.HelpKeyStyle.Render("/"), ui.HelpDescStyle.Render("filter"),
			ui.HelpKeyStyle.Render("esc"), ui.HelpDescStyle.Render("back"),
		)
		b.WriteString(ui.StatusBarStyle.Render(help))

	case screenOutput:
		b.WriteString(ui.TitleStyle.Render(m.outputTitle))
		b.WriteString("\n")
		b.WriteString(m.outputVP.View())
		b.WriteString("\n")
		help := fmt.Sprintf(
			"%s %s · %s %s",
			ui.HelpKeyStyle.Render("↑/↓/pgup/pgdn"), ui.HelpDescStyle.Render("scroll"),
			ui.HelpKeyStyle.Render("q/esc/b"), ui.HelpDescStyle.Render("back"),
		)
		b.WriteString(ui.StatusBarStyle.Render(help))
	}

	if m.lastErr != "" {
		b.WriteString("\n")
		b.WriteString(ui.BoxStyle.Render(lipgloss.NewStyle().Foreground(ui.Danger).Render(m.lastErr)))
	}

	return b.String()
}

// ---------- helpers ----------

func buildRepoList(repos []string, width, height int) list.Model {
	var items []list.Item

	// Global actions appear first and run directly without picking a repo.
	items = append(items, homeItem{
		label: "Create new repo from current directory",
		cmd:   func() *exec.Cmd { return gh.NewCommand("repo", "create") },
	})
	items = append(items, homeItem{
		label: "Clone a repository",
		cmd:   func() *exec.Cmd { return gh.NewCommand("repo", "clone") },
	})

	for _, r := range repos {
		items = append(items, homeItem{label: r, isRepo: true, repo: r})
	}
	if len(repos) == 0 {
		items = append(items, homeItem{label: "(no repos found)"})
	}

	w, h := width, height
	if w == 0 {
		w = 60
	}
	if h == 0 {
		h = 20
	}

	l := list.New(items, homeDelegate{}, w, h)
	l.Title = "Select a repository"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(false)
	l.KeyMap.Quit = key.NewBinding(key.WithKeys("q", "ctrl+c"))
	l.KeyMap.CancelWhileFiltering = key.NewBinding(key.WithKeys("esc"))
	l.Styles.Title = ui.TitleStyle
	l.Styles.FilterPrompt = ui.CursorStyle
	l.Styles.FilterCursor = ui.CursorStyle
	return l
}

func buildCmdList(repo string, width, height int) list.Model {
	var items []list.Item
	for _, a := range commands.Actions() {
		items = append(items, cmdItem{action: a})
	}

	w, h := width, height
	if w == 0 {
		w = 60
	}
	if h == 0 {
		h = 20
	}

	l := list.New(items, cmdDelegate{repo: repo}, w, h)
	l.Title = "GitHub CLI Menu"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(false)
	l.KeyMap.Quit = key.NewBinding(key.WithKeys("q", "ctrl+c"))
	l.KeyMap.CancelWhileFiltering = key.NewBinding(key.WithKeys("esc"))
	l.Styles.Title = ui.TitleStyle
	l.Styles.FilterPrompt = ui.CursorStyle
	l.Styles.FilterCursor = ui.CursorStyle
	return l
}

// Run starts the Bubble Tea program.
func captureOutputCmd(cmd *exec.Cmd, label string) tea.Cmd {
	return func() tea.Msg {
		out, err := cmd.CombinedOutput()
		return cmdOutputMsg{output: string(out), err: err, label: label}
	}
}

func Run() error {
	p := tea.NewProgram(NewAppModel(), tea.WithAltScreen())
	_, err := p.Run()
	if err != nil && !errors.Is(err, tea.ErrProgramKilled) {
		return err
	}
	return nil
}
