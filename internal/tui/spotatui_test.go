package tui

import (
	"strings"
	"testing"

	"github.com/e12jassir/ytx/internal/model"
)

func TestRenderPanelBordersAndTitle(t *testing.T) {
	title := "Search"
	content := []string{"dond ese aprende"}
	width := 30
	height := 3

	// Unfocused
	outDim := RenderPanel(title, content, width, height, false)
	if !strings.Contains(outDim, "Search") {
		t.Fatalf("expected title 'Search' in panel, got:\n%s", outDim)
	}
	if !strings.HasPrefix(outDim, "╭Search") && !strings.Contains(outDim, "Search") {
		t.Fatalf("expected top border starting with '╭', got:\n%s", outDim)
	}
	if !strings.Contains(outDim, "dond ese aprende") {
		t.Fatalf("expected content in body, got:\n%s", outDim)
	}

	// Focused
	outFocused := RenderPanel(title, content, width, height, true)
	if !strings.Contains(outFocused, "Search") {
		t.Fatalf("expected title in focused panel")
	}
}

func TestTableStateScrolling(t *testing.T) {
	vids := makeTestVideos(20)
	ts := NewTableState(vids)

	if ts.SelectedIdx != 0 {
		t.Errorf("expected initial selected 0, got %d", ts.SelectedIdx)
	}

	visHeight := 5
	// Move down 6 times
	for i := 0; i < 6; i++ {
		ts.MoveDown(visHeight)
	}
	if ts.SelectedIdx != 6 {
		t.Errorf("expected selected 6, got %d", ts.SelectedIdx)
	}
	if ts.ScrollOffset != 2 {
		t.Errorf("expected scroll offset 2 (6 - 5 + 1), got %d", ts.ScrollOffset)
	}

	// Move up
	ts.MoveUp(visHeight)
	if ts.SelectedIdx != 5 {
		t.Errorf("expected selected 5, got %d", ts.SelectedIdx)
	}

	// Direct selection
	ts.SelectDirect(0, visHeight)
	if ts.SelectedIdx != 0 || ts.ScrollOffset != 0 {
		t.Errorf("expected reset to top, got selected=%d offset=%d", ts.SelectedIdx, ts.ScrollOffset)
	}
}

func TestRenderTableContent(t *testing.T) {
	vids := []model.Video{
		{ID: "1", Title: "La Tortura", Channel: "Shakira", Duration: "3:36"},
		{ID: "2", Title: "DÓNDE", Channel: "Kapo", Duration: "3:13"},
	}
	ts := NewTableState(vids)

	lines := RenderTableContent(&ts, 80, 10, true, "1")
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines, got %d", len(lines))
	}

	// First line should be header
	if !strings.Contains(lines[0], "Title") || !strings.Contains(lines[0], "Artist / Channel") {
		t.Errorf("expected header row, got: %s", lines[0])
	}

	// Check if La Tortura is rendered
	found := false
	for _, l := range lines {
		if strings.Contains(l, "La Tortura") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected track 'La Tortura' in table lines")
	}
}


func TestModelViewSpotatuiLayout(t *testing.T) {
	feeds := map[model.Tab][]model.Video{
		model.TabHome: {
			{ID: "vid1", Title: "Jeremías 17:5", Channel: "Canserbero", Duration: "5:18"},
			{ID: "vid2", Title: "Mujer Amante", Channel: "Rata Blanca", Duration: "6:05"},
		},
	}
	m := InitialModel(feeds)
	m.Width = 100
	m.Height = 30
	m.Ready = true

	// Render default view
	view := m.View()

	// Should contain top boxes
	if !strings.Contains(view, "Buscar") || !strings.Contains(view, "Ayuda") || !strings.Contains(view, "Ajustes") {
		t.Errorf("expected Buscar, Ayuda, and Ajustes in top bar, got:\n%s", view)
	}

	// Should contain YouTube drawer
	if !strings.Contains(view, "YouTube") || !strings.Contains(view, "PRINCIPAL") {
		t.Errorf("expected YouTube drawer in sidebar, got:\n%s", view)
	}

	// Should contain video titles
	if !strings.Contains(view, "Jeremías 17:5") || !strings.Contains(view, "Rata Blanca") {
		t.Errorf("expected video titles in cards, got:\n%s", view)
	}

	// Check if playing footer is rendered
	if !strings.Contains(view, "Reproductor") || !strings.Contains(view, "[⏯ Pausa]") {
		t.Errorf("expected playing footer, got:\n%s", view)
	}
	t.Logf("\nRendered YouTube Web Terminal View:\n%s\n", view)
}

func TestTruncateANSIPreservesVisibleWidth(t *testing.T) {
	styled := "\x1b[38;2;189;147;249mHola mundo largo con colores ANSI\x1b[0m"
	out := truncateANSI(styled, 10)
	visible := stripANSI(out)
	if len(visible) != 10 {
		t.Errorf("expected visible length 10, got %d (%q)", len(visible), visible)
	}
	if !strings.HasSuffix(out, "\x1b[0m") && !strings.Contains(out, "\x1b[0m") {
		t.Errorf("expected ANSI reset sequence in truncated string, got %q", out)
	}
}

func TestCleanThumbIDStripsMixPrefix(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"Standard video ID", "hgUyIeEJQrA", "hgUyIeEJQrA"},
		{"Mix ID with RD prefix", "RDOgeR2oqZGTs", "OgeR2oqZGTs"},
		{"Short ID untouched", "RD123", "RD123"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cleanThumbID(tt.in); got != tt.want {
				t.Errorf("cleanThumbID(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
