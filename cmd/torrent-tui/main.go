// Torrent TUI is a terminal-based torrent search and management application.
// It provides a clean interface for searching torrents, managing downloads
// via qBittorrent, and organizing media with Plex integration.
package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/litescript/ls-torrent-tui/internal/config"
	"github.com/litescript/ls-torrent-tui/internal/theme"
	"github.com/litescript/ls-torrent-tui/internal/tui"
	"github.com/litescript/ls-torrent-tui/internal/version"
)

func main() {
	// Handle --version / -v flag
	if len(os.Args) > 1 {
		arg := os.Args[1]
		if arg == "--version" || arg == "-v" {
			fmt.Printf("torrent-tui v%s\n", version.Version)
			os.Exit(0)
		}
	}

	// Load config
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load config: %v\n", err)
	}

	// Ensure download directory exists
	if err := config.EnsureDownloadDir(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to create download dir: %v\n", err)
	}

	// Start theme watcher
	themeWatcher, err := theme.NewWatcher(nil)
	if err == nil {
		defer themeWatcher.Stop()
	}

	// Create and run TUI
	model := tui.NewModel(cfg)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
