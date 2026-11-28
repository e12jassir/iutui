package tui

import "testing"

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
