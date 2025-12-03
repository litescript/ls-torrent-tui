// Package theme provides terminal theming with automatic detection.
// It supports reading colors from Alacritty, Kitty, Foot, and Omarchy
// terminal configurations, with environment variable overrides available.
package theme

import "github.com/charmbracelet/lipgloss"

// Palette holds the color scheme for the TUI
type Palette struct {
	BG       string // background
	FG       string // foreground (primary text)
	Muted    string // timestamps, secondary info
	Accent   string // health bars, highlights
	AccentBg string // selection background
	Error    string // error/warning colors
}

// DefaultPalette returns the fallback amber-on-dark theme
func DefaultPalette() Palette {
	return Palette{
		BG:       "#0a0a0a",
		FG:       "#d4a017",
		Muted:    "#6b6b4f",
		Accent:   "#8bc34a",
		AccentBg: "#1a1a14",
		Error:    "#ff6b6b",
	}
}

// Styles holds all lipgloss styles derived from a palette
type Styles struct {
	App           lipgloss.Style
	Header        lipgloss.Style
	Title         lipgloss.Style
	StatusBar     lipgloss.Style
	SearchInput   lipgloss.Style
	SearchPrompt  lipgloss.Style
	Table         lipgloss.Style
	TableHeader   lipgloss.Style
	SortedHeader  lipgloss.Style // Highlighted sorted column
	TableRow      lipgloss.Style
	TableSelected lipgloss.Style
	HealthGood    lipgloss.Style
	HealthMed     lipgloss.Style
	HealthBad     lipgloss.Style
	Muted         lipgloss.Style
	Error         lipgloss.Style
	VPNConnected  lipgloss.Style
	VPNDisconnect lipgloss.Style
	HelpKey       lipgloss.Style
	HelpDesc      lipgloss.Style
	Panel         lipgloss.Style
	PanelTitle    lipgloss.Style
}

// NewStyles creates styles from a palette
func NewStyles(p Palette) Styles {
	return Styles{
		App: lipgloss.NewStyle().
			Background(lipgloss.Color(p.BG)),

		Header: lipgloss.NewStyle().
			Foreground(lipgloss.Color(p.FG)).
			Bold(true).
			Padding(0, 1),

		Title: lipgloss.NewStyle().
			Foreground(lipgloss.Color(p.FG)).
			Bold(true),

		StatusBar: lipgloss.NewStyle().
			Foreground(lipgloss.Color(p.Muted)).
			Padding(0, 1),

		SearchInput: lipgloss.NewStyle().
			Foreground(lipgloss.Color(p.FG)),

		SearchPrompt: lipgloss.NewStyle().
			Foreground(lipgloss.Color(p.Muted)),

		Table: lipgloss.NewStyle().
			Padding(0, 1),

		TableHeader: lipgloss.NewStyle().
			Foreground(lipgloss.Color(p.Muted)).
			Bold(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderBottom(true).
			BorderForeground(lipgloss.Color(p.Muted)),

		SortedHeader: lipgloss.NewStyle().
			Foreground(lipgloss.Color(p.FG)).
			Bold(true),

		TableRow: lipgloss.NewStyle().
			Foreground(lipgloss.Color(p.FG)),

		TableSelected: lipgloss.NewStyle().
			Foreground(lipgloss.Color(p.FG)).
			Background(lipgloss.Color(p.AccentBg)).
			Bold(true),

		HealthGood: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8bc34a")),

		HealthMed: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ffb347")),

		HealthBad: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ff6b6b")),

		Muted: lipgloss.NewStyle().
			Foreground(lipgloss.Color(p.Muted)),

		Error: lipgloss.NewStyle().
			Foreground(lipgloss.Color(p.Error)),

		VPNConnected: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8bc34a")),

		VPNDisconnect: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ff6b6b")),

		HelpKey: lipgloss.NewStyle().
			Foreground(lipgloss.Color(p.Muted)),

		HelpDesc: lipgloss.NewStyle().
			Foreground(lipgloss.Color(p.FG)),

		Panel: lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(p.Muted)).
			Padding(0, 1),

		PanelTitle: lipgloss.NewStyle().
			Foreground(lipgloss.Color(p.FG)).
			Bold(true),
	}
}

// Current holds the active palette and styles
var Current Styles
var CurrentPalette Palette

func init() {
	// Initialize with detected or default theme
	CurrentPalette = Detect()
	Current = NewStyles(CurrentPalette)
}

// Refresh reloads the theme from config files
func Refresh() {
	CurrentPalette = Detect()
	Current = NewStyles(CurrentPalette)
}
