package scraper

import (
	"context"
)

// DummyScraper is a no-op scraper that returns empty results.
// It serves as the default scraper when no sources are configured,
// ensuring the TUI remains functional without any external dependencies.
type DummyScraper struct {
	name string
}

// NewDummyScraper creates a dummy scraper that returns no results.
func NewDummyScraper() *DummyScraper {
	return &DummyScraper{
		name: "None",
	}
}

// Name returns the scraper name.
func (s *DummyScraper) Name() string {
	return s.name
}

// Search returns empty results.
func (s *DummyScraper) Search(ctx context.Context, query string) ([]Torrent, error) {
	return nil, nil
}

// GetFiles is a no-op.
func (s *DummyScraper) GetFiles(ctx context.Context, t *Torrent) error {
	return nil
}
