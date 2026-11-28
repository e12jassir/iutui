// Package thumbs provides thumbnail rendering abstractions and backend detection
// for the iutui terminal user interface.
package thumbs

import (
	"strings"
	"unicode/utf8"
)

// Backend identifies the terminal image rendering strategy.
type Backend int

const (
	// BackendText renders deterministic text placeholder boxes.
	BackendText Backend = iota
	// BackendChafa renders thumbnails as native pure-Go 24-bit ANSI quadrant blocks (no external binary required).
	BackendChafa
	// BackendKitty renders thumbnails using the Kitty graphics protocol.
	BackendKitty
)

// String returns a human-readable name for the backend.
func (b Backend) String() string {
	switch b {
	case BackendKitty:
		return "kitty"
	case BackendChafa:
		return "chafa"
	case BackendText:
		return "text"
	default:
		return "unknown"
	}
}

// Renderer renders video thumbnails for a given video ID and cell dimensions.
type Renderer interface {
	// Render returns a terminal-ready representation of the thumbnail for id
	// sized to width columns by height rows.
	Render(id string, width, height int) string
	// Prefetch warms any underlying cache for the given video id.
	Prefetch(id string)
	// Backend returns the rendering backend used by this Renderer.
	Backend() Backend
}

// DetectBackend determines the thumbnail rendering backend from environment values.
// Explicit ytPreviewEnv ("none", "text", "chafa", "ansi", "blocks", "kitty") takes
// precedence over terminal auto-detection via kittyWinID, term, and termProgram.
func DetectBackend(ytPreviewEnv, kittyWinID, term, termProgram string) Backend {
	return DetectBackendEnv(ytPreviewEnv, kittyWinID, term, termProgram, "", "")
}

// DetectBackendEnv determines the thumbnail rendering backend from environment values,
// including COLORTERM and NO_COLOR.
func DetectBackendEnv(ytPreviewEnv, kittyWinID, term, termProgram, colorTerm, noColor string) Backend {
	switch strings.ToLower(strings.TrimSpace(ytPreviewEnv)) {
	case "none", "text":
		return BackendText
	case "chafa", "ansi", "blocks":
		return BackendChafa
	case "kitty":
		return BackendKitty
	}

	termTrimmed := strings.TrimSpace(term)
	termProgramTrimmed := strings.TrimSpace(termProgram)
	if strings.TrimSpace(kittyWinID) != "" ||
		strings.EqualFold(termTrimmed, "xterm-kitty") ||
		strings.EqualFold(termProgramTrimmed, "ghostty") {
		return BackendKitty
	}

	if strings.TrimSpace(noColor) != "" {
		return BackendText
	}

	colorTermTrimmed := strings.TrimSpace(colorTerm)
	if termTrimmed == "" && termProgramTrimmed == "" && colorTermTrimmed == "" {
		return BackendText
	}

	termLower := strings.ToLower(termTrimmed)
	switch termLower {
	case "dumb", "linux", "vt100", "vt220", "cons25":
		return BackendText
	}

	colorTermLower := strings.ToLower(colorTermTrimmed)
	if strings.Contains(colorTermLower, "truecolor") || strings.Contains(colorTermLower, "24bit") {
		return BackendChafa
	}

	if termProgramTrimmed != "" {
		return BackendChafa
	}

	for _, tok := range []string{
		"256color", "truecolor", "direct",
		"alacritty", "foot", "wezterm",
		"tmux", "screen", "xterm", "rxvt",
		"konsole", "gnome", "vte",
	} {
		if strings.Contains(termLower, tok) {
			return BackendChafa
		}
	}

	return BackendText
}

// TextRenderer renders deterministic ASCII/Unicode placeholder boxes for
// non-graphical terminals and unit tests.
type TextRenderer struct{}

// NewTextRenderer returns a new TextRenderer.
func NewTextRenderer() *TextRenderer {
	return &TextRenderer{}
}

// Backend returns BackendText.
func (r *TextRenderer) Backend() Backend {
	return BackendText
}

// Prefetch is a no-op for TextRenderer.
func (r *TextRenderer) Prefetch(string) {}

// Render returns a deterministic placeholder box of size width x height
// containing the video id (or "[thumb]" when id is empty) centered on the middle row.
func (r *TextRenderer) Render(id string, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	if width == 1 || height == 1 {
		row := strings.Repeat("·", width)
		rows := make([]string, height)
		for i := range rows {
			rows[i] = row
		}
		return strings.Join(rows, "\n")
	}

	innerW := width - 2
	innerH := height - 2
	label := strings.TrimSpace(id)
	if label == "" {
		label = "[thumb]"
	}
	label = centerLabel(label, innerW)

	top := "┌" + strings.Repeat("─", innerW) + "┐"
	bottom := "└" + strings.Repeat("─", innerW) + "┘"
	blank := "│" + strings.Repeat(" ", innerW) + "│"
	midRow := innerH / 2

	lines := make([]string, 0, height)
	lines = append(lines, top)
	for i := 0; i < innerH; i++ {
		if i == midRow {
			lines = append(lines, "│"+label+"│")
		} else {
			lines = append(lines, blank)
		}
	}
	lines = append(lines, bottom)
	return strings.Join(lines, "\n")
}

func centerLabel(label string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(label)
	if len(runes) > width {
		return string(runes[:width])
	}
	padTotal := width - utf8.RuneCountInString(label)
	left := padTotal / 2
	right := padTotal - left
	return strings.Repeat(" ", left) + label + strings.Repeat(" ", right)
}
