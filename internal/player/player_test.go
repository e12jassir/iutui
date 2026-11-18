package player

import (
	"fmt"
	"os"
	"path/filepath"
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

func TestFindSocket_PrefersIutuiRuntimeSocket(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("YT_SOCK", "")
	t.Setenv("XDG_RUNTIME_DIR", tmpDir)

	legacySock := filepath.Join(tmpDir, fmt.Sprintf("yt-%d", os.Getuid()), "mpv.sock")
	if err := os.MkdirAll(filepath.Dir(legacySock), 0o700); err != nil {
		t.Fatalf("mkdir legacy runtime dir: %v", err)
	}
	if err := os.WriteFile(legacySock, nil, 0o600); err != nil {
		t.Fatalf("write legacy socket placeholder: %v", err)
	}

	iutuiSock := filepath.Join(tmpDir, "iutui", "mpv.sock")
	if err := os.MkdirAll(filepath.Dir(iutuiSock), 0o700); err != nil {
		t.Fatalf("mkdir iutui runtime dir: %v", err)
	}
	if err := os.WriteFile(iutuiSock, nil, 0o600); err != nil {
		t.Fatalf("write iutui socket placeholder: %v", err)
	}

	if got := FindSocket(); got != iutuiSock {
		t.Fatalf("FindSocket() = %q, want %q", got, iutuiSock)
	}
}
