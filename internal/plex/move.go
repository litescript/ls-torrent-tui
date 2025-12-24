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
	"sort"
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
}

// MoveResult contains the outcome of a move operation.
type MoveResult struct {
	SourcePath      string   // First/main source file
	DestinationPath string   // Destination directory (TV) or file (movie)
	MediaType       MediaType
	BytesMoved      int64
	FilesMoved      int      // Number of video files moved (1 for movies, N for TV)
	Success         bool
	Error           error
	RemainingFiles  []string // Files left in source directory (for cleanup prompt)
	SourceDir       string   // Source directory path (for cleanup)
}

// MoveProgress reports progress during a move operation.
type MoveProgress struct {
	BytesCopied int64
	TotalBytes  int64
	Percentage  float64   // Overall progress (0.0-1.0)
	CurrentFile string
	Rate        string    // Transfer rate (e.g., "10.5MB/s")
	ETA         string    // Estimated time remaining (e.g., "0:01:23")
	// TV multi-episode fields
	EpisodeIndex    int     // Current episode index (1-based), 0 for movies
	EpisodeTotal    int     // Total episodes, 0 for movies
	EpisodeProgress float64 // Current episode progress (0.0-1.0)
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
// For movies: moves the largest video file to Movies library.
// For TV: moves ALL video files, each to their proper season folder based on S##E## in filename.
// It reports progress via the provided channel.
func (m *Mover) MoveToLibraryWithProgress(
	ctx context.Context,
	sourcePath string,
	detection DetectionResult,
	cleanup bool,
	progress chan<- MoveProgress,
) (*MoveResult, error) {
	// Determine source directory
	sourceDir := sourcePath
	srcInfo, err := os.Stat(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("stat source: %w", err)
	}
	sourceIsDir := srcInfo.IsDir()
	if !sourceIsDir {
		sourceDir = filepath.Dir(sourcePath)
	}

	// Branch based on media type
	switch detection.Type {
	case MediaTypeMovie:
		return m.moveMovie(ctx, sourcePath, sourceDir, sourceIsDir, detection, cleanup, progress)
	case MediaTypeTV:
		return m.moveTV(ctx, sourcePath, sourceDir, sourceIsDir, detection, cleanup, progress)
	default:
		return nil, fmt.Errorf("unknown media type")
	}
}

// moveMovie handles moving a single movie file to the library.
func (m *Mover) moveMovie(
	ctx context.Context,
	sourcePath, sourceDir string,
	sourceIsDir bool,
	detection DetectionResult,
	cleanup bool,
	progress chan<- MoveProgress,
) (*MoveResult, error) {
	// Find the largest video file (movies are single files)
	mainVideo, err := FindMainVideo(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("find video: %w", err)
	}

	info, err := os.Stat(mainVideo)
	if err != nil {
		return nil, fmt.Errorf("stat video: %w", err)
	}
	totalBytes := info.Size()

	// Generate destination path
	ext := filepath.Ext(mainVideo)
	movieFilename, err := FormatMoviePath(MovieNaming{
		Title:     detection.Title,
		Year:      detection.Year,
		Extension: ext,
	})
	if err != nil {
		return nil, fmt.Errorf("format movie path: %w", err)
	}
	destDir := m.config.MovieLibraryPath
	destFile := filepath.Join(destDir, movieFilename)

	// Find subtitles
	subtitles := FindSubtitles(sourcePath)

	// Create destination directory
	if !m.config.UseSudo {
		if err := m.mkdirAll(destDir); err != nil {
			return nil, fmt.Errorf("create directory: %w", err)
		}
	}

	// Copy video with progress
	if err := m.rsyncWithProgress(ctx, mainVideo, destFile, totalBytes, progress); err != nil {
		return nil, fmt.Errorf("copy video: %w", err)
	}

	// Copy subtitles
	for _, sub := range subtitles {
		subDest := filepath.Join(destDir, filepath.Base(sub))
		_ = m.rsyncFile(sub, subDest)
	}

	// Find remaining files for cleanup
	var remaining []string
	if cleanup && sourceIsDir {
		remaining = m.findRemainingFiles(sourceDir, mainVideo, subtitles)
	}

	return &MoveResult{
		SourcePath:      mainVideo,
		DestinationPath: destFile,
		MediaType:       detection.Type,
		BytesMoved:      totalBytes,
		FilesMoved:      1,
		Success:         true,
		RemainingFiles:  remaining,
		SourceDir:       sourceDir,
	}, nil
}

// moveTV handles moving TV episodes - finds ALL video files and moves each to proper season folder.
// Like the bash script: processes each episode, extracts season from THAT file's name.
func (m *Mover) moveTV(
	ctx context.Context,
	sourcePath, sourceDir string,
	sourceIsDir bool,
	detection DetectionResult,
	cleanup bool,
	progress chan<- MoveProgress,
) (*MoveResult, error) {
	// Find ALL video files (not just largest)
	videos, err := FindAllVideos(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("find videos: %w", err)
	}

	// Calculate total size for progress
	var totalBytes int64
	for _, v := range videos {
		if info, err := os.Stat(v); err == nil {
			totalBytes += info.Size()
		}
	}

	// Track all moved files and subtitles for cleanup calculation
	var allMovedVideos []string
	var allMovedSubs []string
	var destDir string // Will be set to last destination for result

	// Move each video file
	var bytesCopied int64
	for i, video := range videos {
		// Detect season from THIS video's filename (e.g., S03E16 -> season 3)
		videoDetection, _ := DetectFromPath(video)
		season := videoDetection.Season
		if season == 0 {
			season = detection.Season // Fallback to modal's detection
		}

		// Build destination: TV/<Show>/Season XX/<original filename>
		tvDir, err := FormatTVPath(TVNaming{
			ShowTitle: detection.Title, // Use show title from modal (user can edit)
			Season:    season,
		})
		if err != nil {
			return nil, fmt.Errorf("format tv path: %w", err)
		}
		destDir = filepath.Join(m.config.TVLibraryPath, tvDir)
		destFile := filepath.Join(destDir, filepath.Base(video))

		// Create destination directory
		if !m.config.UseSudo {
			if err := m.mkdirAll(destDir); err != nil {
				return nil, fmt.Errorf("create directory: %w", err)
			}
		}

		// Get this video's size for progress
		videoInfo, _ := os.Stat(video)
		videoSize := videoInfo.Size()

		// Copy video with progress (reports as part of total)
		if err := m.rsyncWithProgressOffset(ctx, video, destFile, videoSize, totalBytes, bytesCopied, i+1, len(videos), progress); err != nil {
			return nil, fmt.Errorf("copy %s: %w", filepath.Base(video), err)
		}
		bytesCopied += videoSize
		allMovedVideos = append(allMovedVideos, video)

		// Find and copy matching subtitles for THIS episode
		subs := FindSubtitlesForVideo(sourceDir, video)
		for _, sub := range subs {
			subDest := filepath.Join(destDir, filepath.Base(sub))
			_ = m.rsyncFile(sub, subDest)
			allMovedSubs = append(allMovedSubs, sub)
		}

		// Update progress between files
		if progress != nil {
			progress <- MoveProgress{
				BytesCopied:     bytesCopied,
				TotalBytes:      totalBytes,
				Percentage:      float64(bytesCopied) / float64(totalBytes),
				CurrentFile:     filepath.Base(video),
				Rate:            "",
				ETA:             "",
				EpisodeIndex:    i + 1,
				EpisodeTotal:    len(videos),
				EpisodeProgress: 1.0, // Just finished this episode
			}
		}
	}

	// Find remaining files for cleanup
	var remaining []string
	if cleanup && sourceIsDir {
		remaining = m.findRemainingFilesMulti(sourceDir, allMovedVideos, allMovedSubs)
	}

	return &MoveResult{
		SourcePath:      videos[0],
		DestinationPath: destDir,
		MediaType:       detection.Type,
		BytesMoved:      totalBytes,
		FilesMoved:      len(videos),
		Success:         true,
		RemainingFiles:  remaining,
		SourceDir:       sourceDir,
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

// FindAllVideos finds all video files in a directory (up to 2 levels deep), ignoring samples.
// Returns files sorted alphabetically. Used for TV season packs with multiple episodes.
func FindAllVideos(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("source not found: %s", path)
	}

	// If it's a single file, return it directly
	if !info.IsDir() {
		ext := strings.ToLower(filepath.Ext(path))
		if videoExtensions[ext] {
			return []string{path}, nil
		}
		return nil, fmt.Errorf("not a video file: %s", path)
	}

	var videos []string

	// Walk up to 2 levels deep (like script's -maxdepth 2)
	err = filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		// Check depth (max 2 levels from base)
		rel, _ := filepath.Rel(path, p)
		depth := strings.Count(rel, string(filepath.Separator))
		if depth > 1 {
			return nil
		}

		name := info.Name()
		ext := strings.ToLower(filepath.Ext(name))

		// Skip sample files
		if strings.Contains(strings.ToLower(name), "sample") {
			return nil
		}

		if videoExtensions[ext] {
			videos = append(videos, p)
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	if len(videos) == 0 {
		return nil, fmt.Errorf("no video files found in %s", path)
	}

	// Sort for consistent ordering (episodes in order)
	sort.Strings(videos)
	return videos, nil
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

// FindSubtitlesForVideo finds .srt subtitle files matching a specific video file.
// Looks for subtitles that start with the same base name (without extension).
// Like script: find "$BASE_DIR" -maxdepth 2 -type f -iname "${NO_EXT}*.srt"
func FindSubtitlesForVideo(baseDir, videoPath string) []string {
	var subs []string

	// Get video filename without extension
	videoBase := filepath.Base(videoPath)
	videoNoExt := strings.TrimSuffix(videoBase, filepath.Ext(videoBase))

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

		name := info.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".srt") {
			return nil
		}

		// Check if subtitle starts with video's base name (case-insensitive)
		if strings.HasPrefix(strings.ToLower(name), strings.ToLower(videoNoExt)) {
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
				// Clamp values to avoid rsync protocol overhead showing >100%
				if copied > totalBytes {
					copied = totalBytes
				}
				if pct > 100 {
					pct = 100
				}

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

// rsyncWithProgressOffset runs rsync with progress tracking that accounts for previously copied bytes.
// Used when copying multiple files to show overall progress.
func (m *Mover) rsyncWithProgressOffset(
	ctx context.Context,
	src, dst string,
	fileBytes, totalBytes, byteOffset int64,
	episodeIndex, episodeTotal int,
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

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return err
	}

	progressRegex := regexp.MustCompile(`(\d+)%`)
	bytesRegex := regexp.MustCompile(`^\s*([\d.]+)([KMGT]?)`)
	rateRegex := regexp.MustCompile(`([\d.]+[KMGT]?B/s)`)
	etaRegex := regexp.MustCompile(`(\d+:\d+:\d+)`)

	scanner := bufio.NewScanner(stdout)
	scanner.Split(scanRsyncLines)

	for scanner.Scan() {
		line := scanner.Text()

		if matches := progressRegex.FindStringSubmatch(line); matches != nil {
			var fileCopied int64
			if byteMatches := bytesRegex.FindStringSubmatch(line); byteMatches != nil {
				value, _ := strconv.ParseFloat(byteMatches[1], 64)
				switch byteMatches[2] {
				case "K":
					fileCopied = int64(value * 1024)
				case "M":
					fileCopied = int64(value * 1024 * 1024)
				case "G":
					fileCopied = int64(value * 1024 * 1024 * 1024)
				case "T":
					fileCopied = int64(value * 1024 * 1024 * 1024 * 1024)
				default:
					fileCopied = int64(value)
				}
			}

			var rate string
			if rateMatches := rateRegex.FindStringSubmatch(line); rateMatches != nil {
				rate = rateMatches[1]
			}

			var eta string
			if etaMatches := etaRegex.FindStringSubmatch(line); etaMatches != nil {
				eta = etaMatches[1]
			}

			if progress != nil {
				// Calculate overall progress including offset
				overallCopied := byteOffset + fileCopied
				if overallCopied > totalBytes {
					overallCopied = totalBytes
				}
				overallPct := float64(overallCopied) / float64(totalBytes)
				if overallPct > 1.0 {
					overallPct = 1.0
				}

				// Calculate episode progress
				episodePct := float64(fileCopied) / float64(fileBytes)
				if episodePct > 1.0 {
					episodePct = 1.0
				}

				select {
				case progress <- MoveProgress{
					BytesCopied:     overallCopied,
					TotalBytes:      totalBytes,
					Percentage:      overallPct,
					CurrentFile:     filepath.Base(src),
					Rate:            rate,
					ETA:             eta,
					EpisodeIndex:    episodeIndex,
					EpisodeTotal:    episodeTotal,
					EpisodeProgress: episodePct,
				}:
				default:
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

// findRemainingFiles returns all files in the source directory except the moved video and subtitles.
// These are the "cruft" files like NFOs, samples, screenshots, etc.
func (m *Mover) findRemainingFiles(sourceDir, mainVideo string, subtitles []string) []string {
	info, err := os.Stat(sourceDir)
	if err != nil || !info.IsDir() {
		return nil
	}

	// Build set of moved files (use base names for comparison)
	moved := make(map[string]bool)
	moved[filepath.Base(mainVideo)] = true
	for _, sub := range subtitles {
		moved[filepath.Base(sub)] = true
	}

	var remaining []string
	entries, _ := os.ReadDir(sourceDir)
	for _, entry := range entries {
		name := entry.Name()
		if !moved[name] {
			remaining = append(remaining, name)
		}
	}
	return remaining
}

// findRemainingFilesMulti returns all files in the source directory except multiple moved videos and subtitles.
// Used for TV season packs where multiple episodes are moved.
func (m *Mover) findRemainingFilesMulti(sourceDir string, videos, subtitles []string) []string {
	info, err := os.Stat(sourceDir)
	if err != nil || !info.IsDir() {
		return nil
	}

	// Build set of moved files (use base names for comparison)
	moved := make(map[string]bool)
	for _, v := range videos {
		moved[filepath.Base(v)] = true
	}
	for _, sub := range subtitles {
		moved[filepath.Base(sub)] = true
	}

	var remaining []string
	entries, _ := os.ReadDir(sourceDir)
	for _, entry := range entries {
		name := entry.Name()
		if !moved[name] {
			remaining = append(remaining, name)
		}
	}
	return remaining
}

// CleanupSourceDir removes all files and the directory itself.
// Call this after user confirms cleanup of remaining files.
func CleanupSourceDir(sourceDir string) error {
	info, err := os.Stat(sourceDir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		// Single file - just remove it
		return os.Remove(sourceDir)
	}
	// Remove entire directory tree
	return os.RemoveAll(sourceDir)
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
