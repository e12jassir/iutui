package theme

import (
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
		name  string
		style lipgloss.Style
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
