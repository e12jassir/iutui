package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/e12jassir/ytx-cli/internal/data"
	"github.com/e12jassir/ytx-cli/internal/model"
	"github.com/e12jassir/ytx-cli/internal/tui"
)

func main() {
	feeds := make(map[model.Tab][]model.Video)
	// Load Home feed first so initial startup is fast; other tabs load on demand or from cache
	if vids, err := data.LoadFeed(model.TabHome); err == nil {
		feeds[model.TabHome] = vids
	}

	p := tea.NewProgram(
		tui.InitialModel(feeds),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error ejecutando ytx-cli TUI: %v\n", err)
		os.Exit(1)
	}
}
