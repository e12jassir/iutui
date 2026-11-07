package tui

import (
	"strings"
	"testing"

	"github.com/e12jassir/ytx-cli/internal/model"
)

func makeTestVideos(count int) []model.Video {
	videos := make([]model.Video, count)
	for i := 0; i < count; i++ {
		videos[i] = model.Video{
			ID:       string(rune('A' + i)),
			Title:    "Video Title",
			Channel:  "Test Channel",
			Duration: "03:45",
			Views:    "100K",
		}
	}
	return videos
}

func TestCalcCols(t *testing.T) {
	tests := []struct {
		name     string
		width    int
		expected int
	}{
		{"Too narrow defaults to 1", 20, 1},
		{"Exactly one card width", CardTotalWidth + 2, 1},
		{"Two cards", (CardTotalWidth+2)*2 + 5, 2},
		{"Three cards wide", (CardTotalWidth+2)*3 + 10, 3},
		{"Wide terminal", 200, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalcCols(tt.width)
			if got != tt.expected {
				t.Errorf("CalcCols(%d) = %d; want %d", tt.width, got, tt.expected)
			}
		})
	}
}

func TestGridNavigationBounds(t *testing.T) {
	videos := makeTestVideos(7) // 7 items: indices 0..6
	grid := NewGridState(videos)
	cols := 3 // 3 columns -> Row 0: 0, 1, 2 | Row 1: 3, 4, 5 | Row 2: 6

	// Start at 0
	if grid.SelectedIdx != 0 {
		t.Fatalf("expected initial index 0, got %d", grid.SelectedIdx)
	}

	// Move left at boundary (should not go negative)
	grid.MoveLeft()
	if grid.SelectedIdx != 0 {
		t.Errorf("expected index 0 after MoveLeft at left bound, got %d", grid.SelectedIdx)
	}

	// Move right
	grid.MoveRight()
	if grid.SelectedIdx != 1 {
		t.Errorf("expected index 1 after MoveRight, got %d", grid.SelectedIdx)
	}

	// Move down (1 + 3 = 4)
	grid.MoveDown(cols)
	if grid.SelectedIdx != 4 {
		t.Errorf("expected index 4 after MoveDown, got %d", grid.SelectedIdx)
	}

	// Move down again (4 + 3 = 7, out of bounds, should jump to last item 6)
	grid.MoveDown(cols)
	if grid.SelectedIdx != 6 {
		t.Errorf("expected index 6 (last item) after MoveDown, got %d", grid.SelectedIdx)
	}

	// Move down again at end of list (should stay at 6)
	grid.MoveDown(cols)
	if grid.SelectedIdx != 6 {
		t.Errorf("expected index 6, got %d", grid.SelectedIdx)
	}

	// Move up (6 - 3 = 3)
	grid.MoveUp(cols)
	if grid.SelectedIdx != 3 {
		t.Errorf("expected index 3 after MoveUp, got %d", grid.SelectedIdx)
	}

	// Move up to row 0 (3 - 3 = 0)
	grid.MoveUp(cols)
	if grid.SelectedIdx != 0 {
		t.Errorf("expected index 0 after MoveUp, got %d", grid.SelectedIdx)
	}

	// Move up at top boundary (should stay at 0)
	grid.MoveUp(cols)
	if grid.SelectedIdx != 0 {
		t.Errorf("expected index 0, got %d", grid.SelectedIdx)
	}
}

func TestResizePreservesSelectedIdxInvariant(t *testing.T) {
	videos := makeTestVideos(20)
	grid := NewGridState(videos)
	grid.SelectedIdx = 11 // Target video: 12th item

	// In wide terminal (4 columns)
	colsWide := 4
	rowWide, colWide := grid.CurrentCoords(colsWide)
	if rowWide != 2 || colWide != 3 {
		t.Errorf("Wide: coords for idx 11 with 4 cols = (%d, %d); want (2, 3)", rowWide, colWide)
	}

	// Terminal resized to narrow (2 columns)
	colsNarrow := 2
	rowNarrow, colNarrow := grid.CurrentCoords(colsNarrow)
	if rowNarrow != 5 || colNarrow != 1 {
		t.Errorf("Narrow: coords for idx 11 with 2 cols = (%d, %d); want (5, 1)", rowNarrow, colNarrow)
	}

	// Crucial invariant: SelectedIdx itself NEVER mutated due to resize
	if grid.SelectedIdx != 11 {
		t.Errorf("Invariant violated: SelectedIdx changed to %d on resize!", grid.SelectedIdx)
	}

	v, ok := grid.SelectedVideo()
	if !ok || v.ID != string(rune('A'+11)) {
		t.Errorf("Selected video changed or missing after resize!")
	}
}

func TestRenderCardThreeTierLayout(t *testing.T) {
	video := model.Video{
		ID:        "xyz123",
		Title:     "Testing Three Tier Card Layout",
		Channel:   "Go Channel",
		Duration:  "12:34",
		Views:     "50K",
		Published: "ayer",
	}

	cardUnselected := RenderCard(video, false)
	if !strings.Contains(cardUnselected, "xyz123") {
		t.Errorf("Card should contain video ID in thumbnail viewport")
	}
	if !strings.Contains(cardUnselected, "12:34") {
		t.Errorf("Card should contain duration badge")
	}
	if !strings.Contains(cardUnselected, "Testing Three") {
		t.Errorf("Card should contain title")
	}

	cardSelected := RenderCard(video, true)
	if !strings.Contains(cardSelected, "▶") {
		t.Errorf("Selected card should contain cursor indicator")
	}
}
