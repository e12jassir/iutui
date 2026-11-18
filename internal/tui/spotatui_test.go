package tui

import (
	"bytes"
	"image/png"
	"testing"
)

func TestEmbeddedBrandLogoPNG(t *testing.T) {
	if len(embeddedBrandLogoPNG) == 0 {
		t.Fatal("embeddedBrandLogoPNG must not be empty")
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(embeddedBrandLogoPNG))
	if err != nil {
		t.Fatalf("png.DecodeConfig failed: %v", err)
	}
	if cfg.Width != 520 || cfg.Height != 120 {
		t.Fatalf("embedded brand logo dimensions = %dx%d, want 520x120", cfg.Width, cfg.Height)
	}
	got, err := renderYouTubeBrandPNG()
	if err != nil || len(got) == 0 {
		t.Fatalf("renderYouTubeBrandPNG() = (%d bytes, %v), want non-empty nil err", len(got), err)
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
