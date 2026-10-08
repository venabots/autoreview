package tui

import "charm.land/lipgloss/v2"

// The view uses the terminal's own sixteen colors, so it follows the
// person's theme and stays readable on a light background.
var (
	plainStyle   = lipgloss.NewStyle()
	boldStyle    = lipgloss.NewStyle().Bold(true)
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.BrightBlack)
	greenStyle   = lipgloss.NewStyle().Foreground(lipgloss.Green)
	yellowStyle  = lipgloss.NewStyle().Foreground(lipgloss.Yellow)
	redStyle     = lipgloss.NewStyle().Foreground(lipgloss.Red)
	cyanStyle    = lipgloss.NewStyle().Foreground(lipgloss.Cyan)
	numberStyle  = lipgloss.NewStyle().Foreground(lipgloss.Cyan).Bold(true)
	brandStyle   = lipgloss.NewStyle().Foreground(lipgloss.Magenta).Bold(true)
	headingStyle = lipgloss.NewStyle().Foreground(lipgloss.BrightBlack).Bold(true)
	activeTab    = lipgloss.NewStyle().Bold(true).Reverse(true)
	spinnerStyle = lipgloss.NewStyle().Foreground(lipgloss.Magenta)
)
