package plex

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ErrInvalidInput indicates the input cannot be processed for naming.
var ErrInvalidInput = errors.New("invalid input for naming")

// MovieNaming contains parsed movie information for file naming.
type MovieNaming struct {
	Title      string
	Year       int
	Resolution string // e.g., "1080p", "4K"
	Extension  string // e.g., ".mkv", ".mp4"
}

// TVNaming contains parsed TV show information for file naming.
type TVNaming struct {
	ShowTitle    string
	Season       int
	Episode      int
	EpisodeTitle string // Optional episode title
	Resolution   string
	Extension    string
}

// FormatMoviePath generates a Plex-compatible filename for a movie.
// Returns: "Title (Year).ext" (directly in Movies folder, like the bash script)
func FormatMoviePath(m MovieNaming) (string, error) {
	if m.Title == "" {
		return "", ErrInvalidInput
	}

	title := SanitizeFilename(m.Title)

	if m.Year > 0 {
		return fmt.Sprintf("%s (%d)%s", title, m.Year, m.Extension), nil
	}
	return title + m.Extension, nil
}

// FormatTVPath generates a Plex-compatible directory path for a TV episode.
// Returns: "Show Title/Season ##" - caller appends original filename.
func FormatTVPath(t TVNaming) (string, error) {
	if t.ShowTitle == "" {
		return "", ErrInvalidInput
	}

	showDir := SanitizeFilename(t.ShowTitle)
	seasonDir := fmt.Sprintf("Season %02d", t.Season)

	// Return just the directory path - original filename is kept for TV
	return filepath.Join(showDir, seasonDir), nil
}

// FormatTVFilename generates a Plex-compatible filename for a TV episode.
// Returns: "Show Title - S##E## - Episode Title.ext" or "Show Title - S##E##.ext"
func FormatTVFilename(t TVNaming) string {
	title := SanitizeFilename(t.ShowTitle)

	if t.EpisodeTitle != "" {
		epTitle := SanitizeFilename(t.EpisodeTitle)
		return fmt.Sprintf("%s - S%02dE%02d - %s%s", title, t.Season, t.Episode, epTitle, t.Extension)
	}
	return fmt.Sprintf("%s - S%02dE%02d%s", title, t.Season, t.Episode, t.Extension)
}

// SanitizeFilename removes or replaces characters that are invalid
// for filesystem paths.
func SanitizeFilename(name string) string {
	// Characters that are invalid on most filesystems
	replacer := strings.NewReplacer(
		"/", "-",
		"\\", "-",
		":", "-",
		"*", "",
		"?", "",
		"\"", "'",
		"<", "",
		">", "",
		"|", "-",
	)
	result := replacer.Replace(name)

	// Remove leading/trailing spaces and dots
	result = strings.Trim(result, " .")

	return result
}
