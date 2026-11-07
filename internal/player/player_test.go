package player

import (
	"testing"
)

func TestFormatTime(t *testing.T) {
	tests := []struct {
		seconds  float64
		expected string
	}{
		{0, "00:00"},
		{45, "00:45"},
		{125, "02:05"},
		{3661, "01:01:01"},
	}

	for _, tt := range tests {
		got := FormatTime(tt.seconds)
		if got != tt.expected {
			t.Errorf("FormatTime(%f) = %s; want %s", tt.seconds, got, tt.expected)
		}
	}
}

func TestProgressBar(t *testing.T) {
	bar := ProgressBar(50, 100, 10)
	if len(bar) == 0 {
		t.Fatalf("expected non-empty progress bar")
	}
	if bar != "━━━━━●────" {
		t.Errorf("unexpected progress bar: %s", bar)
	}
}
