package theme

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestPaletteColors(t *testing.T) {
	tests := []struct {
		name string
		got  lipgloss.Color
		want string
	}{
		{name: "Bg", got: Bg, want: "#0f0f0f"},
		{name: "Panel", got: Panel, want: "#181818"},
		{name: "Surf", got: Surf, want: "#272727"},
		{name: "SurfHi", got: SurfHi, want: "#333333"},
		{name: "Border", got: Border, want: "#3f3f3f"},
		{name: "Rule", got: Rule, want: "#262626"},
		{name: "Text", got: Text, want: "#f1f1f1"},
		{name: "White", got: White, want: "#ffffff"},
		{name: "Muted", got: Muted, want: "#aaaaaa"},
		{name: "Dim", got: Dim, want: "#717171"},
		{name: "Red", got: Red, want: "#ff0033"},
		{name: "Live", got: Live, want: "#cc0000"},
		{name: "Blue", got: Blue, want: "#3ea6ff"},
		{name: "Yellow", got: Yellow, want: "#f5c518"},
		{name: "Track", got: Track, want: "#3a3a3a"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if string(tc.got) != tc.want {
				t.Fatalf("color %s = %q, want %q", tc.name, tc.got, tc.want)
			}
		})
	}
}

func TestDefaultTheme(t *testing.T) {
	th := DefaultTheme()

	styles := []struct {
		name   string
		style  lipgloss.Style
		sample string
	}{
		{name: "HeaderLogo", style: th.HeaderLogo, sample: "ytx"},
		{name: "SearchBox", style: th.SearchBox, sample: "search"},
		{name: "SearchBoxFocused", style: th.SearchBoxFocused, sample: "search"},
		{name: "Chip", style: th.Chip, sample: "All"},
		{name: "ChipActive", style: th.ChipActive, sample: "All"},
		{name: "Card", style: th.Card, sample: "Video"},
		{name: "CardSelected", style: th.CardSelected, sample: "Video"},
		{name: "BadgeLive", style: th.BadgeLive, sample: "LIVE"},
		{name: "BadgeDuration", style: th.BadgeDuration, sample: "12:34"},
		{name: "StatusBadge", style: th.StatusBadge, sample: "HD"},
		{name: "Toast", style: th.Toast, sample: "Copied"},
		{name: "SidebarItem", style: th.SidebarItem, sample: "Home"},
		{name: "SidebarItemActive", style: th.SidebarItemActive, sample: "Home"},
	}

	for _, tc := range styles {
		t.Run(tc.name, func(t *testing.T) {
			rendered := tc.style.Render(tc.sample)
			if rendered == "" {
				t.Fatalf("%s.Render(%q) returned empty string", tc.name, tc.sample)
			}
			if !strings.Contains(rendered, tc.sample) {
				t.Fatalf("%s.Render(%q) = %q, expected to contain sample text", tc.name, tc.sample, rendered)
			}
		})
	}

	t.Run("DistinctStateColors", func(t *testing.T) {
		if th.SearchBox.GetBackground() == th.SearchBoxFocused.GetBackground() {
			t.Errorf("SearchBox and SearchBoxFocused should have distinct background colors")
		}
		if th.Chip.GetBackground() == th.ChipActive.GetBackground() {
			t.Errorf("Chip and ChipActive should have distinct background colors")
		}
		if th.Card.GetBorderTopForeground() == th.CardSelected.GetBorderTopForeground() {
			t.Errorf("Card and CardSelected should have distinct border colors")
		}
		if th.SidebarItem.GetForeground() == th.SidebarItemActive.GetForeground() {
			t.Errorf("SidebarItem and SidebarItemActive should have distinct foreground colors")
		}
	})
}

func TestPalettesOrder(t *testing.T) {
	want := []string{"youtube-dark", "catppuccin", "tokyonight", "gruvbox", "kanagawa"}
	got := Palettes()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Palettes() = %v, want %v", got, want)
	}

	// Ensure returned slice is a defensive copy.
	got[0] = "mutated"
	if again := Palettes(); !reflect.DeepEqual(again, want) {
		t.Fatalf("Palettes() mutated after modifying returned slice: %v", again)
	}
}

func TestByNameAndThemeFromPalette(t *testing.T) {
	t.Setenv("NO_COLOR", "")

	for _, name := range Palettes() {
		t.Run("palette/"+name, func(t *testing.T) {
			p := ByName(name)
			if p.Name != name {
				t.Fatalf("ByName(%q).Name = %q, want %q", name, p.Name, name)
			}
			for _, field := range []struct {
				label string
				val   lipgloss.Color
			}{
				{"Bg", p.Bg},
				{"Panel", p.Panel},
				{"Surf", p.Surf},
				{"SurfHi", p.SurfHi},
				{"Border", p.Border},
				{"Rule", p.Rule},
				{"Text", p.Text},
				{"White", p.White},
				{"Muted", p.Muted},
				{"Dim", p.Dim},
				{"Red", p.Red},
				{"Live", p.Live},
				{"Blue", p.Blue},
				{"Yellow", p.Yellow},
				{"Track", p.Track},
				{"Accent", p.Accent},
				{"Secondary", p.Secondary},
			} {
				if string(field.val) == "" {
					t.Errorf("palette %q field %s is empty", name, field.label)
				}
			}

			th := ThemeFromPalette(p)
			if th.CardSelected.GetBorderTopForeground() != p.Accent {
				t.Errorf("CardSelected border = %v, want %v", th.CardSelected.GetBorderTopForeground(), p.Accent)
			}
			if th.HeaderLogo.GetBackground() != p.Red {
				t.Errorf("HeaderLogo bg = %v, want %v", th.HeaderLogo.GetBackground(), p.Red)
			}
			if th.SearchBox.GetBackground() == th.SearchBoxFocused.GetBackground() {
				t.Errorf("SearchBox and SearchBoxFocused share background in %q", name)
			}
			if th.Chip.GetBackground() == th.ChipActive.GetBackground() {
				t.Errorf("Chip and ChipActive share background in %q", name)
			}
			if th.Card.GetBorderTopForeground() == th.CardSelected.GetBorderTopForeground() {
				t.Errorf("Card and CardSelected share border in %q", name)
			}
			if th.SidebarItem.GetForeground() == th.SidebarItemActive.GetForeground() {
				t.Errorf("SidebarItem and SidebarItemActive share foreground in %q", name)
			}
			if rendered := th.HeaderLogo.Render("ytx"); !strings.Contains(rendered, "ytx") {
				t.Errorf("HeaderLogo render = %q, expected to contain ytx", rendered)
			}
		})
	}

	t.Run("normalizes whitespace and case", func(t *testing.T) {
		p := ByName("  Catppuccin \n")
		if p.Name != "catppuccin" {
			t.Fatalf("ByName normalized = %q, want catppuccin", p.Name)
		}
	})

	t.Run("unknown falls back to youtube-dark", func(t *testing.T) {
		p := ByName("nonexistent-theme")
		if p.Name != "youtube-dark" {
			t.Fatalf("ByName(unknown).Name = %q, want youtube-dark", p.Name)
		}
	})
}

func TestNextTheme(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "youtube-dark", want: "catppuccin"},
		{in: "catppuccin", want: "tokyonight"},
		{in: "tokyonight", want: "gruvbox"},
		{in: "gruvbox", want: "kanagawa"},
		{in: "kanagawa", want: "youtube-dark"},
		{in: "  Catppuccin  ", want: "tokyonight"},
		{in: "unknown", want: "youtube-dark"},
		{in: "", want: "youtube-dark"},
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			if got := NextTheme(tc.in); got != tc.want {
				t.Fatalf("NextTheme(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNoColorOverride(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	p := ByName("catppuccin")
	if p.Name != "no-color" {
		t.Fatalf("ByName with NO_COLOR set = %q, want no-color", p.Name)
	}
	if p.Bg != lipgloss.Color("") || p.Panel != lipgloss.Color("") {
		t.Fatalf("expected default terminal Bg/Panel under NO_COLOR, got Bg=%q Panel=%q", p.Bg, p.Panel)
	}
	if p.Red == p.White {
		t.Fatalf("expected contrasting Red (%q) and White (%q) under NO_COLOR", p.Red, p.White)
	}

	th := ThemeFromPalette(p)
	if th.Card.GetBorderTopForeground() == th.CardSelected.GetBorderTopForeground() {
		t.Errorf("Card and CardSelected should remain distinct under NO_COLOR")
	}
	if th.SearchBox.GetBackground() == th.SearchBoxFocused.GetBackground() {
		t.Errorf("SearchBox and SearchBoxFocused should remain distinct under NO_COLOR")
	}
	if rendered := th.HeaderLogo.Render("ytx"); !strings.Contains(rendered, "ytx") {
		t.Errorf("HeaderLogo.Render failed under NO_COLOR: %q", rendered)
	}
}
