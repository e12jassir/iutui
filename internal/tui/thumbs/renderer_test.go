package thumbs

import (
	"strings"
	"testing"
)

func TestDetectBackend(t *testing.T) {
	tests := []struct {
		name        string
		ytPreview   string
		kittyWinID  string
		term        string
		termProgram string
		want        Backend
	}{
		{
			name: "all empty defaults to text",
			want: BackendText,
		},
		{
			name:       "KITTY_WINDOW_ID selects kitty",
			kittyWinID: "1",
			want:       BackendKitty,
		},
		{
			name: "TERM=xterm-kitty selects kitty",
			term: "xterm-kitty",
			want: BackendKitty,
		},
		{
			name:        "TERM_PROGRAM=ghostty selects kitty",
			termProgram: "ghostty",
			want:        BackendKitty,
		},
		{
			name:        "case-insensitive ghostty",
			termProgram: "Ghostty",
			want:        BackendKitty,
		},
		{
			name:       "YT_PREVIEW=none overrides kitty",
			ytPreview:  "none",
			kittyWinID: "42",
			term:       "xterm-kitty",
			want:       BackendText,
		},
		{
			name:       "YT_PREVIEW=text overrides kitty",
			ytPreview:  "text",
			kittyWinID: "42",
			want:       BackendText,
		},
		{
			name:       "YT_PREVIEW=chafa overrides kitty",
			ytPreview:  "chafa",
			kittyWinID: "42",
			want:       BackendChafa,
		},
		{
			name:      "YT_PREVIEW=kitty forces kitty",
			ytPreview: "kitty",
			term:      "xterm-256color",
			want:      BackendKitty,
		},
		{
			name:      "YT_PREVIEW trims whitespace and ignores case",
			ytPreview: "  CHAFA ",
			want:      BackendChafa,
		},
		{
			name:        "alacritty and tmux default to chafa",
			term:        "alacritty",
			termProgram: "tmux",
			want:        BackendChafa,
		},
		{
			name: "plain dumb terminal defaults to text",
			term: "dumb",
			want: BackendText,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DetectBackend(tc.ytPreview, tc.kittyWinID, tc.term, tc.termProgram)
			if got != tc.want {
				t.Fatalf("DetectBackend(%q, %q, %q, %q) = %v, want %v",
					tc.ytPreview, tc.kittyWinID, tc.term, tc.termProgram, got, tc.want)
			}
		})
	}
}

func TestDetectBackendEnv(t *testing.T) {
	tests := []struct {
		name        string
		ytPreview   string
		kittyWinID  string
		term        string
		termProgram string
		colorTerm   string
		noColor     string
		want        Backend
	}{
		{
			name: "all empty defaults to text",
			want: BackendText,
		},
		{
			name: "TERM=dumb falls back to text",
			term: "dumb",
			want: BackendText,
		},
		{
			name: "TERM=linux falls back to text",
			term: "linux",
			want: BackendText,
		},
		{
			name: "TERM=vt100 falls back to text",
			term: "vt100",
			want: BackendText,
		},
		{
			name: "TERM=vt220 falls back to text",
			term: "vt220",
			want: BackendText,
		},
		{
			name: "TERM=cons25 falls back to text",
			term: "cons25",
			want: BackendText,
		},
		{
			name: "unknown plain TERM falls back to text",
			term: "vt52",
			want: BackendText,
		},
		{
			name:        "NO_COLOR overrides COLORTERM=truecolor and TERM_PROGRAM",
			term:        "xterm-256color",
			termProgram: "WezTerm",
			colorTerm:   "truecolor",
			noColor:     "1",
			want:        BackendText,
		},
		{
			name:      "COLORTERM=truecolor selects chafa",
			colorTerm: "truecolor",
			want:      BackendChafa,
		},
		{
			name:      "COLORTERM=24bit selects chafa",
			colorTerm: "24bit",
			want:      BackendChafa,
		},
		{
			name: "TERM=alacritty selects chafa",
			term: "alacritty",
			want: BackendChafa,
		},
		{
			name: "TERM=xterm-256color selects chafa",
			term: "xterm-256color",
			want: BackendChafa,
		},
		{
			name: "TERM=foot selects chafa",
			term: "foot",
			want: BackendChafa,
		},
		{
			name: "TERM=tmux-256color selects chafa",
			term: "tmux-256color",
			want: BackendChafa,
		},
		{
			name:        "TERM_PROGRAM=WezTerm selects chafa",
			termProgram: "WezTerm",
			want:        BackendChafa,
		},
		{
			name:        "TERM_PROGRAM=iTerm.app selects chafa",
			termProgram: "iTerm.app",
			want:        BackendChafa,
		},
		{
			name:      "YT_PREVIEW=blocks selects chafa",
			ytPreview: "blocks",
			want:      BackendChafa,
		},
		{
			name:      "YT_PREVIEW=ansi selects chafa",
			ytPreview: "ansi",
			want:      BackendChafa,
		},
		{
			name:      "YT_PREVIEW=chafa selects chafa",
			ytPreview: "chafa",
			want:      BackendChafa,
		},
		{
			name:       "YT_PREVIEW=none overrides COLORTERM=truecolor and kitty",
			ytPreview:  "none",
			kittyWinID: "1",
			term:       "xterm-kitty",
			colorTerm:  "truecolor",
			want:       BackendText,
		},
		{
			name:        "TERM_PROGRAM=ghostty selects kitty",
			termProgram: "ghostty",
			colorTerm:   "truecolor",
			want:        BackendKitty,
		},
		{
			name:       "KITTY_WINDOW_ID=1 selects kitty",
			kittyWinID: "1",
			colorTerm:  "truecolor",
			want:       BackendKitty,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DetectBackendEnv(tc.ytPreview, tc.kittyWinID, tc.term, tc.termProgram, tc.colorTerm, tc.noColor)
			if got != tc.want {
				t.Fatalf("DetectBackendEnv(%q, %q, %q, %q, %q, %q) = %v, want %v",
					tc.ytPreview, tc.kittyWinID, tc.term, tc.termProgram, tc.colorTerm, tc.noColor, got, tc.want)
			}
		})
	}
}

func TestTextRenderer(t *testing.T) {
	var r Renderer = NewTextRenderer()

	if r.Backend() != BackendText {
		t.Fatalf("Backend() = %v, want %v", r.Backend(), BackendText)
	}

	// Prefetch should be a safe no-op.
	r.Prefetch("dQw4w9WgXcQ")

	t.Run("deterministic box dimensions and label", func(t *testing.T) {
		out1 := r.Render("abc123", 12, 5)
		out2 := r.Render("abc123", 12, 5)
		if out1 != out2 {
			t.Fatalf("Render is not deterministic:\n%s\nvs\n%s", out1, out2)
		}
		lines := strings.Split(out1, "\n")
		if len(lines) != 5 {
			t.Fatalf("got %d lines, want 5", len(lines))
		}
		if !strings.Contains(out1, "abc123") {
			t.Fatalf("expected output to contain video id, got:\n%s", out1)
		}
	})

	t.Run("truncates long id to fit inner width", func(t *testing.T) {
		out := r.Render("very-long-video-id", 8, 3)
		lines := strings.Split(out, "\n")
		if len(lines) != 3 {
			t.Fatalf("got %d lines, want 3", len(lines))
		}
		if !strings.Contains(lines[1], "very-l") {
			t.Fatalf("expected truncated id in middle row, got %q", lines[1])
		}
	})

	t.Run("empty id uses fallback label", func(t *testing.T) {
		out := r.Render("", 11, 3)
		if !strings.Contains(out, "[thumb]") {
			t.Fatalf("expected fallback label [thumb], got:\n%s", out)
		}
	})

	t.Run("non-positive dimensions return empty", func(t *testing.T) {
		if got := r.Render("id", 0, 5); got != "" {
			t.Fatalf("Render width=0 = %q, want empty", got)
		}
		if got := r.Render("id", 5, 0); got != "" {
			t.Fatalf("Render height=0 = %q, want empty", got)
		}
	})

	t.Run("minimal 1x1 dimensions", func(t *testing.T) {
		if got := r.Render("id", 1, 1); got != "·" {
			t.Fatalf("Render 1x1 = %q, want %q", got, "·")
		}
	})
}
