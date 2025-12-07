// Package plex provides media organization for Plex libraries.
// It handles media type detection, file renaming, and library placement.
package plex

import (
	"errors"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

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
	Title      string  // Parsed title
	Year       int     // Release year (movies) or 0 if unknown
	Season     int     // Season number (TV) or 0
	Episode    int     // Episode number (TV) or 0
	Confidence float64 // 0.0-1.0 confidence score
}

// TV show patterns - check these first (more specific)
var (
	// S01E02, s01e02, S1E2
	tvPatternSE = regexp.MustCompile(`(?i)[sS](\d{1,2})[eE](\d{1,2})`)
	// 1x02, 01x02
	tvPatternX = regexp.MustCompile(`(?i)(\d{1,2})x(\d{2})`)
)

// Movie year pattern - 4 digit year between 1900-2099
var yearPattern = regexp.MustCompile(`(19\d{2}|20\d{2})`)

// Detect analyzes a filename to determine media type.
// Checks TV patterns first (more specific), then falls back to movie year detection.
func Detect(filename string) (DetectionResult, error) {
	// Get just the filename without path
	name := filepath.Base(filename)
	// Remove extension for cleaner parsing
	ext := filepath.Ext(name)
	nameNoExt := strings.TrimSuffix(name, ext)

	// Try TV detection first (more specific patterns)
	if result, ok := detectTV(nameNoExt); ok {
		return result, nil
	}

	// Try movie detection (year-based)
	if result, ok := detectMovie(nameNoExt); ok {
		return result, nil
	}

	// Fallback: return unknown with cleaned title
	return DetectionResult{
		Type:       MediaTypeUnknown,
		Title:      cleanTitle(nameNoExt),
		Confidence: 0.1,
	}, ErrDetectionFailed
}

// DetectFromPath analyzes a full path including parent directories.
// Uses the innermost directory or filename for detection.
func DetectFromPath(path string) (DetectionResult, error) {
	// Try detecting from the path itself first
	result, err := Detect(path)
	if err == nil && result.Type != MediaTypeUnknown {
		return result, nil
	}

	// If path is a directory, try using the directory name
	name := filepath.Base(path)
	return Detect(name)
}

// detectTV attempts to identify TV show patterns in the filename.
func detectTV(name string) (DetectionResult, bool) {
	// Try S##E## pattern first
	if matches := tvPatternSE.FindStringSubmatch(name); matches != nil {
		season, _ := strconv.Atoi(matches[1])
		episode, _ := strconv.Atoi(matches[2])

		// Extract show title (everything before the pattern)
		idx := tvPatternSE.FindStringIndex(name)
		titleRaw := name[:idx[0]]
		title := cleanTitle(titleRaw)

		return DetectionResult{
			Type:       MediaTypeTV,
			Title:      title,
			Season:     season,
			Episode:    episode,
			Confidence: 0.9,
		}, true
	}

	// Try ##x## pattern
	if matches := tvPatternX.FindStringSubmatch(name); matches != nil {
		season, _ := strconv.Atoi(matches[1])
		episode, _ := strconv.Atoi(matches[2])

		// Extract show title
		idx := tvPatternX.FindStringIndex(name)
		titleRaw := name[:idx[0]]
		title := cleanTitle(titleRaw)

		return DetectionResult{
			Type:       MediaTypeTV,
			Title:      title,
			Season:     season,
			Episode:    episode,
			Confidence: 0.85,
		}, true
	}

	return DetectionResult{}, false
}

// detectMovie attempts to identify movie patterns (year-based).
func detectMovie(name string) (DetectionResult, bool) {
	// Find year in filename
	matches := yearPattern.FindStringSubmatch(name)
	if matches == nil {
		return DetectionResult{}, false
	}

	year, _ := strconv.Atoi(matches[1])

	// Extract title (everything before the year)
	idx := yearPattern.FindStringIndex(name)
	titleRaw := name[:idx[0]]
	title := cleanTitle(titleRaw)

	// Validate: title should not be empty
	if title == "" {
		return DetectionResult{}, false
	}

	return DetectionResult{
		Type:       MediaTypeMovie,
		Title:      title,
		Year:       year,
		Confidence: 0.8,
	}, true
}

// cleanTitle converts a raw filename segment into a clean title.
// Replaces dots, underscores with spaces and trims whitespace.
func cleanTitle(raw string) string {
	// Replace dots and underscores with spaces
	cleaned := strings.ReplaceAll(raw, ".", " ")
	cleaned = strings.ReplaceAll(cleaned, "_", " ")

	// Remove common release tags that might be left at the end
	// (These often appear between title and year/episode)
	// Also trim opening parenthesis/bracket that precedes year
	cleaned = strings.TrimRight(cleaned, " -([{")

	// Collapse multiple spaces
	spacePattern := regexp.MustCompile(`\s+`)
	cleaned = spacePattern.ReplaceAllString(cleaned, " ")

	return strings.TrimSpace(cleaned)
}
