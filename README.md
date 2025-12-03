# ls-torrent-tui

A terminal user interface for managing qBittorrent and organizing torrent downloads.

This is a **client and management interface** for qBittorrent—it helps you monitor, control, and organize your existing torrents through a clean terminal UI. It does not provide or index any content.

## What This Project Is *Not*

- Not a torrent indexer
- Not a scraper for any real sites
- Not a tool for finding copyrighted material
- Not a VPN/anonymity tool

It is only a **local qBittorrent client + file organizer**. All providers are user-supplied.

## Features

- **qBittorrent Management** — Monitor and control torrents via the qBittorrent Web API
- **Multi-Tab Interface** — Organized tabs for Search, Downloads, Completed, and Sources
- **User-Supplied Search Providers** — No providers are shipped; users configure their own
- **Media Organization** — Optional Plex-oriented file organization for movie and TV libraries
- **VPN Integration** — Optional VPN status checking and connection management
- **Terminal Theming** — Automatic theme detection for popular terminal emulators

## Safety & Intended Use

This project ships **no real indexers/scrapers** and does **not** endorse copyright infringement.
All provider configs must use **placeholder domains**.
Users must ensure legal compliance and supply their own sources.

## How It Works

The TUI is built with Go using [Bubble Tea](https://github.com/charmbracelet/bubbletea), [Bubbles](https://github.com/charmbracelet/bubbles), and [Lip Gloss](https://github.com/charmbracelet/lipgloss).

- Communicates with qBittorrent through its Web API
- Search providers are pluggable scrapers that users configure themselves
- Optional integrations:
    - **VPN** — Status checking via external scripts or native integration (planned)
    - **Plex** — Organize completed downloads into movie/TV library folder structures

## Requirements

- **Go** 1.21 or later (developed with 1.25)
- **qBittorrent** with Web UI enabled
- **Linux** or **macOS** (Windows should work but is not the primary development target)

## Installation

### Using Go

```bash
go install github.com/litescript/ls-torrent-tui/cmd/torrent-tui@latest
```

### From Source

```bash
git clone https://github.com/litescript/ls-torrent-tui.git
cd ls-torrent-tui
make build
```

The binary will be created at `./build/torrent-tui`.

To install to your PATH:

```bash
make install  # Installs to ~/.local/bin/torrent-tui
```

## Configuration

Configuration is stored at `~/.config/torrent-tui/config.toml`.

### Example Configuration

```toml
[qbittorrent]
host = "localhost"
port = 8080
username = "admin"
password = "your-password"

[downloads]
path = "~/Downloads/torrents"

[vpn]
use_native = false
status_script = "~/scripts/vpn-status.sh"
connect_script = "~/scripts/vpn-connect.sh"

[plex]
movie_library = "/media/Movies"
tv_library = "/media/TV Shows"
auto_detect = true

# User-defined search sources (placeholder examples)
# [[sources]]
# name = "local-json-catalog"
# url = "http://127.0.0.1:8081/search"
# enabled = true

# [[sources]]
# name = "example-scraper"
# url = "https://example.local"
# enabled = false
```

### Configuration Sections

| Section | Description |
|---------|-------------|
| `[qbittorrent]` | Connection settings for qBittorrent Web API |
| `[downloads]` | Default download path for new torrents |
| `[vpn]` | VPN integration settings (scripts or native) |
| `[plex]` | Media library paths for file organization |
| `[[sources]]` | User-defined search providers (repeatable) |

### Adding Search Sources

By default, no search sources are configured. To add a source:

1. Open the application and navigate to the **Sources** tab
2. Press `a` to add a new source
3. Enter the URL of a source that returns magnet links for content you're allowed to access
4. The generic scraper will attempt to parse search results

Sources are saved to your config file and persist between sessions.

## Usage

Start the application:

```bash
torrent-tui
```

### Tabs

| Tab | Purpose |
|-----|---------|
| **Search** | Query your configured search providers |
| **Downloads** | View and manage active downloads |
| **Completed** | View finished torrents, organize into libraries |
| **Sources** | Manage search providers |

### Keybindings

| Key | Action |
|-----|--------|
| `Tab` / `1-4` | Switch tabs |
| `j` / `k` / `↑` / `↓` | Navigate lists |
| `Enter` | Select / Confirm |
| `d` | Download selected torrent |
| `p` | Pause / Resume torrent |
| `x` | Delete torrent (keep files) |
| `X` | Delete torrent and files |
| `m` | Move to movie library |
| `t` | Move to TV library |
| `v` | Check VPN status |
| `V` | Connect to VPN |
| `a` | Add new search source |
| `q` / `Ctrl+C` | Quit |

## Architecture

```
cmd/torrent-tui/       # Application entry point
internal/
    config/            # TOML configuration handling
    plex/              # Media library organization
    qbit/              # qBittorrent Web API client
    scraper/           # Search provider interface (pluggable)
    theme/             # Terminal theming and detection
    tui/               # Bubble Tea UI components
    version/           # Version information
    vpn/               # VPN status and connection
```

The scraper system is modular—the `Scraper` interface can be implemented by users for custom providers. See `internal/scraper/example.go` for implementation guidance.

## Development

### Commands

```bash
# Format code
go fmt ./...
make fmt

# Static analysis
go vet ./...
make vet

# Run tests
go test ./...
make test

# Lint (vet + format check)
make lint

# Build
go build ./...
make build
```

### CI

The project uses GitHub Actions for continuous integration. On every push and pull request, CI runs:

- Format check (`gofmt`)
- Static analysis (`go vet`)
- Tests (`go test`)
- Build verification (`go build`)

## Roadmap

**v0.1.0** (current): Basic TUI + qBittorrent control

**Next**:
- Search provider UX improvements
- Native VPN integration
- Plex organize pipeline

## License

Licensed under the MIT License — see [LICENSE](LICENSE) for details.
