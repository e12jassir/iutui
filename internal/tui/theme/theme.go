// Package theme centralizes the YouTube Dark color palette and Lip Gloss styles
// used across the ytx terminal user interface.
package theme

import (
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

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
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#374151")).
			Foreground(Text).
			Padding(0, 1),
		CardSelected: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#BD93F9")).
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

// Palette defines a complete color scheme for the ytx TUI.
type Palette struct {
	Name      string
	Bg        lipgloss.Color
	Panel     lipgloss.Color
	Surf      lipgloss.Color
	SurfHi    lipgloss.Color
	Border    lipgloss.Color
	Rule      lipgloss.Color
	Text      lipgloss.Color
	White     lipgloss.Color
	Muted     lipgloss.Color
	Dim       lipgloss.Color
	Red       lipgloss.Color
	Live      lipgloss.Color
	Blue      lipgloss.Color
	Yellow    lipgloss.Color
	Track     lipgloss.Color
	Accent    lipgloss.Color
	Secondary lipgloss.Color
}

var paletteOrder = []string{
	"youtube-dark",
	"catppuccin",
	"tokyonight",
	"gruvbox",
	"kanagawa",
	"terminal",
}

var palettesByName = map[string]Palette{
	"youtube-dark": {
		Name:      "youtube-dark",
		Bg:        Bg,
		Panel:     Panel,
		Surf:      Surf,
		SurfHi:    SurfHi,
		Border:    Border,
		Rule:      Rule,
		Text:      Text,
		White:     White,
		Muted:     Muted,
		Dim:       Dim,
		Red:       Red,
		Live:      Live,
		Blue:      Blue,
		Yellow:    Yellow,
		Track:     Track,
		Accent:    lipgloss.Color("#BD93F9"),
		Secondary: Blue,
	},
	"catppuccin": {
		Name:      "catppuccin",
		Bg:        lipgloss.Color("#1e1e2e"),
		Panel:     lipgloss.Color("#181825"),
		Surf:      lipgloss.Color("#313244"),
		SurfHi:    lipgloss.Color("#45475a"),
		Border:    lipgloss.Color("#585b70"),
		Rule:      lipgloss.Color("#313244"),
		Text:      lipgloss.Color("#cdd6f4"),
		White:     lipgloss.Color("#eff1f5"),
		Muted:     lipgloss.Color("#a6adc8"),
		Dim:       lipgloss.Color("#6c7086"),
		Red:       lipgloss.Color("#f38ba8"),
		Live:      lipgloss.Color("#eba0ac"),
		Blue:      lipgloss.Color("#89b4fa"),
		Yellow:    lipgloss.Color("#f9e2af"),
		Track:     lipgloss.Color("#45475a"),
		Accent:    lipgloss.Color("#cba6f7"),
		Secondary: lipgloss.Color("#89b4fa"),
	},
	"tokyonight": {
		Name:      "tokyonight",
		Bg:        lipgloss.Color("#1a1b26"),
		Panel:     lipgloss.Color("#16161e"),
		Surf:      lipgloss.Color("#24283b"),
		SurfHi:    lipgloss.Color("#292e42"),
		Border:    lipgloss.Color("#3b4261"),
		Rule:      lipgloss.Color("#292e42"),
		Text:      lipgloss.Color("#c0caf5"),
		White:     lipgloss.Color("#e0e8ff"),
		Muted:     lipgloss.Color("#a9b1d6"),
		Dim:       lipgloss.Color("#565f89"),
		Red:       lipgloss.Color("#f7768e"),
		Live:      lipgloss.Color("#db4b4b"),
		Blue:      lipgloss.Color("#7aa2f7"),
		Yellow:    lipgloss.Color("#e0af68"),
		Track:     lipgloss.Color("#3b4261"),
		Accent:    lipgloss.Color("#bb9af7"),
		Secondary: lipgloss.Color("#7dcfff"),
	},
	"gruvbox": {
		Name:      "gruvbox",
		Bg:        lipgloss.Color("#282828"),
		Panel:     lipgloss.Color("#1d2021"),
		Surf:      lipgloss.Color("#3c3836"),
		SurfHi:    lipgloss.Color("#504945"),
		Border:    lipgloss.Color("#665c54"),
		Rule:      lipgloss.Color("#3c3836"),
		Text:      lipgloss.Color("#ebdbb2"),
		White:     lipgloss.Color("#fbf1c7"),
		Muted:     lipgloss.Color("#bdae93"),
		Dim:       lipgloss.Color("#928374"),
		Red:       lipgloss.Color("#fb4934"),
		Live:      lipgloss.Color("#cc241d"),
		Blue:      lipgloss.Color("#83a598"),
		Yellow:    lipgloss.Color("#fabd2f"),
		Track:     lipgloss.Color("#504945"),
		Accent:    lipgloss.Color("#fe8019"),
		Secondary: lipgloss.Color("#8ec07c"),
	},
	"kanagawa": {
		Name:      "kanagawa",
		Bg:        lipgloss.Color("#1f1f28"),
		Panel:     lipgloss.Color("#16161d"),
		Surf:      lipgloss.Color("#2a2a37"),
		SurfHi:    lipgloss.Color("#363646"),
		Border:    lipgloss.Color("#54546d"),
		Rule:      lipgloss.Color("#2a2a37"),
		Text:      lipgloss.Color("#dcd7ba"),
		White:     lipgloss.Color("#e6e0c2"),
		Muted:     lipgloss.Color("#c8c093"),
		Dim:       lipgloss.Color("#727169"),
		Red:       lipgloss.Color("#e82424"),
		Live:      lipgloss.Color("#c34043"),
		Blue:      lipgloss.Color("#7e9cd8"),
		Yellow:    lipgloss.Color("#dca561"),
		Track:     lipgloss.Color("#363646"),
		Accent:    lipgloss.Color("#957fb8"),
		Secondary: lipgloss.Color("#7fb4ca"),
	},
	"terminal": {
		Name:      "terminal",
		Bg:        lipgloss.Color("0"),
		Panel:     lipgloss.Color("0"),
		Surf:      lipgloss.Color("0"),
		SurfHi:    lipgloss.Color("8"),
		Border:    lipgloss.Color("8"),
		Rule:      lipgloss.Color("8"),
		Text:      lipgloss.Color("7"),
		White:     lipgloss.Color("15"),
		Muted:     lipgloss.Color("7"),
		Dim:       lipgloss.Color("8"),
		Red:       lipgloss.Color("1"),
		Live:      lipgloss.Color("9"),
		Blue:      lipgloss.Color("4"),
		Yellow:    lipgloss.Color("3"),
		Track:     lipgloss.Color("8"),
		Accent:    lipgloss.Color("6"),
		Secondary: lipgloss.Color("14"),
	},
}

var noColorPalette = Palette{
	Name:      "no-color",
	Bg:        lipgloss.Color(""),
	Panel:     lipgloss.Color(""),
	Surf:      lipgloss.Color(""),
	SurfHi:    lipgloss.Color("8"),
	Border:    lipgloss.Color("8"),
	Rule:      lipgloss.Color("8"),
	Text:      lipgloss.Color("7"),
	White:     lipgloss.Color("15"),
	Muted:     lipgloss.Color("7"),
	Dim:       lipgloss.Color("8"),
	Red:       lipgloss.Color("8"),
	Live:      lipgloss.Color("8"),
	Blue:      lipgloss.Color("15"),
	Yellow:    lipgloss.Color("15"),
	Track:     lipgloss.Color("8"),
	Accent:    lipgloss.Color("15"),
	Secondary: lipgloss.Color("7"),
}

// Palettes returns the ordered list of built-in palette names.
func Palettes() []string {
	out := make([]string, len(paletteOrder))
	copy(out, paletteOrder)
	return out
}

// ByName returns the named palette (case-insensitive, whitespace-trimmed),
// falling back to "youtube-dark". When NO_COLOR is non-empty, a monochrome
// high-contrast palette named "no-color" is returned.
func ByName(name string) Palette {
	if os.Getenv("NO_COLOR") != "" {
		return noColorPalette
	}
	norm := strings.ToLower(strings.TrimSpace(name))
	if p, ok := palettesByName[norm]; ok {
		return p
	}
	return palettesByName["youtube-dark"]
}

// NextTheme rotates across Palettes() starting from current.
// Unknown theme names rotate back to the default "youtube-dark" palette.
func NextTheme(current string) string {
	norm := strings.ToLower(strings.TrimSpace(current))
	for i, name := range paletteOrder {
		if name == norm {
			return paletteOrder[(i+1)%len(paletteOrder)]
		}
	}
	return paletteOrder[0]
}

// ThemeFromPalette constructs a complete Theme using the provided Palette.
func ThemeFromPalette(p Palette) Theme {
	sidebarBase := lipgloss.NewStyle().
		Foreground(p.Muted).
		Padding(0, 1)
	if p.Name != "terminal" && p.Name != "no-color" {
		sidebarBase = sidebarBase.Background(p.Bg)
	}
	chipBase := lipgloss.NewStyle().
		Foreground(p.Text).
		Padding(0, 1)
	if p.Name != "terminal" && p.Name != "no-color" {
		chipBase = chipBase.Background(p.Surf)
	} else {
		chipBase = chipBase.Background(lipgloss.Color("0"))
	}
	badgeDur := lipgloss.NewStyle().
		Foreground(p.White).
		Padding(0, 1)
	if p.Name != "terminal" && p.Name != "no-color" {
		badgeDur = badgeDur.Background(p.Bg)
	}
	return Theme{
		HeaderLogo: lipgloss.NewStyle().
			Foreground(p.White).
			Background(p.Red).
			Bold(true).
			Padding(0, 1),
		SearchBox: lipgloss.NewStyle().
			Background(p.Surf).
			Foreground(p.Muted).
			Padding(0, 1),
		SearchBoxFocused: lipgloss.NewStyle().
			Background(p.SurfHi).
			Foreground(p.White).
			Padding(0, 1),
		Chip: chipBase,
		ChipActive: lipgloss.NewStyle().
			Background(p.White).
			Foreground(p.Bg).
			Bold(true).
			Padding(0, 1),
		Card: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(p.Border).
			Foreground(p.Text).
			Padding(0, 1),
		CardSelected: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(p.Accent).
			Foreground(p.White).
			Padding(0, 1),
		BadgeLive: lipgloss.NewStyle().
			Background(p.Live).
			Foreground(p.White).
			Bold(true).
			Padding(0, 1),
		BadgeDuration: badgeDur,
		StatusBadge: lipgloss.NewStyle().
			Background(p.SurfHi).
			Foreground(p.Muted).
			Padding(0, 1),
		Toast: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(p.Border).
			Background(p.Surf).
			Foreground(p.White).
			Padding(0, 1),
		SidebarItem: sidebarBase,
		SidebarItemActive: lipgloss.NewStyle().
			Foreground(p.White).
			Background(p.Surf).
			Bold(true).
			Padding(0, 1),
	}
}
