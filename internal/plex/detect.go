// Package plex provides media organization for Plex libraries.
// It handles media type detection, file renaming, and library placement.
package plex

import "errors"

// MediaType represents the detected type of media content.
type MediaType int

const (
	MediaTypeUnknown MediaType = iota
	MediaTypeMovie
	MediaTypeTV
)

// String returns a human-readable media type name.
func (m MediaType) String() string {
	switch m {
	case MediaTypeMovie:
		return "Movie"
	case MediaTypeTV:
		return "TV Show"
	default:
		return "Unknown"
	}
}

// ErrDetectionFailed indicates media type could not be determined.
var ErrDetectionFailed = errors.New("unable to detect media type")

// DetectionResult holds the outcome of media type detection.
type DetectionResult struct {
	Type       MediaType
	Title      string // Parsed title
	Year       int    // Release year (movies) or 0 if unknown
	Season     int    // Season number (TV) or 0
	Episode    int    // Episode number (TV) or 0
	Confidence float64 // 0.0-1.0 confidence score
}

// Detect analyzes a filename or path to determine media type.
// TODO: Implement detection using:
//   - Filename pattern matching (S01E02, 1x02, etc. for TV)
//   - Year extraction for movies (Movie Name (2024))
//   - Directory structure hints
//   - Optional: external API lookup (TMDB, TVDB)
func Detect(filename string) (DetectionResult, error) {
	// TODO: Implement media type detection
	//
	// Strategy:
	// 1. Check for TV patterns: S##E##, ##x##, Season #, etc.
	// 2. Check for movie patterns: Name (YYYY), Name.YYYY
	// 3. Parse release group tags, quality indicators
	// 4. Use heuristics on filename structure
	//
	// Consider edge cases:
	// - Anime naming conventions
	// - Multi-episode files
	// - Movie collections/box sets
	// - Documentary series vs films
	return DetectionResult{Type: MediaTypeUnknown}, ErrDetectionFailed
}

// DetectFromPath analyzes a full path including parent directories.
// TODO: Implement path-aware detection.
func DetectFromPath(path string) (DetectionResult, error) {
	// TODO: Use directory structure for hints
	// e.g., /downloads/TV Shows/... suggests TV content
	return DetectionResult{Type: MediaTypeUnknown}, ErrDetectionFailed
}
