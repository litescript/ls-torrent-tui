// Package scraper provides a modular interface for torrent search providers.
// It defines the Scraper interface and common types. Users can implement
// custom scrapers for their preferred sources or use the GenericScraper
// which works with any site that provides magnet links.
package scraper

import "context"

// Torrent represents a search result
type Torrent struct {
	Name     string
	Size     string
	Seeders  int
	Leechers int
	Magnet   string
	InfoURL  string
	Source   string
	Files    []FileInfo
}

// FileInfo represents a file within a torrent
type FileInfo struct {
	Name string
	Size string
}

// Health returns a health score 0-100 based on seeders/leechers ratio
func (t Torrent) Health() int {
	if t.Seeders == 0 {
		return 0
	}
	if t.Leechers == 0 {
		return 100
	}

	ratio := float64(t.Seeders) / float64(t.Seeders+t.Leechers) * 100
	if ratio > 100 {
		ratio = 100
	}
	return int(ratio)
}

// Scraper interface for torrent search providers
type Scraper interface {
	// Name returns the source name
	Name() string

	// Search queries for torrents
	Search(ctx context.Context, query string) ([]Torrent, error)

	// GetFiles fetches file list for a torrent (if supported)
	GetFiles(ctx context.Context, t *Torrent) error
}

// MultiScraper aggregates results from multiple sources
type MultiScraper struct {
	scrapers []Scraper
}

// NewMultiScraper creates a scraper that queries multiple sources
func NewMultiScraper(scrapers ...Scraper) *MultiScraper {
	return &MultiScraper{scrapers: scrapers}
}

// Search queries all scrapers and merges results
func (m *MultiScraper) Search(ctx context.Context, query string) ([]Torrent, error) {
	var results []Torrent

	for _, s := range m.scrapers {
		torrents, err := s.Search(ctx, query)
		if err != nil {
			continue // Skip failed sources
		}
		results = append(results, torrents...)
	}

	return results, nil
}
