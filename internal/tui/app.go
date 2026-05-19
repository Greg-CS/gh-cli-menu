package tui

import (
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
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

// ---------- model ----------

type AppModel struct {
	screen    screen
	repo      string
	repoList  list.Model
	cmdList   list.Model
	textInput textinput.Model
	width     int
	height    int
	quitting  bool
	lastErr   string
}

func NewAppModel() AppModel {
	return AppModel{screen: screenLoading}
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

func (m AppModel) Init() tea.Cmd {
	return detectRepoCmd()
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
		}
		return m, nil

	case reposLoadedMsg:
		if msg.err != nil {
			m.lastErr = fmt.Sprintf("Could not load repos: %v", msg.err)
			m.screen = screenRepoSelector
			m.repoList = buildRepoList(nil, m.width, m.height)
			return m, nil
		}
		if len(msg.repos) == 1 && m.repo == "" {
			m.repo = msg.repos[0]
			m.screen = screenMainMenu
			m.cmdList = buildCmdList(m.repo, m.width, m.height)
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

		case screenMainMenu:
			if key.Matches(msg, m.cmdList.KeyMap.Quit) && m.cmdList.FilterState() != list.Filtering {
				m.quitting = true
				return m, tea.Quit
			}
			if msg.String() == "b" && m.cmdList.FilterState() != list.Filtering {
				m.screen = screenRepoSelector
				return m, nil
			}
			if msg.String() == "enter" && m.cmdList.FilterState() != list.Filtering {
				if i, ok := m.cmdList.SelectedItem().(cmdItem); ok {
					if i.action.Label == "Checkout new branch" {
						m.textInput = newBranchInput()
						m.screen = screenBranchInput
						return m, nil
					}
					return m, tea.ExecProcess(i.action.Cmd(m.repo), func(err error) tea.Msg {
						return execFinishedMsg{err: err}
					})
				}
			}
			var cmd tea.Cmd
			m.cmdList, cmd = m.cmdList.Update(msg)
			return m, cmd
		}

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
		b.WriteString("\n")
		b.WriteString(ui.SubtitleStyle.Render("Detecting repository..."))

	case screenRepoSelector:
		b.WriteString(m.repoList.View())
		b.WriteString("\n")
		help := fmt.Sprintf(
			"%s %s · %s %s · %s %s · %s %s",
			ui.HelpKeyStyle.Render("↑/k"), ui.HelpDescStyle.Render("up"),
			ui.HelpKeyStyle.Render("↓/j"), ui.HelpDescStyle.Render("down"),
			ui.HelpKeyStyle.Render("/"), ui.HelpDescStyle.Render("filter"),
			ui.HelpKeyStyle.Render("n"), ui.HelpDescStyle.Render("manual"),
		)
		b.WriteString(ui.StatusBarStyle.Render(help))

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
func Run() error {
	p := tea.NewProgram(NewAppModel(), tea.WithAltScreen())
	_, err := p.Run()
	if err != nil && !errors.Is(err, tea.ErrProgramKilled) {
		return err
	}
	return nil
}
