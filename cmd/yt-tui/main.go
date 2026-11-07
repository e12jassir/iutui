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
	tabs := []model.Tab{
		model.TabHome,
		model.TabMusic,
		model.TabSubscriptions,
		model.TabDownloads,
		model.TabHistory,
	}

	feeds := make(map[model.Tab][]model.Video)
	for _, t := range tabs {
		vids, _ := data.LoadFeed(t)
		feeds[t] = vids
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
