package tui

import "github.com/charmbracelet/lipgloss"

// Palette loosely follows FreshRSS's default "Origine" theme: a blue accent,
// muted greys and a gold star.
var (
	colAccent = lipgloss.AdaptiveColor{Light: "#0062BE", Dark: "#5AA9FF"}
	colMuted  = lipgloss.AdaptiveColor{Light: "#6B6B6B", Dark: "#8C8C8C"}
	colError  = lipgloss.AdaptiveColor{Light: "#C0262D", Dark: "#FF6B6B"}
	colStar   = lipgloss.AdaptiveColor{Light: "#B07A00", Dark: "#F5C542"}
	colBorder = lipgloss.AdaptiveColor{Light: "#BDBDBD", Dark: "#4A4A4A"}
	colOnBar  = lipgloss.Color("#FFFFFF")

	// colSelBlur highlights the selected row of a pane that does not have focus.
	colSelBlur = lipgloss.AdaptiveColor{Light: "#DCE8F7", Dark: "#2B3B55"}
)

var (
	styHeader   = lipgloss.NewStyle().Bold(true).Foreground(colOnBar).Background(colAccent)
	styFooter   = lipgloss.NewStyle().Foreground(colMuted)
	styError    = lipgloss.NewStyle().Foreground(colError).Bold(true)
	styMuted    = lipgloss.NewStyle().Foreground(colMuted)
	styBold     = lipgloss.NewStyle().Bold(true)
	styAccent   = lipgloss.NewStyle().Foreground(colAccent)
	styAccentB  = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	styStar     = lipgloss.NewStyle().Foreground(colStar)
	styBorder   = lipgloss.NewStyle().Foreground(colBorder)
	styCursor   = lipgloss.NewStyle().Foreground(colOnBar).Background(colAccent)
	styCursorBg = lipgloss.NewStyle().Bold(true).Background(colSelBlur)
)
