package style

import "github.com/charmbracelet/lipgloss"

var (
	Brand   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7C3AED"))
	Success = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#10B981"))
	Error   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#EF4444"))
	Muted   = lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
	File    = lipgloss.NewStyle().Foreground(lipgloss.Color("#60A5FA"))
	Bold    = lipgloss.NewStyle().Bold(true)

	Banner = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#7C3AED")).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7C3AED")).
		Padding(0, 2)
)
