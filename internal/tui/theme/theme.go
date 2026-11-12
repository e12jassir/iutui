// Package theme centralizes the YouTube Dark color palette and Lip Gloss styles
// used across the ytx terminal user interface.
package theme

import "github.com/charmbracelet/lipgloss"

// YouTube Dark palette colors matching the web prototype.
const (
	Bg     = lipgloss.Color("#0f0f0f")
	Panel  = lipgloss.Color("#181818")
	Surf   = lipgloss.Color("#272727")
	SurfHi = lipgloss.Color("#333333")
	Border = lipgloss.Color("#3f3f3f")
	Rule   = lipgloss.Color("#262626")
	Text   = lipgloss.Color("#f1f1f1")
	White  = lipgloss.Color("#ffffff")
	Muted  = lipgloss.Color("#aaaaaa")
	Dim    = lipgloss.Color("#717171")
	Red    = lipgloss.Color("#ff0033")
	Live   = lipgloss.Color("#cc0000")
	Blue   = lipgloss.Color("#3ea6ff")
	Yellow = lipgloss.Color("#f5c518")
	Track  = lipgloss.Color("#3a3a3a")
)

// Theme holds the Lip Gloss styles for the TUI components.
type Theme struct {
	HeaderLogo        lipgloss.Style
	SearchBox         lipgloss.Style
	SearchBoxFocused  lipgloss.Style
	Chip              lipgloss.Style
	ChipActive        lipgloss.Style
	Card              lipgloss.Style
	CardSelected      lipgloss.Style
	BadgeLive         lipgloss.Style
	BadgeDuration     lipgloss.Style
	StatusBadge       lipgloss.Style
	Toast             lipgloss.Style
	SidebarItem       lipgloss.Style
	SidebarItemActive lipgloss.Style
}

// DefaultTheme returns the default YouTube Dark Lip Gloss theme.
func DefaultTheme() Theme {
	return Theme{
		HeaderLogo: lipgloss.NewStyle().
			Foreground(White).
			Background(Red).
			Bold(true).
			Padding(0, 1),
		SearchBox: lipgloss.NewStyle().
			Background(Surf).
			Foreground(Muted).
			Padding(0, 1),
		SearchBoxFocused: lipgloss.NewStyle().
			Background(SurfHi).
			Foreground(White).
			Padding(0, 1),
		Chip: lipgloss.NewStyle().
			Background(Surf).
			Foreground(Text).
			Padding(0, 1),
		ChipActive: lipgloss.NewStyle().
			Background(White).
			Foreground(Bg).
			Bold(true).
			Padding(0, 1),
		Card: lipgloss.NewStyle().
			Border(lipgloss.HiddenBorder()).
			Background(Bg).
			Foreground(Text).
			Padding(0, 1),
		CardSelected: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(Text).
			Background(Bg).
			Foreground(White).
			Padding(0, 1),
		BadgeLive: lipgloss.NewStyle().
			Background(Live).
			Foreground(White).
			Bold(true).
			Padding(0, 1),
		BadgeDuration: lipgloss.NewStyle().
			Background(Bg).
			Foreground(White).
			Padding(0, 1),
		StatusBadge: lipgloss.NewStyle().
			Background(SurfHi).
			Foreground(Muted).
			Padding(0, 1),
		Toast: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(Border).
			Background(Surf).
			Foreground(White).
			Padding(0, 1),
		SidebarItem: lipgloss.NewStyle().
			Foreground(Muted).
			Background(Bg).
			Padding(0, 1),
		SidebarItemActive: lipgloss.NewStyle().
			Foreground(White).
			Background(Surf).
			Bold(true).
			Padding(0, 1),
	}
}
