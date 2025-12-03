// Package config handles application configuration via TOML files.
// Configuration is stored at ~/.config/torrent-tui/config.toml and includes
// settings for qBittorrent, VPN, downloads, and custom torrent sources.
package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config holds application configuration
type Config struct {
	QBittorrent QBittorrentConfig `toml:"qbittorrent"`
	VPN         VPNConfig         `toml:"vpn"`
	Downloads   DownloadsConfig   `toml:"downloads"`
	Plex        PlexConfig        `toml:"plex"`
	Sources     []SourceConfig    `toml:"sources"`
}

// SourceConfig holds a custom torrent source
type SourceConfig struct {
	Name    string `toml:"name"`
	URL     string `toml:"url"`
	Enabled bool   `toml:"enabled"`
}

// QBittorrentConfig holds qBittorrent Web API settings
type QBittorrentConfig struct {
	Host     string `toml:"host"`
	Port     int    `toml:"port"`
	Username string `toml:"username"`
	Password string `toml:"password"`
}

// VPNConfig holds VPN configuration
type VPNConfig struct {
	// UseNative enables native VPN integration (future feature).
	// When false (default), uses external scripts.
	UseNative     bool   `toml:"use_native"`
	StatusScript  string `toml:"status_script"`
	ConnectScript string `toml:"connect_script"`
}

// DownloadsConfig holds download settings
type DownloadsConfig struct {
	Path string `toml:"path"`
}

// PlexConfig holds Plex library integration settings
type PlexConfig struct {
	// MovieLibrary is the path to the Plex movie library.
	// Example: /media/plex/Movies
	MovieLibrary string `toml:"movie_library"`

	// TVLibrary is the path to the Plex TV library.
	// Example: /media/plex/TV Shows
	TVLibrary string `toml:"tv_library"`

	// AutoDetect enables automatic media type detection.
	// When true, attempts to detect movie vs TV from filename patterns.
	// When false, user must explicitly choose during move operation.
	AutoDetect bool `toml:"auto_detect"`

	// TODO: Future settings to consider:
	// - Naming templates
	// - API integration (Plex server URL, token)
	// - Library scan triggering
	// - Subtitle handling
}

// Default returns the default configuration
func Default() Config {
	home, _ := os.UserHomeDir()

	return Config{
		QBittorrent: QBittorrentConfig{
			Host:     "localhost",
			Port:     8080,
			Username: "admin",
			Password: "adminadmin",
		},
		VPN: VPNConfig{
			UseNative:     false, // Use scripts by default until native is implemented
			StatusScript:  filepath.Join(home, "vpn", "nord", "nordvpn-status.sh"),
			ConnectScript: filepath.Join(home, "vpn", "nord", "nordvpn-connect.sh"),
		},
		Downloads: DownloadsConfig{
			Path: filepath.Join(home, "Downloads", "torrents"),
		},
		Plex: PlexConfig{
			MovieLibrary: "", // Must be configured by user
			TVLibrary:    "", // Must be configured by user
			AutoDetect:   true,
		},
		// Sources: nil - no search sources by default
		// Users add their own sources via the Sources tab in the TUI
	}
}

// ConfigPath returns the path to the config file
func ConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "torrent-tui", "config.toml")
}

// Load reads config from disk or returns defaults
func Load() (Config, error) {
	cfg := Default()
	path := ConfigPath()

	data, err := os.ReadFile(path)
	if err != nil {
		// No config file, return defaults
		return cfg, nil
	}

	if err := toml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}

	return cfg, nil
}

// Save writes config to disk
func Save(cfg Config) error {
	path := ConfigPath()

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	return toml.NewEncoder(f).Encode(cfg)
}

// EnsureDownloadDir creates the download directory if it doesn't exist
func EnsureDownloadDir(cfg Config) error {
	return os.MkdirAll(cfg.Downloads.Path, 0755)
}
