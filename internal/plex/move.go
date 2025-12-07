package plex

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Move operation errors.
var (
	ErrSourceNotFound    = errors.New("source file or directory not found")
	ErrDestinationExists = errors.New("destination already exists")
	ErrPermissionDenied  = errors.New("permission denied")
	ErrPathEscape        = errors.New("path escapes allowed directory")
)

// MoveConfig holds configuration for media file operations.
type MoveConfig struct {
	MovieLibraryPath string // Base path for movie library
	TVLibraryPath    string // Base path for TV library
	UseSudo          bool   // Use sudo for rsync operations
	DryRun           bool   // If true, simulate without moving
}

// MoveResult contains the outcome of a move operation.
type MoveResult struct {
	SourcePath      string
	DestinationPath string
	MediaType       MediaType
	BytesMoved      int64
	Success         bool
	Error           error
}

// MoveProgress reports progress during a move operation.
type MoveProgress struct {
	BytesCopied int64
	TotalBytes  int64
	Percentage  float64
	CurrentFile string
	Rate        string // Transfer rate (e.g., "10.5MB/s")
	ETA         string // Estimated time remaining (e.g., "0:01:23")
}

// Mover handles moving completed downloads to Plex libraries.
type Mover struct {
	config MoveConfig
}

// NewMover creates a new Mover with the given configuration.
func NewMover(config MoveConfig) *Mover {
	return &Mover{config: config}
}

// Video file extensions to look for
var videoExtensions = map[string]bool{
	".mkv": true, ".mp4": true, ".m4v": true,
	".avi": true, ".mov": true, ".wmv": true,
}

// MoveToLibraryWithProgress moves a completed download to the appropriate Plex library.
// It reports progress via the provided channel.
func (m *Mover) MoveToLibraryWithProgress(
	ctx context.Context,
	sourcePath string,
	detection DetectionResult,
	cleanup bool,
	progress chan<- MoveProgress,
) (*MoveResult, error) {
	// Find the main video file
	mainVideo, err := FindMainVideo(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("find video: %w", err)
	}

	// Get video file info for size
	info, err := os.Stat(mainVideo)
	if err != nil {
		return nil, fmt.Errorf("stat video: %w", err)
	}
	totalBytes := info.Size()

	// Generate destination path based on media type
	var destDir, destFile string
	ext := filepath.Ext(mainVideo)

	switch detection.Type {
	case MediaTypeMovie:
		movieFilename, err := FormatMoviePath(MovieNaming{
			Title:     detection.Title,
			Year:      detection.Year,
			Extension: ext,
		})
		if err != nil {
			return nil, fmt.Errorf("format movie path: %w", err)
		}
		// Movies go directly in the Movies folder (no subdirectory)
		destDir = m.config.MovieLibraryPath
		destFile = filepath.Join(m.config.MovieLibraryPath, movieFilename)

	case MediaTypeTV:
		tvDir, err := FormatTVPath(TVNaming{
			ShowTitle: detection.Title,
			Season:    detection.Season,
		})
		if err != nil {
			return nil, fmt.Errorf("format tv path: %w", err)
		}
		destDir = filepath.Join(m.config.TVLibraryPath, tvDir)
		// Keep original filename for TV
		destFile = filepath.Join(destDir, filepath.Base(mainVideo))

	default:
		return nil, fmt.Errorf("unknown media type")
	}

	// Create destination directory (skip if using sudo - rsync --mkpath handles it)
	if !m.config.UseSudo {
		if err := m.mkdirAll(destDir); err != nil {
			return nil, fmt.Errorf("create directory: %w", err)
		}
	}

	// Copy main video with rsync and progress
	if err := m.rsyncWithProgress(ctx, mainVideo, destFile, totalBytes, progress); err != nil {
		return nil, fmt.Errorf("copy video: %w", err)
	}

	// Find and copy subtitles
	subtitles := FindSubtitles(sourcePath)
	for _, sub := range subtitles {
		subDest := filepath.Join(destDir, filepath.Base(sub))
		_ = m.rsyncFile(sub, subDest) // Non-fatal if subtitle copy fails
	}

	// Cleanup source if requested
	if cleanup {
		m.cleanupSource(sourcePath, mainVideo, subtitles)
	}

	return &MoveResult{
		SourcePath:      mainVideo,
		DestinationPath: destFile,
		MediaType:       detection.Type,
		BytesMoved:      totalBytes,
		Success:         true,
	}, nil
}

// MoveToLibrary moves a completed download without progress reporting.
func (m *Mover) MoveToLibrary(ctx context.Context, sourcePath string) (*MoveResult, error) {
	detection, err := DetectFromPath(sourcePath)
	if err != nil {
		return nil, err
	}
	return m.MoveToLibraryWithProgress(ctx, sourcePath, detection, false, nil)
}

// MoveAsMovie moves a file to the movie library with specified naming.
func (m *Mover) MoveAsMovie(ctx context.Context, sourcePath string, naming MovieNaming) (*MoveResult, error) {
	detection := DetectionResult{
		Type:  MediaTypeMovie,
		Title: naming.Title,
		Year:  naming.Year,
	}
	return m.MoveToLibraryWithProgress(ctx, sourcePath, detection, false, nil)
}

// MoveAsTV moves a file to the TV library with specified naming.
func (m *Mover) MoveAsTV(ctx context.Context, sourcePath string, naming TVNaming) (*MoveResult, error) {
	detection := DetectionResult{
		Type:    MediaTypeTV,
		Title:   naming.ShowTitle,
		Season:  naming.Season,
		Episode: naming.Episode,
	}
	return m.MoveToLibraryWithProgress(ctx, sourcePath, detection, false, nil)
}

// FindMainVideo finds the largest video file in a directory (top level only), ignoring samples.
func FindMainVideo(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("source not found: %s", path)
	}

	// If it's a file, return it directly
	if !info.IsDir() {
		ext := strings.ToLower(filepath.Ext(path))
		if videoExtensions[ext] {
			return path, nil
		}
		return "", fmt.Errorf("not a video file: %s", path)
	}

	// Read directory entries (top level only, like script's -maxdepth 1)
	entries, err := os.ReadDir(path)
	if err != nil {
		return "", fmt.Errorf("cannot read directory: %w", err)
	}

	var largest string
	var largestSize int64
	var filesFound []string

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))
		filesFound = append(filesFound, name)

		// Skip sample files
		if strings.Contains(strings.ToLower(name), "sample") {
			continue
		}

		if videoExtensions[ext] {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			if info.Size() > largestSize {
				largestSize = info.Size()
				largest = filepath.Join(path, name)
			}
		}
	}

	if largest == "" {
		return "", fmt.Errorf("no video files (.mkv/.mp4/.avi) in %s - found: %v", path, filesFound)
	}
	return largest, nil
}

// FindSubtitles finds all .srt subtitle files in a directory (up to 2 levels deep).
func FindSubtitles(path string) []string {
	var subs []string

	info, err := os.Stat(path)
	if err != nil {
		return subs
	}

	baseDir := path
	if !info.IsDir() {
		baseDir = filepath.Dir(path)
	}

	filepath.Walk(baseDir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		// Check depth (max 2 levels)
		rel, _ := filepath.Rel(baseDir, p)
		depth := strings.Count(rel, string(filepath.Separator))
		if depth > 1 {
			return nil
		}

		if strings.HasSuffix(strings.ToLower(p), ".srt") {
			subs = append(subs, p)
		}
		return nil
	})

	return subs
}

// mkdirAll creates a directory, trying without sudo first.
func (m *Mover) mkdirAll(path string) error {
	// Try regular mkdir first
	if err := os.MkdirAll(path, 0755); err == nil {
		return nil
	}
	// Fall back to sudo mkdir if regular fails and sudo is enabled
	if m.config.UseSudo {
		return exec.Command("sudo", "-n", "mkdir", "-p", path).Run()
	}
	return os.MkdirAll(path, 0755)
}

// rsyncFile copies a single file using rsync.
func (m *Mover) rsyncFile(src, dst string) error {
	args := []string{"-avh", "--inplace", "--mkpath", src, dst}
	var cmd *exec.Cmd
	if m.config.UseSudo {
		sudoArgs := append([]string{"-n", "rsync"}, args...)
		cmd = exec.Command("sudo", sudoArgs...)
	} else {
		cmd = exec.Command("rsync", args...)
	}
	return cmd.Run()
}

// rsyncWithProgress runs rsync and parses progress output.
func (m *Mover) rsyncWithProgress(
	ctx context.Context,
	src, dst string,
	totalBytes int64,
	progress chan<- MoveProgress,
) error {
	args := []string{"-avh", "--info=progress2", "--no-inc-recursive", "--partial", "--inplace", "--mkpath", src, dst}

	var cmd *exec.Cmd
	if m.config.UseSudo {
		sudoArgs := append([]string{"-n", "rsync"}, args...)
		cmd = exec.CommandContext(ctx, "sudo", sudoArgs...)
	} else {
		cmd = exec.CommandContext(ctx, "rsync", args...)
	}

	// Get stdout pipe for progress parsing
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	// Capture stderr for error messages
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return err
	}

	// Parse progress from rsync output
	// Format: "  5.70G  86%   10.12MB/s    0:00:45"
	progressRegex := regexp.MustCompile(`(\d+)%`)
	// Match human-readable sizes like "5.70G", "123.45M", "1.2K", "500"
	bytesRegex := regexp.MustCompile(`^\s*([\d.]+)([KMGT]?)`)
	// Match transfer rate like "10.12MB/s" or "1.5GB/s"
	rateRegex := regexp.MustCompile(`([\d.]+[KMGT]?B/s)`)
	// Match ETA like "0:01:23" or "0:00:45"
	etaRegex := regexp.MustCompile(`(\d+:\d+:\d+)`)

	scanner := bufio.NewScanner(stdout)
	scanner.Split(scanRsyncLines)

	for scanner.Scan() {
		line := scanner.Text()

		// Try to extract percentage
		if matches := progressRegex.FindStringSubmatch(line); matches != nil {
			pct, _ := strconv.Atoi(matches[1])

			// Try to extract bytes copied (human-readable format)
			var copied int64
			if byteMatches := bytesRegex.FindStringSubmatch(line); byteMatches != nil {
				value, _ := strconv.ParseFloat(byteMatches[1], 64)
				suffix := byteMatches[2]
				switch suffix {
				case "K":
					copied = int64(value * 1024)
				case "M":
					copied = int64(value * 1024 * 1024)
				case "G":
					copied = int64(value * 1024 * 1024 * 1024)
				case "T":
					copied = int64(value * 1024 * 1024 * 1024 * 1024)
				default:
					copied = int64(value)
				}
			}

			// Extract rate
			var rate string
			if rateMatches := rateRegex.FindStringSubmatch(line); rateMatches != nil {
				rate = rateMatches[1]
			}

			// Extract ETA
			var eta string
			if etaMatches := etaRegex.FindStringSubmatch(line); etaMatches != nil {
				eta = etaMatches[1]
			}

			if progress != nil {
				// Non-blocking send to prevent rsync from hanging
				select {
				case progress <- MoveProgress{
					BytesCopied: copied,
					TotalBytes:  totalBytes,
					Percentage:  float64(pct) / 100.0,
					CurrentFile: filepath.Base(src),
					Rate:        rate,
					ETA:         eta,
				}:
				default:
					// Channel full, skip this update
				}
			}
		}
	}

	if err := cmd.Wait(); err != nil {
		if stderrBuf.Len() > 0 {
			return fmt.Errorf("%w: %s", err, stderrBuf.String())
		}
		return err
	}
	return nil
}

// scanRsyncLines is a custom scanner that handles rsync's carriage return progress updates.
func scanRsyncLines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}

	// Look for \r or \n
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' || data[i] == '\r' {
			return i + 1, data[0:i], nil
		}
	}

	if atEOF {
		return len(data), data, nil
	}

	// Request more data
	return 0, nil, nil
}

// cleanupSource removes source files after successful move.
// Note: No sudo needed - these are the user's own downloaded files.
func (m *Mover) cleanupSource(basePath, mainVideo string, subtitles []string) {
	// Remove main video
	os.Remove(mainVideo)

	// Remove subtitles
	for _, sub := range subtitles {
		os.Remove(sub)
	}

	// Try to remove empty directory
	info, err := os.Stat(basePath)
	if err == nil && info.IsDir() {
		// Check if empty
		entries, _ := os.ReadDir(basePath)
		if len(entries) == 0 {
			os.Remove(basePath)
		}
	}
}

// ValidatePath checks that a path is safe and within allowed boundaries.
func ValidatePath(path, allowedBase string) error {
	// Resolve both paths to absolute
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	absBase, err := filepath.Abs(allowedBase)
	if err != nil {
		return err
	}

	// Check that path is within base
	if !strings.HasPrefix(absPath, absBase) {
		return ErrPathEscape
	}

	return nil
}
