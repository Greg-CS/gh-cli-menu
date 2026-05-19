package ui

import "github.com/charmbracelet/lipgloss"

var (
	// Base colors
	Primary   = lipgloss.Color("#7B61FF")
	Secondary = lipgloss.Color("#FF61DC")
	Success   = lipgloss.Color("#04B575")
	Warning   = lipgloss.Color("#F1C40F")
	Danger    = lipgloss.Color("#E74C3C")
	Text      = lipgloss.Color("#FAFAFA")
	Muted     = lipgloss.Color("#727272")
	Surface   = lipgloss.Color("#2A2A2A")
	Bg        = lipgloss.Color("#1A1A1A")

	TitleStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(Primary).
		MarginBottom(1).
		MarginTop(1)

	SubtitleStyle = lipgloss.NewStyle().
		Foreground(Muted).
		MarginBottom(2)

	MenuItemStyle = lipgloss.NewStyle().
		PaddingLeft(2).
		Foreground(Text)

	SelectedMenuItemStyle = lipgloss.NewStyle().
		PaddingLeft(0).
		Foreground(Primary).
		Bold(true).
		SetString("▸ ")

	CursorStyle = lipgloss.NewStyle().
		Foreground(Primary)

	StatusBarStyle = lipgloss.NewStyle().
		Background(Surface).
		Foreground(Muted).
		Padding(0, 1)

	BoxStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(Surface).
		Padding(1, 2)

	HelpKeyStyle = lipgloss.NewStyle().
		Foreground(Primary).
		Bold(true)

	HelpDescStyle = lipgloss.NewStyle().
		Foreground(Muted)
)
