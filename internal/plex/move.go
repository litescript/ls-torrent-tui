package plex

import (
	"context"
	"errors"
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

// Mover handles moving completed downloads to Plex libraries.
type Mover struct {
	config MoveConfig
}

// NewMover creates a new Mover with the given configuration.
func NewMover(config MoveConfig) *Mover {
	return &Mover{config: config}
}

// MoveToLibrary moves a completed download to the appropriate Plex library.
// It auto-detects the media type and applies proper naming.
// TODO: Implement with:
//   - Path validation (no directory escape)
//   - Atomic move operations where possible
//   - Progress reporting for large files
//   - Rollback on failure
func (m *Mover) MoveToLibrary(ctx context.Context, sourcePath string) (*MoveResult, error) {
	// TODO: Implement move operation
	//
	// Steps:
	// 1. Validate source exists and is accessible
	// 2. Detect media type
	// 3. Generate destination path with proper naming
	// 4. Validate destination doesn't escape library path
	// 5. Check destination doesn't already exist
	// 6. Perform move (or copy+delete across filesystems)
	// 7. Verify move completed successfully
	//
	// Security considerations:
	// - Validate paths don't contain .. or symlink escapes
	// - Ensure destination is within configured library paths
	// - Check file permissions before and after
	return nil, ErrSourceNotFound
}

// MoveAsMovie moves a file to the movie library with specified naming.
// TODO: Implement explicit movie move.
func (m *Mover) MoveAsMovie(ctx context.Context, sourcePath string, naming MovieNaming) (*MoveResult, error) {
	// TODO: Implement explicit movie move
	return nil, ErrSourceNotFound
}

// MoveAsTV moves a file to the TV library with specified naming.
// TODO: Implement explicit TV move.
func (m *Mover) MoveAsTV(ctx context.Context, sourcePath string, naming TVNaming) (*MoveResult, error) {
	// TODO: Implement explicit TV move
	return nil, ErrSourceNotFound
}

// ValidatePath checks that a path is safe and within allowed boundaries.
// TODO: Implement path validation.
func ValidatePath(path, allowedBase string) error {
	// TODO: Implement path validation
	//
	// Must check:
	// - Path doesn't contain ..
	// - Resolved path is within allowedBase
	// - No symlink escape attempts
	// - Path characters are valid for filesystem
	return nil
}
