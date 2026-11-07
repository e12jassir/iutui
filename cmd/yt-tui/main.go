package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/e12jassir/ytx-cli/internal/model"
	"github.com/e12jassir/ytx-cli/internal/tui"
)

func main() {
	feeds := map[model.Tab][]model.Video{
		model.TabHome: {
			{
				ID:        "dQw4w9WgXcQ",
				Title:     "Rick Astley - Never Gonna Give You Up (Official Music Video)",
				Channel:   "Rick Astley",
				Duration:  "03:33",
				Views:     "1.5B vistas",
				Published: "hace 14 años",
			},
			{
				ID:        "9bZkp7q19f0",
				Title:     "PSY - GANGNAM STYLE(강남스타일) M/V",
				Channel:   "officialpsy",
				Duration:  "04:13",
				Views:     "5.1B vistas",
				Published: "hace 12 años",
			},
			{
				ID:        "live_stream_1",
				Title:     "lofi hip hop radio 📚 - beats to relax/study to",
				Channel:   "Lofi Girl",
				IsLive:    true,
				Views:     "24K espectadores",
				Published: "En vivo ahora",
			},
			{
				ID:        "kJQP7kiw5Fk",
				Title:     "Luis Fonsi - Despacito ft. Daddy Yankee",
				Channel:   "Luis Fonsi",
				Duration:  "04:41",
				Views:     "8.3B vistas",
				Published: "hace 7 años",
			},
			{
				ID:        "fJ9rUzIMcZQ",
				Title:     "Queen - Bohemian Rhapsody (Official Video Remastered)",
				Channel:   "Queen Official",
				Duration:  "05:59",
				Views:     "1.7B vistas",
				Published: "hace 15 años",
			},
			{
				ID:        "JGwWNGJdvx8",
				Title:     "Ed Sheeran - Shape of You (Official Music Video)",
				Channel:   "Ed Sheeran",
				Duration:  "04:23",
				Views:     "6.2B vistas",
				Published: "hace 7 años",
			},
		},
		model.TabMusic: {
			{
				ID:        "dio_01",
				Title:     "Diomedes Díaz - Mi Primera Cana (Clásico Vallenato)",
				Channel:   "Vallenato Noble",
				Duration:  "04:52",
				Views:     "12M vistas",
				Published: "hace 5 años",
			},
			{
				ID:        "soda_01",
				Title:     "Soda Stereo - De Música Ligera (El Último Concierto)",
				Channel:   "Soda Stereo",
				Duration:  "04:10",
				Views:     "95M vistas",
				Published: "hace 9 años",
			},
			{
				ID:        "cure_01",
				Title:     "The Cure - Pictures of You (Official Remaster)",
				Channel:   "The Cure",
				Duration:  "07:28",
				Views:     "35M vistas",
				Published: "hace 11 años",
			},
		},
		model.TabSubscriptions: {
			{
				ID:        "sub_01",
				Title:     "Neovim 0.11 Deep Dive: Everything You Missed",
				Channel:   "ThePrimeagen",
				Duration:  "22:15",
				Views:     "150K vistas",
				Published: "hace 2 horas",
			},
			{
				ID:        "sub_02",
				Title:     "Writing a Terminal OS in Rust and Go",
				Channel:   "Tsoding Daily",
				Duration:  "45:00",
				Views:     "82K vistas",
				Published: "hace 1 día",
			},
		},
		model.TabDownloads: {
			{
				ID:        "dl_01",
				Title:     "Descarga completa: Canserbero - Maquiavélico [1080p]",
				Channel:   "Canserbero",
				Duration:  "04:45",
				Views:     "Local Cache",
				Published: "Completado",
			},
		},
		model.TabHistory: {
			{
				ID:        "hist_01",
				Title:     "Pink Floyd - Comfortably Numb (Pulse Live 1994)",
				Channel:   "Pink Floyd",
				Duration:  "09:29",
				Views:     "110M vistas",
				Published: "Visto hoy",
			},
		},
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
