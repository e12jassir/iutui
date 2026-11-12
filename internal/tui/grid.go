package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/e12jassir/ytx/internal/model"
)

// GridState tracks layout and viewport parameters anchored to SelectedIdx.
type GridState struct {
	Videos      []model.Video
	SelectedIdx int
	ScrollRow   int
	Width       int
	Height      int
}

// NewGridState initializes grid state with videos.
func NewGridState(videos []model.Video) GridState {
	return GridState{
		Videos:      videos,
		SelectedIdx: 0,
		ScrollRow:   0,
	}
}

// CalcCols computes how many cards fit horizontally in the available grid width.
func CalcCols(availableWidth int) int {
	innerW := availableWidth - 2
	if innerW < CardTotalWidth {
		return 1
	}
	cols := (innerW + 2) / (CardTotalWidth + 2) // includes horizontal gap
	if cols < 1 {
		return 1
	}
	return cols
}

// TotalRows returns the number of rows needed for the current item count and column width.
func (g GridState) TotalRows(cols int) int {
	if len(g.Videos) == 0 || cols <= 0 {
		return 0
	}
	return (len(g.Videos) + cols - 1) / cols
}

// CurrentCoords returns (row, col) for the current SelectedIdx.
func (g GridState) CurrentCoords(cols int) (int, int) {
	if len(g.Videos) == 0 || cols <= 0 {
		return 0, 0
	}
	return g.SelectedIdx / cols, g.SelectedIdx % cols
}

// MoveLeft moves selection 1 card to the left.
func (g *GridState) MoveLeft() {
	if g.SelectedIdx > 0 {
		g.SelectedIdx--
	}
}

// MoveRight moves selection 1 card to the right.
func (g *GridState) MoveRight() {
	if g.SelectedIdx < len(g.Videos)-1 {
		g.SelectedIdx++
	}
}

// MoveUp moves selection up by one row (by cols).
func (g *GridState) MoveUp(cols int) {
	if cols <= 0 {
		cols = 1
	}
	target := g.SelectedIdx - cols
	if target >= 0 {
		g.SelectedIdx = target
	}
}

// MoveDown moves selection down by one row (by cols).
func (g *GridState) MoveDown(cols int) {
	if cols <= 0 {
		cols = 1
	}
	target := g.SelectedIdx + cols
	if target < len(g.Videos) {
		g.SelectedIdx = target
	} else if g.SelectedIdx < len(g.Videos)-1 {
		// Jump to the last item if in the bottom incomplete row
		g.SelectedIdx = len(g.Videos) - 1
	}
}

// SelectedVideo returns the currently selected video if any exist.
func (g GridState) SelectedVideo() (model.Video, bool) {
	if len(g.Videos) == 0 || g.SelectedIdx < 0 || g.SelectedIdx >= len(g.Videos) {
		return model.Video{}, false
	}
	return g.Videos[g.SelectedIdx], true
}

// RenderGrid renders the 2D grid with responsive columns and viewport scrolling.
func RenderGrid(g *GridState, availableWidth, availableHeight int, hasFocus bool) string {
	return RenderGridPanel(g, "Videos", availableWidth, availableHeight, hasFocus)
}

// RenderGridPanel renders the 2D video card grid framed inside a Spotatui panel with scroll metrics.
func RenderGridPanel(g *GridState, headerTitle string, availableWidth, availableHeight int, hasFocus bool) string {
	if len(g.Videos) == 0 {
		emptyMsg := lipgloss.NewStyle().
			Foreground(colorCardDimText).
			Render("  No se encontraron videos en esta vista. Presiona '/' para buscar o 'r' para recargar.")
		return RenderPanel(headerTitle, []string{"", emptyMsg}, availableWidth, availableHeight, hasFocus)
	}

	cols := CalcCols(availableWidth)
	totalRows := g.TotalRows(cols)
	currRow, _ := g.CurrentCoords(cols)

	// Invariant check: Ensure SelectedIdx is within valid bounds
	if g.SelectedIdx >= len(g.Videos) {
		g.SelectedIdx = len(g.Videos) - 1
	}
	if g.SelectedIdx < 0 {
		g.SelectedIdx = 0
	}

	innerHeight := availableHeight - 2
	if innerHeight < CardTotalHeight {
		innerHeight = CardTotalHeight
	}

	// Approximate rows visible given available inner height
	rowRenderHeight := CardTotalHeight + 1
	visibleRows := (innerHeight + 1) / rowRenderHeight
	if visibleRows < 1 {
		visibleRows = 1
	}

	// Adjust ScrollRow viewport so currRow is always visible
	if currRow < g.ScrollRow {
		g.ScrollRow = currRow
	} else if currRow >= g.ScrollRow+visibleRows {
		g.ScrollRow = currRow - visibleRows + 1
	}
	if g.ScrollRow < 0 {
		g.ScrollRow = 0
	}

	endRow := g.ScrollRow + visibleRows
	if endRow > totalRows {
		endRow = totalRows
	}

	var renderedRows []string
	for r := g.ScrollRow; r < endRow; r++ {
		var rowCards []string
		for c := 0; c < cols; c++ {
			idx := r*cols + c
			if idx < len(g.Videos) {
				isSelected := (idx == g.SelectedIdx)
				if len(rowCards) > 0 {
					rowCards = append(rowCards, "  ") // Horizontal card gap
				} else {
					rowCards = append(rowCards, " ") // Left padding inside panel
				}
				rowCards = append(rowCards, RenderCard(g.Videos[idx], isSelected))
			}
		}
		if len(rowCards) > 0 {
			renderedRows = append(renderedRows, lipgloss.JoinHorizontal(lipgloss.Top, rowCards...))
		}
	}

	gridContent := strings.Join(renderedRows, "\n\n")
	contentLines := strings.Split(gridContent, "\n")

	panelTitle := fmt.Sprintf(" %s (%d/%d) • Fila %d/%d ", headerTitle, g.SelectedIdx+1, len(g.Videos), currRow+1, totalRows)
	return RenderPanel(panelTitle, contentLines, availableWidth, availableHeight, hasFocus)
}
