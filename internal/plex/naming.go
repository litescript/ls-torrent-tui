package plex

import "errors"

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

// FormatMoviePath generates a Plex-compatible path for a movie.
// Returns: "Title (Year)/Title (Year).ext"
// TODO: Implement proper formatting with:
//   - Character sanitization for filesystem safety
//   - Configurable naming templates
//   - Quality suffix options
func FormatMoviePath(m MovieNaming) (string, error) {
	// TODO: Implement movie path formatting
	//
	// Plex convention: Movie Name (2024)/Movie Name (2024).mkv
	//
	// Must handle:
	// - Special characters in titles
	// - Missing year information
	// - Multi-file movies (CD1, CD2, Part 1, etc.)
	// - Extras and bonus content
	return "", ErrInvalidInput
}

// FormatTVPath generates a Plex-compatible path for a TV episode.
// Returns: "Show Title/Season ##/Show Title - S##E## - Episode Title.ext"
// TODO: Implement proper formatting.
func FormatTVPath(t TVNaming) (string, error) {
	// TODO: Implement TV path formatting
	//
	// Plex convention: Show Name/Season 01/Show Name - S01E01 - Episode Title.mkv
	//
	// Must handle:
	// - Multi-episode files (S01E01E02 or S01E01-E03)
	// - Specials (Season 00)
	// - Missing episode titles
	// - Anime absolute numbering
	return "", ErrInvalidInput
}

// SanitizeFilename removes or replaces characters that are invalid
// for filesystem paths.
// TODO: Implement cross-platform path sanitization.
func SanitizeFilename(name string) string {
	// TODO: Implement filename sanitization
	//
	// Must remove/replace: / \ : * ? " < > |
	// Preserve Unicode where safe
	// Handle leading/trailing spaces and dots
	return name
}
