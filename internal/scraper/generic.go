package scraper

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// GenericScraper attempts to scrape any torrent site using heuristics
type GenericScraper struct {
	name      string
	baseURL   string
	searchURL string // Discovered or configured search URL pattern
	client    *http.Client
}

// NewGenericScraper creates a scraper for an arbitrary torrent site
func NewGenericScraper(name, baseURL string) *GenericScraper {
	return &GenericScraper{
		name:    name,
		baseURL: strings.TrimRight(baseURL, "/"),
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// Name returns the source name
func (s *GenericScraper) Name() string {
	return s.name
}

// Search queries the site for torrents
func (s *GenericScraper) Search(ctx context.Context, query string) ([]Torrent, error) {
	// Try common search URL patterns
	searchPatterns := []string{
		s.baseURL + "/search/" + url.PathEscape(query) + "/",
		s.baseURL + "/search/" + url.PathEscape(query),
		s.baseURL + "/search?q=" + url.QueryEscape(query),
		s.baseURL + "/?s=" + url.QueryEscape(query),
		s.baseURL + "/torrents/?search=" + url.QueryEscape(query),
	}

	// If we've discovered a working search URL, use it first
	if s.searchURL != "" {
		searchPatterns = append([]string{
			strings.Replace(s.searchURL, "%s", url.PathEscape(query), 1),
		}, searchPatterns...)
	}

	var lastErr error
	for _, searchURL := range searchPatterns {
		results, err := s.trySearch(ctx, searchURL)
		if err != nil {
			lastErr = err
			continue
		}
		if len(results) > 0 {
			// Remember this pattern worked
			s.searchURL = strings.Replace(searchURL, url.PathEscape(query), "%s", 1)
			return results, nil
		}
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no results found with any search pattern")
}

func (s *GenericScraper) trySearch(ctx context.Context, searchURL string) ([]Torrent, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	return s.extractTorrents(doc), nil
}

// extractTorrents uses heuristics to find torrent info in any page
func (s *GenericScraper) extractTorrents(doc *goquery.Document) []Torrent {
	var results []Torrent
	seen := make(map[string]bool)

	// Strategy 1: Find all magnet links and work backwards to find info
	doc.Find("a[href^='magnet:']").Each(func(i int, link *goquery.Selection) {
		magnet, _ := link.Attr("href")
		if seen[magnet] {
			return
		}
		seen[magnet] = true

		t := Torrent{
			Source: s.name,
			Magnet: magnet,
		}

		// Extract name from magnet link dn parameter
		t.Name = extractMagnetName(magnet)

		// Look for info in parent/ancestor elements
		s.extractInfoFromContext(link, &t)

		if t.Name != "" {
			results = append(results, t)
		}
	})

	// Strategy 2: Look for torrent tables/lists if we didn't find magnets inline
	if len(results) == 0 {
		results = s.extractFromTables(doc)
	}

	return results
}

// extractInfoFromContext looks at surrounding elements for torrent metadata
func (s *GenericScraper) extractInfoFromContext(link *goquery.Selection, t *Torrent) {
	// Walk up to find a container (tr, div, li, article)
	containers := []string{"tr", "div.torrent", "div.result", "li", "article", "div"}

	for _, sel := range containers {
		parent := link.Closest(sel)
		if parent.Length() == 0 {
			continue
		}

		text := parent.Text()

		// Try to find name if we don't have one
		if t.Name == "" {
			// Look for title link
			parent.Find("a").Each(func(i int, a *goquery.Selection) {
				href, _ := a.Attr("href")
				if href != "" && !strings.HasPrefix(href, "magnet:") && a.Text() != "" {
					candidate := strings.TrimSpace(a.Text())
					if len(candidate) > len(t.Name) && !isBoilerplate(candidate) {
						t.Name = candidate
					}
				}
			})
		}

		// Extract seeders
		if t.Seeders == 0 {
			t.Seeders = extractNumber(text, []string{"seed", "se", "s:"})
		}

		// Extract leechers
		if t.Leechers == 0 {
			t.Leechers = extractNumber(text, []string{"leech", "le", "l:", "peer"})
		}

		// Extract size
		if t.Size == "" {
			t.Size = extractSize(text)
		}

		// If we found useful info, stop looking
		if t.Seeders > 0 || t.Size != "" {
			break
		}
	}
}

// extractFromTables looks for torrent data in HTML tables
func (s *GenericScraper) extractFromTables(doc *goquery.Document) []Torrent {
	var results []Torrent

	doc.Find("table").Each(func(i int, table *goquery.Selection) {
		table.Find("tr").Each(func(j int, row *goquery.Selection) {
			// Skip header rows
			if row.Find("th").Length() > 0 {
				return
			}

			t := Torrent{Source: s.name}
			text := row.Text()

			// Look for links
			row.Find("a").Each(func(k int, link *goquery.Selection) {
				href, _ := link.Attr("href")
				if strings.HasPrefix(href, "magnet:") {
					t.Magnet = href
					if t.Name == "" {
						t.Name = extractMagnetName(href)
					}
				} else if t.InfoURL == "" && strings.Contains(href, "torrent") {
					if !strings.HasPrefix(href, "http") {
						t.InfoURL = s.baseURL + href
					} else {
						t.InfoURL = href
					}
					if t.Name == "" && link.Text() != "" {
						t.Name = strings.TrimSpace(link.Text())
					}
				}
			})

			// Extract numbers and size from row text
			t.Seeders = extractNumber(text, []string{"seed"})
			t.Leechers = extractNumber(text, []string{"leech", "peer"})
			t.Size = extractSize(text)

			if t.Name != "" && (t.Magnet != "" || t.InfoURL != "") {
				results = append(results, t)
			}
		})
	})

	return results
}

// GetFiles fetches additional info from torrent detail page
func (s *GenericScraper) GetFiles(ctx context.Context, t *Torrent) error {
	if t.InfoURL == "" {
		return nil // Nothing to fetch
	}

	req, err := http.NewRequestWithContext(ctx, "GET", t.InfoURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0")

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return err
	}

	// Find magnet link if we don't have one
	if t.Magnet == "" {
		doc.Find("a[href^='magnet:']").First().Each(func(i int, sel *goquery.Selection) {
			t.Magnet, _ = sel.Attr("href")
		})
	}

	return nil
}

// ValidateURL checks if a URL is reachable and looks like a torrent site
func ValidateURL(ctx context.Context, rawURL string) (string, error) {
	// Parse and validate URL format
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}

	if parsed.Scheme == "" {
		parsed.Scheme = "https"
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("URL must be http or https")
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("URL must have a host")
	}

	normalizedURL := parsed.Scheme + "://" + parsed.Host

	// Check reachability
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", normalizedURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("site unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("site returned HTTP %d", resp.StatusCode)
	}

	// Check if it looks like a torrent site
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return "", fmt.Errorf("couldn't parse page: %w", err)
	}

	// Look for torrent indicators
	pageText := strings.ToLower(doc.Text())
	hasMagnet := doc.Find("a[href^='magnet:']").Length() > 0
	hasSearch := doc.Find("input[type='search'], input[name='q'], input[name='search'], form[action*='search']").Length() > 0
	hasTorrentWords := strings.Contains(pageText, "torrent") ||
		strings.Contains(pageText, "magnet") ||
		strings.Contains(pageText, "seeders") ||
		strings.Contains(pageText, "leechers")

	if !hasMagnet && !hasSearch && !hasTorrentWords {
		return "", fmt.Errorf("doesn't look like a torrent site")
	}

	return normalizedURL, nil
}

// TestSearch performs a test search to verify the site works
func TestSearch(ctx context.Context, baseURL string) (int, error) {
	scraper := NewGenericScraper("test", baseURL)

	// Try a common search term
	results, err := scraper.Search(ctx, "test")
	if err != nil {
		// Try another term
		results, err = scraper.Search(ctx, "linux")
		if err != nil {
			return 0, fmt.Errorf("search failed: %w", err)
		}
	}

	return len(results), nil
}

// Helper functions

func extractMagnetName(magnet string) string {
	// Parse dn (display name) from magnet link
	if idx := strings.Index(magnet, "dn="); idx != -1 {
		end := strings.Index(magnet[idx:], "&")
		var name string
		if end == -1 {
			name = magnet[idx+3:]
		} else {
			name = magnet[idx+3 : idx+end]
		}
		decoded, err := url.QueryUnescape(name)
		if err == nil {
			return decoded
		}
		return strings.ReplaceAll(name, "+", " ")
	}
	return ""
}

var sizeRegex = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*(TB|GB|MB|KB|B)\b`)

func extractSize(text string) string {
	matches := sizeRegex.FindStringSubmatch(strings.ToUpper(text))
	if len(matches) >= 3 {
		return matches[1] + " " + matches[2]
	}
	return ""
}

var numberRegex = regexp.MustCompile(`\d+`)

func extractNumber(text string, hints []string) int {
	textLower := strings.ToLower(text)

	for _, hint := range hints {
		idx := strings.Index(textLower, hint)
		if idx == -1 {
			continue
		}

		// Look for number near the hint (before or after)
		window := 50
		start := idx - window
		if start < 0 {
			start = 0
		}
		end := idx + len(hint) + window
		if end > len(text) {
			end = len(text)
		}

		snippet := text[start:end]
		matches := numberRegex.FindAllString(snippet, -1)
		for _, m := range matches {
			if n, err := strconv.Atoi(m); err == nil && n < 1000000 {
				return n
			}
		}
	}

	return 0
}

func isBoilerplate(text string) bool {
	lower := strings.ToLower(text)
	boilerplate := []string{
		"home", "search", "login", "register", "about", "contact",
		"download", "magnet", "torrent", "category", "browse",
	}
	for _, b := range boilerplate {
		if lower == b {
			return true
		}
	}
	return len(text) < 3 || len(text) > 300
}
