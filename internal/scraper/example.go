package scraper

import (
	"context"
)

// ExampleScraper demonstrates how to implement the Scraper interface.
// This is a reference implementation for developers who want to create
// their own scraper for a specific site.
//
// To create a custom scraper:
//  1. Implement the Scraper interface (Name, Search, GetFiles)
//  2. Parse HTML responses to extract torrent information
//  3. Return properly populated Torrent structs
//
// Example usage:
//
//	scraper := NewExampleScraper()
//	results, err := scraper.Search(ctx, "query")
type ExampleScraper struct {
	name string
}

// NewExampleScraper creates an example scraper instance.
// In a real implementation, you would pass configuration here
// such as the base URL, authentication credentials, etc.
func NewExampleScraper() *ExampleScraper {
	return &ExampleScraper{
		name: "Example",
	}
}

// Name returns the scraper's display name.
// This appears in the TUI's source list and search results.
func (s *ExampleScraper) Name() string {
	return s.name
}

// Search queries the source for torrents matching the query.
// This example returns empty results. A real implementation would:
//  1. Construct a search URL with the query
//  2. Make an HTTP request
//  3. Parse the HTML response
//  4. Extract torrent metadata (name, size, seeders, etc.)
//  5. Return a slice of Torrent structs
func (s *ExampleScraper) Search(ctx context.Context, query string) ([]Torrent, error) {
	// Example of what a real implementation might return:
	//
	// return []Torrent{
	//     {
	//         Name:     "Example Torrent",
	//         Size:     "1.5 GB",
	//         Seeders:  100,
	//         Leechers: 10,
	//         Magnet:   "magnet:?xt=urn:btih:...",
	//         Source:   s.name,
	//     },
	// }, nil

	// This example scraper returns no results
	return nil, nil
}

// GetFiles fetches the file list for a torrent.
// This is called when viewing torrent details in the TUI.
// A real implementation would:
//  1. Navigate to the torrent's detail page (using InfoURL)
//  2. Parse the page to find file listings
//  3. Populate the Files slice on the Torrent struct
//  4. Optionally fetch the magnet link if not already present
func (s *ExampleScraper) GetFiles(ctx context.Context, t *Torrent) error {
	// Example of populating files:
	//
	// t.Files = []FileInfo{
	//     {Name: "file1.mkv", Size: "1.2 GB"},
	//     {Name: "file2.srt", Size: "50 KB"},
	// }

	return nil
}
