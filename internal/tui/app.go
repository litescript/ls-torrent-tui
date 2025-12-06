// Package tui implements the terminal user interface using Bubble Tea.
// It handles all user interaction, display rendering, and coordinates
// between the various backend services (scrapers, qBittorrent, VPN).
package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/litescript/ls-torrent-tui/internal/config"
	"github.com/litescript/ls-torrent-tui/internal/plex"
	"github.com/litescript/ls-torrent-tui/internal/qbit"
	"github.com/litescript/ls-torrent-tui/internal/scraper"
	"github.com/litescript/ls-torrent-tui/internal/theme"
	"github.com/litescript/ls-torrent-tui/internal/version"
	"github.com/litescript/ls-torrent-tui/internal/vpn"
)

// View modes
type viewMode int

const (
	viewSearch viewMode = iota
	viewResults
	viewDetails
	viewVPNConnect // Shown when VPN is disconnected
)

// Tabs
type tabType int

const (
	tabSearch tabType = iota
	tabDownloads
	tabCompleted
	tabSources
)

// SearchSource represents a configured torrent search site
type SearchSource struct {
	Name    string
	URL     string
	Enabled bool
	Scraper scraper.Scraper
	Builtin bool   // true for built-in sources, false for user-added
	Warning string // non-empty if source has issues (e.g., "search may not work")
}

// Model is the main application state
type Model struct {
	// Config
	cfg config.Config

	// Components
	searchInput textinput.Model
	spinner     spinner.Model

	// State
	mode          viewMode
	activeTab     tabType
	results       []scraper.Torrent
	cursor        int
	dlCursor      int // cursor for downloads tab
	searching     bool
	err           error
	statusMsg     string
	vpnStatus     vpn.Status
	vpnChecked    bool // Have we done initial VPN check?
	vpnConnecting bool // Are we currently connecting to VPN?
	qbitOnline    bool
	isFetching    bool // Guard against overlapping torrent fetches

	// Torrent lists from qBittorrent
	downloading []qbit.TorrentInfo
	completed   []qbit.TorrentInfo

	// Sorting (downloads tab): 0=name, 1=size, 2=done, 3=dl, 4=ul, 5=seed, 6=leech, 7=eta
	dlSortCol     int
	dlSortAsc     bool
	followingHash string // Hash of torrent to follow (keeps cursor on it during re-sorts)

	// Sorting (completed tab): 0=name, 1=size, 2=ratio, 3=uploaded
	compSortCol int
	compSortAsc bool

	// Search results sorting: 0=name, 1=size, 2=seeds, 3=leech, 4=health
	searchSortCol int
	searchSortAsc bool

	// Track which results have been sent to download (by name, since indices change with sort)
	downloaded map[string]bool

	// Search sources
	sources        []SearchSource
	srcCursor      int
	addingURL      bool // Are we adding a URL?
	validatingURL  bool // Are we validating a URL?
	validationDot  int  // Animation state for validation dots (0-2)
	urlInput       textinput.Model
	confirmingQuit bool // Are we showing the quit confirmation modal?

	// Settings modal state
	showSettings    bool              // Are we showing the settings modal?
	settingsSection int               // 0=qBit, 1=Downloads, 2=VPN, 3=Plex
	settingsField   int               // Which field is selected in current section
	settingsEditing bool              // Are we editing a field?
	settingsInputs  []textinput.Model // Text inputs for settings fields

	// Move to Plex modal state
	showMoveModal   bool                 // Are we showing the move modal?
	moveDetection   plex.DetectionResult // Auto-detected media info
	moveMediaType   plex.MediaType       // Current selection (togglable)
	moveSourcePath  string               // Full source path of selected torrent
	moveDestPreview string               // Generated destination path preview
	moveSubtitles   []string             // Found subtitle files
	moveCleanup     bool                 // Whether to delete source after move
	moveEditing     bool                 // Is user editing the title?
	moveTitleInput  textinput.Model      // Editable title field
	moveProgress    float64              // Transfer progress (0.0-1.0)
	moveInProgress  bool                 // Is a move operation running?
	moveError       string               // Error message if move failed
	moveTotalBytes  int64                // Total bytes to transfer
	moveCopiedBytes int64                // Bytes copied so far

	// Dimensions
	width  int
	height int

	// Services
	qbitClient *qbit.Client
	vpnChecker *vpn.Checker
}

// Messages
type searchResultMsg struct {
	results []scraper.Torrent
	err     error
}

type vpnStatusMsg struct {
	status vpn.Status
}

type updateCheckMsg struct {
	info version.UpdateInfo
}

type qbitStatusMsg struct {
	online bool
}

type filesLoadedMsg struct {
	index int
	err   error
}

type torrentAddedMsg struct {
	name string
	err  error
}

type vpnConnectMsg struct {
	err error
}

type urlValidateMsg struct {
	url     string
	name    string
	scraper scraper.Scraper
	err     error
	warning string
}

type torrentListMsg struct {
	downloading []qbit.TorrentInfo
	completed   []qbit.TorrentInfo
	err         error
}

type tickMsg time.Time

type torrentActionMsg struct {
	action string
	name   string
	err    error
}

type plexMoveMsg struct {
	name string
	err  error
}

// NewModel creates the initial model
func NewModel(cfg config.Config) Model {
	ti := textinput.New()
	ti.Placeholder = "Search torrents..."
	ti.Focus()
	ti.CharLimit = 256
	ti.Width = 50

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.CurrentPalette.Accent))

	// URL input for adding sources
	urlIn := textinput.New()
	urlIn.Placeholder = "Paste torrent site URL..."
	urlIn.CharLimit = 512
	urlIn.Width = 60

	// Settings inputs (10 fields total)
	// qBit: host, port, username, password (indices 0-3)
	// Downloads: path (index 4)
	// VPN: status_script, connect_script (indices 5-6)
	// Plex: movie_library, tv_library, use_sudo (indices 7-9)
	settingsInputs := make([]textinput.Model, 10)
	for i := range settingsInputs {
		settingsInputs[i] = textinput.New()
		settingsInputs[i].CharLimit = 256
		settingsInputs[i].Width = 50
	}
	// Set initial values from config
	settingsInputs[0].SetValue(cfg.QBittorrent.Host)
	settingsInputs[1].SetValue(fmt.Sprintf("%d", cfg.QBittorrent.Port))
	settingsInputs[2].SetValue(cfg.QBittorrent.Username)
	settingsInputs[3].SetValue(cfg.QBittorrent.Password)
	settingsInputs[3].EchoMode = textinput.EchoPassword
	settingsInputs[4].SetValue(cfg.Downloads.Path)
	settingsInputs[5].SetValue(cfg.VPN.StatusScript)
	settingsInputs[6].SetValue(cfg.VPN.ConnectScript)
	settingsInputs[7].SetValue(cfg.Plex.MovieLibrary)
	settingsInputs[8].SetValue(cfg.Plex.TVLibrary)
	if cfg.Plex.UseSudo {
		settingsInputs[9].SetValue("yes")
	} else {
		settingsInputs[9].SetValue("no")
	}

	// Initialize search sources from config
	// No built-in sources - users add their own via the Sources tab
	var sources []SearchSource
	for _, src := range cfg.Sources {
		sources = append(sources, SearchSource{
			Name:    src.Name,
			URL:     src.URL,
			Enabled: src.Enabled,
			Scraper: scraper.NewGenericScraper(src.Name, src.URL),
			Builtin: false,
			Warning: src.Warning,
		})
	}

	qbitClient := qbit.NewClient(
		cfg.QBittorrent.Host,
		cfg.QBittorrent.Port,
		cfg.QBittorrent.Username,
		cfg.QBittorrent.Password,
	)

	vpnChecker := vpn.NewChecker(cfg.VPN.StatusScript, cfg.VPN.ConnectScript)

	return Model{
		cfg:            cfg,
		searchInput:    ti,
		spinner:        sp,
		urlInput:       urlIn,
		mode:           viewSearch,
		sources:        sources,
		qbitClient:     qbitClient,
		vpnChecker:     vpnChecker,
		searchSortCol:  cfg.Sort.SearchCol,
		searchSortAsc:  cfg.Sort.SearchAsc,
		dlSortCol:      cfg.Sort.DownloadsCol,
		dlSortAsc:      cfg.Sort.DownloadsAsc,
		compSortCol:    cfg.Sort.CompletedCol,
		compSortAsc:    cfg.Sort.CompletedAsc,
		downloaded:     make(map[string]bool),
		settingsInputs: settingsInputs,
	}
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		m.checkVPNStatus(),
		m.checkQbitStatus(),
		m.fetchTorrents(),
		tickCmd(),
	)
}

// tickCmd returns a command that ticks every 2 seconds
func tickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// Update handles messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		newModel, cmd := m.handleKeyPress(msg)
		if cmd != nil {
			// Key was handled, return with command
			return newModel, cmd
		}
		// Key wasn't fully handled, continue to text input
		m = newModel.(Model)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.searchInput.Width = msg.Width - 20

	case spinner.TickMsg:
		if m.searching || m.vpnConnecting || m.validatingURL {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			cmds = append(cmds, cmd)
			// Cycle validation dots for animation
			if m.validatingURL {
				m.validationDot = (m.validationDot + 1) % 3
			}
		}

	case searchResultMsg:
		m.searching = false
		if msg.err != nil {
			m.err = msg.err
			m.statusMsg = fmt.Sprintf("Search failed: %v", msg.err)
		} else if len(msg.results) == 0 {
			m.statusMsg = "No results found"
			m.results = nil
		} else {
			m.results = msg.results
			// Apply current sort settings to new results
			sortSearchResults(m.results, m.searchSortCol, m.searchSortAsc)
			m.cursor = 0
			m.mode = viewResults
			m.statusMsg = fmt.Sprintf("Found %d results", len(m.results))
			// Clear downloaded indicators for new search
			m.downloaded = make(map[string]bool)
		}

	case vpnStatusMsg:
		m.vpnStatus = msg.status
		wasChecked := m.vpnChecked
		m.vpnChecked = true

		// On initial check, if VPN is disconnected, show connect prompt
		if !wasChecked && !m.vpnStatus.Connected {
			m.mode = viewVPNConnect
			m.statusMsg = "VPN required - press Enter to connect or q to quit"
			m.searchInput.Blur() // Unfocus so keys work
		} else if wasChecked {
			// Manual refresh - show status
			if m.vpnStatus.Connected {
				m.statusMsg = "VPN: " + m.vpnStatus.StatusString()
			} else {
				m.statusMsg = "VPN: Disconnected!"
			}
		}

		// If we were in VPN connect mode and now connected, go to search
		if m.mode == viewVPNConnect && m.vpnStatus.Connected {
			m.mode = viewSearch
			m.statusMsg = "VPN connected!"
			m.searchInput.Focus()
		}

	case qbitStatusMsg:
		m.qbitOnline = msg.online

	case updateCheckMsg:
		if msg.info.Error != nil {
			m.statusMsg = fmt.Sprintf("Update check failed: %v", msg.info.Error)
		} else if msg.info.UpdateAvailable {
			m.statusMsg = fmt.Sprintf("Update available: v%s -> v%s (run: %s)",
				msg.info.CurrentVersion, msg.info.LatestVersion, version.InstallCommand())
		} else {
			m.statusMsg = fmt.Sprintf("You're on the latest version (v%s)", msg.info.CurrentVersion)
		}

	case filesLoadedMsg:
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("Error loading files: %v", msg.err)
			m.mode = viewResults // Reset to results mode on error
		} else if msg.index < len(m.results) && len(m.results[msg.index].Files) > 0 {
			m.statusMsg = fmt.Sprintf("Loaded %d files", len(m.results[msg.index].Files))
		} else {
			m.statusMsg = "No file details available"
			m.mode = viewResults // Reset to results mode when no details
		}

	case torrentAddedMsg:
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("Error: %v", msg.err)
		} else {
			m.statusMsg = fmt.Sprintf("Added: %s", TruncateString(msg.name, 40))
			// Mark as downloaded so we can show indicator in results
			m.downloaded[msg.name] = true
		}

	case vpnConnectMsg:
		m.vpnConnecting = false
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("VPN connection failed: %v", msg.err)
		} else {
			m.statusMsg = "VPN connecting... checking status"
			// Re-check VPN status after connect attempt
			cmds = append(cmds, m.checkVPNStatus())
		}

	case urlValidateMsg:
		m.validatingURL = false
		m.addingURL = false
		m.urlInput.Blur()
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("Invalid source: %v", msg.err)
		} else {
			m.sources = append(m.sources, SearchSource{
				Name:    msg.name,
				URL:     msg.url,
				Enabled: true,
				Scraper: msg.scraper,
				Builtin: false,
				Warning: msg.warning,
			})
			m.saveSources()
			m.statusMsg = fmt.Sprintf("Added source: %s%s", msg.name, msg.warning)
		}

	case moveCompleteMsg:
		m.moveInProgress = false
		if msg.err != nil {
			m.moveError = msg.err.Error()
		} else {
			m.showMoveModal = false
			m.statusMsg = fmt.Sprintf("Moved to Plex: %s", TruncateString(msg.result.DestinationPath, 40))
			// Refresh torrent list to reflect changes
			return m, m.fetchTorrents()
		}

	case torrentListMsg:
		m.isFetching = false // Clear guard regardless of success/failure
		if msg.err == nil {
			m.downloading = msg.downloading
			m.completed = msg.completed
			// Apply current sort settings
			sortTorrents(m.downloading, m.dlSortCol, m.dlSortAsc)
			sortCompletedTorrents(m.completed, m.compSortCol, m.compSortAsc)
			// Update cursor to follow tracked torrent
			if m.followingHash != "" {
				if idx, found := findTorrentByHash(m.downloading, m.followingHash); found {
					m.dlCursor = idx
				} else {
					// Torrent completed or was removed - stop following
					m.followingHash = ""
				}
			}
		}

	case tickMsg:
		// Periodic refresh - fetch torrents and schedule next tick
		// Skip if a fetch is already in-flight to prevent goroutine pile-up
		if !m.isFetching {
			m.isFetching = true
			cmds = append(cmds, m.fetchTorrents())
		}
		cmds = append(cmds, tickCmd())

	case torrentActionMsg:
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("%s failed: %v", msg.action, msg.err)
		} else {
			m.statusMsg = fmt.Sprintf("%s: %s", msg.action, TruncateString(msg.name, 30))
		}
		// Refresh torrent list after action
		cmds = append(cmds, m.fetchTorrents())

	case plexMoveMsg:
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("Plex move failed: %v", msg.err)
		} else {
			m.statusMsg = fmt.Sprintf("Moved to Plex: %s", TruncateString(msg.name, 30))
		}
	}

	// Update text inputs (only when not in VPN connect mode)
	if m.mode != viewVPNConnect {
		if m.addingURL {
			var cmd tea.Cmd
			m.urlInput, cmd = m.urlInput.Update(msg)
			cmds = append(cmds, cmd)
		} else {
			var cmd tea.Cmd
			m.searchInput, cmd = m.searchInput.Update(msg)
			cmds = append(cmds, cmd)
		}
	}

	return m, tea.Batch(cmds...)
}

// handled returns a no-op command to signal the key was handled
func handled() tea.Cmd {
	return func() tea.Msg { return nil }
}

func (m Model) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Global quit - always works
	if key == "ctrl+c" {
		return m, tea.Quit
	}

	// Handle VPN connect mode specially
	if m.mode == viewVPNConnect {
		switch key {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "enter", "V":
			if !m.vpnConnecting {
				m.vpnConnecting = true
				m.statusMsg = "Connecting to VPN..."
				return m, tea.Batch(m.spinner.Tick, m.connectVPN())
			}
		}
		return m, handled()
	}

	// Handle quit confirmation modal
	if m.confirmingQuit {
		switch key {
		case "q", "y", "enter":
			return m, tea.Quit
		case "ctrl+c":
			return m, tea.Quit
		default:
			// Any other key cancels
			m.confirmingQuit = false
			return m, handled()
		}
	}

	// Handle move modal
	if m.showMoveModal {
		return m.handleMoveModalKey(key)
	}

	// Handle settings modal
	if m.showSettings {
		return m.handleSettingsKey(key)
	}

	// When adding URL in sources tab
	if m.addingURL && m.urlInput.Focused() {
		switch key {
		case "ctrl+c", "esc":
			m.addingURL = false
			m.validatingURL = false
			m.urlInput.Blur()
			return m, handled()
		case "alt+1":
			m.addingURL = false
			m.urlInput.Blur()
			m.activeTab = tabSearch
			m.mode = viewSearch
			return m, handled()
		case "alt+2":
			m.addingURL = false
			m.urlInput.Blur()
			m.activeTab = tabDownloads
			m.dlCursor = 0
			return m, handled()
		case "alt+3":
			m.addingURL = false
			m.urlInput.Blur()
			m.activeTab = tabCompleted
			m.dlCursor = 0
			return m, handled()
		case "alt+4":
			m.addingURL = false
			m.urlInput.Blur()
			m.activeTab = tabSources
			m.srcCursor = 0
			return m, handled()
		case "enter":
			if m.validatingURL {
				return m, handled() // Already validating
			}
			rawURL := strings.TrimSpace(m.urlInput.Value())
			if rawURL != "" {
				m.validatingURL = true
				m.statusMsg = "Validating URL..."
				return m, tea.Batch(m.spinner.Tick, m.validateURL(rawURL))
			}
			m.addingURL = false
			m.urlInput.Blur()
			return m, handled()
		}
		// All other keys go to URL input
		return m, nil
	}

	// When search input is focused (INPUT MODE), only handle specific keys
	// Let everything else go to the text input
	if m.searchInput.Focused() {
		switch key {
		case "ctrl+c":
			return m, tea.Quit
		case "ctrl+u":
			// Clear search input and results
			m.searchInput.SetValue("")
			m.results = nil
			m.cursor = 0
			m.mode = viewSearch
			m.statusMsg = ""
			m.downloaded = make(map[string]bool)
			return m, handled()
		case "alt+1":
			m.activeTab = tabSearch
			// Preserve results view if we have results
			if len(m.results) > 0 {
				m.mode = viewResults
			} else {
				m.mode = viewSearch
			}
			m.addingURL = false
			return m, handled()
		case "alt+2":
			m.searchInput.Blur()
			m.activeTab = tabDownloads
			m.dlCursor = 0
			m.addingURL = false
			return m, handled()
		case "alt+3":
			m.searchInput.Blur()
			m.activeTab = tabCompleted
			m.dlCursor = 0
			m.addingURL = false
			return m, handled()
		case "alt+4":
			m.searchInput.Blur()
			m.activeTab = tabSources
			m.srcCursor = 0
			m.addingURL = false
			return m, handled()
		case "esc":
			m.searchInput.Blur()
			return m, handled()
		case "enter":
			if m.searchInput.Value() != "" {
				if !m.vpnStatus.Connected {
					m.statusMsg = "VPN required! Press V to connect"
					return m, handled()
				}
				m.searching = true
				m.err = nil
				m.statusMsg = "Searching..."
				m.searchInput.Blur()
				return m, tea.Batch(m.spinner.Tick, m.doSearch())
			}
			return m, handled()
		}
		// All other keys go to text input
		return m, nil
	}

	// Tab switching (works in any mode when not typing)
	switch key {
	case "1", "alt+1":
		m.activeTab = tabSearch
		// Preserve results view if we have results
		if len(m.results) > 0 {
			m.mode = viewResults
		} else {
			m.mode = viewSearch
		}
		m.addingURL = false
		return m, handled()
	case "2", "alt+2":
		m.activeTab = tabDownloads
		m.dlCursor = 0
		m.addingURL = false
		return m, handled()
	case "3", "alt+3":
		m.activeTab = tabCompleted
		m.dlCursor = 0
		m.addingURL = false
		return m, handled()
	case "4", "alt+4":
		m.activeTab = tabSources
		m.srcCursor = 0
		m.addingURL = false
		return m, handled()
	}

	// Search input NOT focused (CMD MODE) - handle navigation keys
	switch key {
	case "ctrl+c":
		return m, tea.Quit
	case "q":
		// Show quit confirmation modal
		m.confirmingQuit = true
		return m, handled()

	case "esc":
		m.mode = viewSearch
		return m, handled()

	case "ctrl+u":
		// Clear search and enter input mode (works from any tab)
		m.activeTab = tabSearch
		m.searchInput.SetValue("")
		m.results = nil
		m.cursor = 0
		m.mode = viewSearch
		m.statusMsg = ""
		m.downloaded = make(map[string]bool)
		m.searchInput.Focus()
		return m, handled()

	case "enter":
		// Context-dependent enter action
		if m.activeTab == tabSources && len(m.sources) > 0 && m.srcCursor < len(m.sources) {
			m.sources[m.srcCursor].Enabled = !m.sources[m.srcCursor].Enabled
			if m.sources[m.srcCursor].Enabled {
				m.statusMsg = fmt.Sprintf("Enabled: %s", m.sources[m.srcCursor].Name)
			} else {
				m.statusMsg = fmt.Sprintf("Disabled: %s", m.sources[m.srcCursor].Name)
			}
			m.saveSources()
			return m, handled()
		}
		if m.activeTab == tabSearch && m.mode == viewResults && len(m.results) > 0 {
			if !m.vpnStatus.Connected {
				m.statusMsg = "VPN required! Press V to connect"
				return m, handled()
			}
			return m, m.downloadTorrent()
		}
		return m, handled()

	case "up", "k":
		switch m.activeTab {
		case tabSearch:
			if (m.mode == viewResults || m.mode == viewDetails) && m.cursor > 0 {
				m.cursor--
			}
		case tabDownloads:
			if m.dlCursor > 0 {
				m.dlCursor--
			}
		case tabCompleted:
			if m.dlCursor > 0 {
				m.dlCursor--
			}
		case tabSources:
			if m.srcCursor > 0 {
				m.srcCursor--
			}
		}
		return m, handled()

	case "down", "j":
		switch m.activeTab {
		case tabSearch:
			if (m.mode == viewResults || m.mode == viewDetails) && m.cursor < len(m.results)-1 {
				m.cursor++
			}
		case tabDownloads:
			if m.dlCursor < len(m.downloading)-1 {
				m.dlCursor++
			}
		case tabCompleted:
			if m.dlCursor < len(m.completed)-1 {
				m.dlCursor++
			}
		case tabSources:
			if m.srcCursor < len(m.sources)-1 {
				m.srcCursor++
			}
		}
		return m, handled()

	case "left", "h":
		// Navigate sort columns
		if m.activeTab == tabSearch && (m.mode == viewResults || m.mode == viewDetails) {
			if m.searchSortCol > 0 {
				m.searchSortCol--
			} else {
				m.searchSortCol = 4 // Wrap to last column (5 columns)
			}
			sortSearchResults(m.results, m.searchSortCol, m.searchSortAsc)
			m.saveSortSettings()
			return m, handled()
		}
		if m.activeTab == tabDownloads {
			if m.dlSortCol > 0 {
				m.dlSortCol--
			} else {
				m.dlSortCol = 7 // Wrap to last column (8 columns)
			}
			sortTorrents(m.downloading, m.dlSortCol, m.dlSortAsc)
			m.saveSortSettings()
			return m, handled()
		}
		if m.activeTab == tabCompleted {
			if m.compSortCol > 0 {
				m.compSortCol--
			} else {
				m.compSortCol = 3 // Wrap to last column (4 columns)
			}
			sortCompletedTorrents(m.completed, m.compSortCol, m.compSortAsc)
			m.saveSortSettings()
			return m, handled()
		}

	case "right", "l":
		// Navigate sort columns
		if m.activeTab == tabSearch && (m.mode == viewResults || m.mode == viewDetails) {
			if m.searchSortCol < 4 {
				m.searchSortCol++
			} else {
				m.searchSortCol = 0 // Wrap to first column
			}
			sortSearchResults(m.results, m.searchSortCol, m.searchSortAsc)
			m.saveSortSettings()
			return m, handled()
		}
		if m.activeTab == tabDownloads {
			if m.dlSortCol < 7 {
				m.dlSortCol++
			} else {
				m.dlSortCol = 0 // Wrap to first column
			}
			sortTorrents(m.downloading, m.dlSortCol, m.dlSortAsc)
			m.saveSortSettings()
			return m, handled()
		}
		if m.activeTab == tabCompleted {
			if m.compSortCol < 3 {
				m.compSortCol++
			} else {
				m.compSortCol = 0 // Wrap to first column
			}
			sortCompletedTorrents(m.completed, m.compSortCol, m.compSortAsc)
			m.saveSortSettings()
			return m, handled()
		}

	case "s": // Toggle sort direction
		if m.activeTab == tabSearch && (m.mode == viewResults || m.mode == viewDetails) {
			m.searchSortAsc = !m.searchSortAsc
			sortSearchResults(m.results, m.searchSortCol, m.searchSortAsc)
			m.saveSortSettings()
			return m, handled()
		}
		if m.activeTab == tabDownloads {
			m.dlSortAsc = !m.dlSortAsc
			sortTorrents(m.downloading, m.dlSortCol, m.dlSortAsc)
			m.saveSortSettings()
			return m, handled()
		}
		if m.activeTab == tabCompleted {
			m.compSortAsc = !m.compSortAsc
			sortCompletedTorrents(m.completed, m.compSortCol, m.compSortAsc)
			m.saveSortSettings()
			return m, handled()
		}

	case "space":
		// Toggle source enabled/disabled
		if m.activeTab == tabSources && len(m.sources) > 0 && m.srcCursor < len(m.sources) {
			m.sources[m.srcCursor].Enabled = !m.sources[m.srcCursor].Enabled
			if m.sources[m.srcCursor].Enabled {
				m.statusMsg = fmt.Sprintf("Enabled: %s", m.sources[m.srcCursor].Name)
			} else {
				m.statusMsg = fmt.Sprintf("Disabled: %s", m.sources[m.srcCursor].Name)
			}
			m.saveSources()
			return m, handled()
		}

	case "a": // Add URL (sources tab)
		if m.activeTab == tabSources {
			m.addingURL = true
			m.urlInput.Focus()
			m.urlInput.SetValue("")
			return m, handled()
		}

	case "d": // Details - load files for selected torrent
		if m.activeTab == tabSearch && (m.mode == viewResults || m.mode == viewDetails) && len(m.results) > 0 {
			m.mode = viewDetails
			m.statusMsg = "Loading file details..."
			return m, m.loadFiles()
		}
		return m, handled()

	case "p": // Pause/Resume toggle
		if m.activeTab == tabDownloads && len(m.downloading) > 0 {
			return m, m.togglePauseTorrent()
		}
		return m, handled()

	case "f": // Follow/unfollow torrent (keep cursor on it during re-sorts)
		if m.activeTab == tabDownloads && len(m.downloading) > 0 && m.dlCursor < len(m.downloading) {
			t := m.downloading[m.dlCursor]
			if m.followingHash == t.Hash {
				m.followingHash = ""
				m.statusMsg = "Unfollowed: " + TruncateString(t.Name, 30)
			} else {
				m.followingHash = t.Hash
				m.statusMsg = "Following: " + TruncateString(t.Name, 30)
			}
			return m, handled()
		}
		return m, handled()

	case "x", "delete": // Delete torrent or remove source
		if m.activeTab == tabDownloads && len(m.downloading) > 0 {
			return m, m.deleteTorrent(false)
		}
		if m.activeTab == tabCompleted && len(m.completed) > 0 {
			return m, m.deleteTorrent(false)
		}
		if m.activeTab == tabSources && len(m.sources) > 0 && m.srcCursor < len(m.sources) {
			src := m.sources[m.srcCursor]
			if src.Builtin {
				m.statusMsg = "Cannot remove built-in source"
				return m, handled()
			}
			m.sources = append(m.sources[:m.srcCursor], m.sources[m.srcCursor+1:]...)
			if m.srcCursor >= len(m.sources) && m.srcCursor > 0 {
				m.srcCursor--
			}
			m.saveSources()
			m.statusMsg = fmt.Sprintf("Removed: %s", src.Name)
			return m, handled()
		}
		return m, handled()

	case "X": // Delete with files
		if m.activeTab == tabDownloads && len(m.downloading) > 0 {
			return m, m.deleteTorrent(true)
		}
		if m.activeTab == tabCompleted && len(m.completed) > 0 {
			return m, m.deleteTorrent(true)
		}
		return m, handled()

	case "m": // Move to Plex
		if m.activeTab == tabCompleted && len(m.completed) > 0 {
			return m.openMoveModal()
		}
		return m, handled()

	case "v":
		return m, m.checkVPNStatus()

	case "V":
		if !m.vpnStatus.Connected && !m.vpnConnecting {
			m.vpnConnecting = true
			m.statusMsg = "Connecting to VPN..."
			return m, tea.Batch(m.spinner.Tick, m.connectVPN())
		}
		return m, handled()

	case "u":
		m.statusMsg = "Checking for updates..."
		return m, checkForUpdate()

	case "c": // Open settings modal
		m.showSettings = true
		m.settingsSection = 0
		m.settingsField = 0
		m.settingsEditing = false
		// Refresh input values from current config
		m.settingsInputs[0].SetValue(m.cfg.QBittorrent.Host)
		m.settingsInputs[1].SetValue(fmt.Sprintf("%d", m.cfg.QBittorrent.Port))
		m.settingsInputs[2].SetValue(m.cfg.QBittorrent.Username)
		m.settingsInputs[3].SetValue(m.cfg.QBittorrent.Password)
		m.settingsInputs[4].SetValue(m.cfg.Downloads.Path)
		m.settingsInputs[5].SetValue(m.cfg.VPN.StatusScript)
		m.settingsInputs[6].SetValue(m.cfg.VPN.ConnectScript)
		m.settingsInputs[7].SetValue(m.cfg.Plex.MovieLibrary)
		m.settingsInputs[8].SetValue(m.cfg.Plex.TVLibrary)
		if m.cfg.Plex.UseSudo {
			m.settingsInputs[9].SetValue("yes")
		} else {
			m.settingsInputs[9].SetValue("no")
		}
		return m, handled()

	case "/", "i": // / or i to focus search input (preserves results)
		m.activeTab = tabSearch
		m.searchInput.Focus()
		// Keep results visible if we have them, but allow editing query
		if len(m.results) == 0 {
			m.mode = viewSearch
		}
		return m, handled()

	case "tab":
		if m.mode == viewResults {
			m.mode = viewDetails
		} else if m.mode == viewDetails {
			m.mode = viewResults
		}
		return m, handled()
	}

	return m, handled()
}

// Commands
func (m Model) doSearch() tea.Cmd {
	query := m.searchInput.Value()
	sources := m.sources

	return func() tea.Msg {
		var allResults []scraper.Torrent
		var lastErr error

		// Search all enabled sources
		for _, src := range sources {
			if !src.Enabled || src.Scraper == nil {
				continue
			}

			results, err := src.Scraper.Search(context.Background(), query)
			if err != nil {
				lastErr = err
				continue
			}
			allResults = append(allResults, results...)
		}

		// Filter out obvious garbage (no seeds, no leechers, no size = sidebar/ad links)
		filtered := make([]scraper.Torrent, 0, len(allResults))
		for _, t := range allResults {
			// Keep if has any activity or size info
			if t.Seeders > 0 || t.Leechers > 0 || t.Size != "" {
				filtered = append(filtered, t)
			}
		}
		allResults = filtered

		// Sorting is applied in searchResultMsg handler using user's sort settings

		if len(allResults) == 0 && lastErr != nil {
			return searchResultMsg{err: lastErr}
		}

		return searchResultMsg{results: allResults}
	}
}

func (m Model) checkVPNStatus() tea.Cmd {
	return func() tea.Msg {
		status := m.vpnChecker.Check(context.Background())
		return vpnStatusMsg{status: status}
	}
}

func (m Model) connectVPN() tea.Cmd {
	checker := m.vpnChecker
	return func() tea.Msg {
		err := checker.Connect(context.Background())
		return vpnConnectMsg{err: err}
	}
}

func checkForUpdate() tea.Cmd {
	return func() tea.Msg {
		info := version.CheckForUpdate()
		return updateCheckMsg{info: info}
	}
}

func (m Model) validateURL(rawURL string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Validate URL format and reachability
		normalizedURL, err := scraper.ValidateURL(ctx, rawURL)
		if err != nil {
			return urlValidateMsg{url: rawURL, err: err}
		}

		// Test search to verify it works - non-fatal, just warn
		var warning string
		resultCount, err := scraper.TestSearch(ctx, normalizedURL)
		if err != nil {
			warning = " (search may not work)"
		} else if resultCount == 0 {
			warning = " (search may not work - no test results)"
		}

		// Create scraper regardless - let user try it
		s := scraper.NewGenericScraper(normalizedURL, normalizedURL)

		return urlValidateMsg{
			url:     normalizedURL,
			name:    normalizedURL,
			scraper: s,
			warning: warning,
		}
	}
}

// saveSources saves custom (non-builtin) sources to config
func (m Model) saveSources() {
	var customSources []config.SourceConfig
	for _, src := range m.sources {
		if !src.Builtin {
			customSources = append(customSources, config.SourceConfig{
				Name:    src.Name,
				URL:     src.URL,
				Enabled: src.Enabled,
				Warning: src.Warning,
			})
		}
	}
	m.cfg.Sources = customSources
	_ = config.Save(m.cfg) // Ignore error, it's just persistence
}

// saveSortSettings saves sort preferences to config
func (m Model) saveSortSettings() {
	m.cfg.Sort.SearchCol = m.searchSortCol
	m.cfg.Sort.SearchAsc = m.searchSortAsc
	m.cfg.Sort.DownloadsCol = m.dlSortCol
	m.cfg.Sort.DownloadsAsc = m.dlSortAsc
	m.cfg.Sort.CompletedCol = m.compSortCol
	m.cfg.Sort.CompletedAsc = m.compSortAsc
	_ = config.Save(m.cfg) // Ignore error, it's just persistence
}

// settingsSectionFields returns the field indices for each section
// Section 0 (qBit): fields 0-3 (host, port, username, password)
// Section 1 (Downloads): field 4 (path)
// Section 2 (VPN): fields 5-6 (status_script, connect_script)
// Section 3 (Plex): fields 7-9 (movie_library, tv_library, use_sudo)
func settingsSectionFields(section int) []int {
	switch section {
	case 0:
		return []int{0, 1, 2, 3}
	case 1:
		return []int{4}
	case 2:
		return []int{5, 6}
	case 3:
		return []int{7, 8, 9}
	default:
		return []int{}
	}
}

// handleSettingsKey handles keyboard input for the settings modal
func (m Model) handleSettingsKey(key string) (tea.Model, tea.Cmd) {
	fields := settingsSectionFields(m.settingsSection)

	// If editing a field, handle text input
	if m.settingsEditing {
		fieldIdx := fields[m.settingsField]
		switch key {
		case "esc":
			m.settingsEditing = false
			m.settingsInputs[fieldIdx].Blur()
			return m, handled()
		case "enter":
			m.settingsEditing = false
			m.settingsInputs[fieldIdx].Blur()
			return m, handled()
		case "tab":
			// Tab completion for path fields
			if isPathField(fieldIdx) {
				m.completePathInput(fieldIdx)
				return m, handled()
			}
			return m, handled()
		default:
			// Let the text input handle it
			var cmd tea.Cmd
			m.settingsInputs[fieldIdx], cmd = m.settingsInputs[fieldIdx].Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
			return m, cmd
		}
	}

	// Not editing - handle navigation
	switch key {
	case "esc", "c":
		// Close settings without saving
		m.showSettings = false
		return m, handled()

	case "enter":
		// Save and close
		m.saveSettings()
		m.showSettings = false
		m.statusMsg = "Settings saved"
		return m, handled()

	case "tab", "right", "l":
		// Next section
		m.settingsSection = (m.settingsSection + 1) % 4
		m.settingsField = 0
		return m, handled()

	case "shift+tab", "left", "h":
		// Previous section
		m.settingsSection = (m.settingsSection + 3) % 4 // +3 is same as -1 mod 4
		m.settingsField = 0
		return m, handled()

	case "up", "k":
		// Previous field in section
		if m.settingsField > 0 {
			m.settingsField--
		}
		return m, handled()

	case "down", "j":
		// Next field in section
		if m.settingsField < len(fields)-1 {
			m.settingsField++
		}
		return m, handled()

	case "i", " ":
		// Edit current field
		if len(fields) > 0 {
			fieldIdx := fields[m.settingsField]
			m.settingsEditing = true
			m.settingsInputs[fieldIdx].Focus()
			return m, textinput.Blink
		}
		return m, handled()

	case "ctrl+c":
		return m, tea.Quit
	}

	return m, handled()
}

// completePathInput performs tab completion on a path input field.
// Returns true if completion was performed.
func (m *Model) completePathInput(fieldIdx int) bool {
	input := m.settingsInputs[fieldIdx].Value()
	if input == "" {
		return false
	}

	// Expand ~ to home directory
	if strings.HasPrefix(input, "~/") {
		home, _ := os.UserHomeDir()
		input = filepath.Join(home, input[2:])
	} else if input == "~" {
		home, _ := os.UserHomeDir()
		m.settingsInputs[fieldIdx].SetValue(home + "/")
		m.settingsInputs[fieldIdx].SetCursor(len(home) + 1)
		return true
	}

	// Get directory and prefix to match
	dir := filepath.Dir(input)
	prefix := filepath.Base(input)

	// If input ends with /, we're completing inside that directory
	if strings.HasSuffix(input, "/") {
		dir = input
		prefix = ""
	}

	// Read directory entries
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}

	// Find matches
	var matches []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, prefix) {
			fullPath := filepath.Join(dir, name)
			if entry.IsDir() {
				fullPath += "/"
			}
			matches = append(matches, fullPath)
		}
	}

	if len(matches) == 0 {
		return false
	}

	if len(matches) == 1 {
		// Single match - complete it
		result := matches[0]
		// Convert back to ~ prefix if it was used
		home, _ := os.UserHomeDir()
		if strings.HasPrefix(result, home) {
			result = "~" + result[len(home):]
		}
		m.settingsInputs[fieldIdx].SetValue(result)
		m.settingsInputs[fieldIdx].SetCursor(len(result))
		return true
	}

	// Multiple matches - complete to longest common prefix
	common := matches[0]
	for _, match := range matches[1:] {
		for i := 0; i < len(common) && i < len(match); i++ {
			if common[i] != match[i] {
				common = common[:i]
				break
			}
		}
		if len(match) < len(common) {
			common = common[:len(match)]
		}
	}

	if len(common) > len(input) {
		// Convert back to ~ prefix if it was used
		home, _ := os.UserHomeDir()
		if strings.HasPrefix(common, home) {
			common = "~" + common[len(home):]
		}
		m.settingsInputs[fieldIdx].SetValue(common)
		m.settingsInputs[fieldIdx].SetCursor(len(common))
		return true
	}

	return false
}

// isPathField returns true if the settings field index is a path input
func isPathField(fieldIdx int) bool {
	// 4=Download Path, 5=VPN Status, 6=VPN Connect, 7=Movie Library, 8=TV Library
	return fieldIdx >= 4 && fieldIdx <= 8
}

// saveSettings saves the current settings input values to config
func (m *Model) saveSettings() {
	m.cfg.QBittorrent.Host = m.settingsInputs[0].Value()
	// Parse port, default to 8080 on error
	port := 8080
	if _, err := fmt.Sscanf(m.settingsInputs[1].Value(), "%d", &port); err == nil {
		m.cfg.QBittorrent.Port = port
	}
	m.cfg.QBittorrent.Username = m.settingsInputs[2].Value()
	m.cfg.QBittorrent.Password = m.settingsInputs[3].Value()
	m.cfg.Downloads.Path = m.settingsInputs[4].Value()
	m.cfg.VPN.StatusScript = m.settingsInputs[5].Value()
	m.cfg.VPN.ConnectScript = m.settingsInputs[6].Value()
	m.cfg.Plex.MovieLibrary = m.settingsInputs[7].Value()
	m.cfg.Plex.TVLibrary = m.settingsInputs[8].Value()
	useSudoVal := strings.ToLower(m.settingsInputs[9].Value())
	m.cfg.Plex.UseSudo = useSudoVal == "yes" || useSudoVal == "true" || useSudoVal == "1"

	// Validate Plex library paths
	var warnings []string
	if m.cfg.Plex.MovieLibrary != "" {
		if info, err := os.Stat(m.cfg.Plex.MovieLibrary); err != nil {
			warnings = append(warnings, "Movie library path not found")
		} else if !info.IsDir() {
			warnings = append(warnings, "Movie library is not a directory")
		}
	}
	if m.cfg.Plex.TVLibrary != "" {
		if info, err := os.Stat(m.cfg.Plex.TVLibrary); err != nil {
			warnings = append(warnings, "TV library path not found")
		} else if !info.IsDir() {
			warnings = append(warnings, "TV library is not a directory")
		}
	}

	// Save to disk
	_ = config.Save(m.cfg)

	// Set warning status if paths invalid
	if len(warnings) > 0 {
		m.statusMsg = "Settings saved. Warning: " + strings.Join(warnings, ", ")
	}

	// Recreate clients with new config
	m.qbitClient = qbit.NewClient(
		m.cfg.QBittorrent.Host,
		m.cfg.QBittorrent.Port,
		m.cfg.QBittorrent.Username,
		m.cfg.QBittorrent.Password,
	)
	m.vpnChecker = vpn.NewChecker(m.cfg.VPN.StatusScript, m.cfg.VPN.ConnectScript)
}

// openMoveModal opens the move to Plex modal for the selected torrent
func (m Model) openMoveModal() (tea.Model, tea.Cmd) {
	// Validate config - paths must be set
	if m.cfg.Plex.MovieLibrary == "" || m.cfg.Plex.TVLibrary == "" {
		m.statusMsg = "Configure Plex libraries in Settings (c) first"
		return m, handled()
	}

	// Validate that library paths exist and are directories
	if info, err := os.Stat(m.cfg.Plex.MovieLibrary); err != nil {
		m.statusMsg = fmt.Sprintf("Movie library not found: %s", m.cfg.Plex.MovieLibrary)
		return m, handled()
	} else if !info.IsDir() {
		m.statusMsg = "Movie library path is not a directory"
		return m, handled()
	}

	if info, err := os.Stat(m.cfg.Plex.TVLibrary); err != nil {
		m.statusMsg = fmt.Sprintf("TV library not found: %s", m.cfg.Plex.TVLibrary)
		return m, handled()
	} else if !info.IsDir() {
		m.statusMsg = "TV library path is not a directory"
		return m, handled()
	}

	if len(m.completed) == 0 || m.dlCursor >= len(m.completed) {
		return m, handled()
	}

	t := m.completed[m.dlCursor]
	sourcePath := filepath.Join(t.SavePath, t.Name)

	// Run detection
	detection, _ := plex.DetectFromPath(sourcePath)
	if detection.Type == plex.MediaTypeUnknown {
		// Default to movie if detection failed
		detection.Type = plex.MediaTypeMovie
		detection.Title = plex.SanitizeFilename(t.Name)
	}

	m.showMoveModal = true
	m.moveDetection = detection
	m.moveMediaType = detection.Type
	m.moveSourcePath = sourcePath
	m.moveCleanup = true
	m.moveError = ""
	m.moveInProgress = false
	m.moveProgress = 0

	// Initialize title input
	m.moveTitleInput = textinput.New()
	m.moveTitleInput.SetValue(detection.Title)
	m.moveTitleInput.CharLimit = 200
	m.moveTitleInput.Width = 50

	// Find subtitles
	m.moveSubtitles = plex.FindSubtitles(sourcePath)

	// Generate destination preview
	m.updateMoveDestPreview()

	return m, handled()
}

// updateMoveDestPreview updates the destination preview based on current settings
func (m *Model) updateMoveDestPreview() {
	ext := ".mkv" // Default extension
	if video, err := plex.FindMainVideo(m.moveSourcePath); err == nil {
		ext = filepath.Ext(video)
	}

	title := m.moveTitleInput.Value()
	if title == "" {
		title = m.moveDetection.Title
	}

	switch m.moveMediaType {
	case plex.MediaTypeMovie:
		if m.moveDetection.Year > 0 {
			m.moveDestPreview = filepath.Join(
				m.cfg.Plex.MovieLibrary,
				fmt.Sprintf("%s (%d)", title, m.moveDetection.Year),
				fmt.Sprintf("%s (%d)%s", title, m.moveDetection.Year, ext),
			)
		} else {
			m.moveDestPreview = filepath.Join(m.cfg.Plex.MovieLibrary, title, title+ext)
		}
	case plex.MediaTypeTV:
		m.moveDestPreview = filepath.Join(
			m.cfg.Plex.TVLibrary,
			title,
			fmt.Sprintf("Season %02d", m.moveDetection.Season),
			filepath.Base(m.moveSourcePath),
		)
	}
}

// handleMoveModalKey handles keyboard input for the move modal
func (m Model) handleMoveModalKey(key string) (tea.Model, tea.Cmd) {
	// If editing title, handle text input
	if m.moveEditing {
		switch key {
		case "esc":
			m.moveEditing = false
			m.moveTitleInput.Blur()
			return m, handled()
		case "enter":
			m.moveEditing = false
			m.moveTitleInput.Blur()
			m.moveDetection.Title = m.moveTitleInput.Value()
			m.updateMoveDestPreview()
			return m, handled()
		default:
			var cmd tea.Cmd
			m.moveTitleInput, cmd = m.moveTitleInput.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
			return m, cmd
		}
	}

	// If move in progress, only allow escape to cancel
	if m.moveInProgress {
		return m, handled()
	}

	switch key {
	case "esc", "m":
		m.showMoveModal = false
		return m, handled()

	case "tab", "t":
		// Toggle media type
		if m.moveMediaType == plex.MediaTypeMovie {
			m.moveMediaType = plex.MediaTypeTV
		} else {
			m.moveMediaType = plex.MediaTypeMovie
		}
		m.updateMoveDestPreview()
		return m, handled()

	case "i":
		// Edit title
		m.moveEditing = true
		m.moveTitleInput.Focus()
		return m, handled()

	case "c":
		// Toggle cleanup
		m.moveCleanup = !m.moveCleanup
		return m, handled()

	case "enter":
		// Start move operation
		return m.startMoveOperation()

	case "ctrl+c":
		return m, tea.Quit
	}

	return m, handled()
}

// Move operation messages
type moveProgressMsg struct {
	progress float64
	copied   int64
	total    int64
}

type moveCompleteMsg struct {
	result *plex.MoveResult
	err    error
}

// startMoveOperation begins the async move operation
func (m Model) startMoveOperation() (tea.Model, tea.Cmd) {
	m.moveInProgress = true
	m.moveProgress = 0
	m.moveError = ""

	// Get file size for progress tracking
	if video, err := plex.FindMainVideo(m.moveSourcePath); err == nil {
		if info, err := os.Stat(video); err == nil {
			m.moveTotalBytes = info.Size()
		}
	}

	// Build detection with current settings
	detection := m.moveDetection
	detection.Type = m.moveMediaType
	detection.Title = m.moveTitleInput.Value()

	sourcePath := m.moveSourcePath
	cleanup := m.moveCleanup
	movieLib := m.cfg.Plex.MovieLibrary
	tvLib := m.cfg.Plex.TVLibrary
	useSudo := m.cfg.Plex.UseSudo

	return m, func() tea.Msg {
		mover := plex.NewMover(plex.MoveConfig{
			MovieLibraryPath: movieLib,
			TVLibraryPath:    tvLib,
			UseSudo:          useSudo,
		})

		// Create progress channel
		progressChan := make(chan plex.MoveProgress, 10)
		defer close(progressChan)

		// Run move in goroutine
		resultChan := make(chan struct {
			result *plex.MoveResult
			err    error
		}, 1)

		go func() {
			result, err := mover.MoveToLibraryWithProgress(
				context.Background(),
				sourcePath,
				detection,
				cleanup,
				progressChan,
			)
			resultChan <- struct {
				result *plex.MoveResult
				err    error
			}{result, err}
		}()

		// Wait for completion (progress updates are handled by rsync parsing)
		res := <-resultChan
		return moveCompleteMsg{result: res.result, err: res.err}
	}
}

func (m Model) checkQbitStatus() tea.Cmd {
	return func() tea.Msg {
		online := m.qbitClient.IsConnected(context.Background())
		return qbitStatusMsg{online: online}
	}
}

func (m Model) fetchTorrents() tea.Cmd {
	client := m.qbitClient
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		torrents, err := client.GetTorrents(ctx)
		if err != nil {
			return torrentListMsg{err: err}
		}

		var downloading, completed []qbit.TorrentInfo
		for _, t := range torrents {
			// States: downloading, stalledDL, pausedDL, queuedDL, checkingDL
			// completed: uploading, stalledUP, pausedUP, queuedUP, checkingUP, completed
			switch t.State {
			case "downloading", "stalledDL", "pausedDL", "queuedDL", "checkingDL", "metaDL", "forcedDL":
				downloading = append(downloading, t)
			default:
				// Everything else is considered completed/seeding
				if t.Progress >= 1.0 {
					completed = append(completed, t)
				} else {
					downloading = append(downloading, t)
				}
			}
		}

		return torrentListMsg{downloading: downloading, completed: completed}
	}
}

func (m Model) togglePauseTorrent() tea.Cmd {
	if m.dlCursor >= len(m.downloading) {
		return nil
	}
	t := m.downloading[m.dlCursor]
	client := m.qbitClient
	isPaused := strings.Contains(t.State, "paused")

	return func() tea.Msg {
		var err error
		var action string
		if isPaused {
			err = client.Resume(context.Background(), t.Hash)
			action = "Resumed"
		} else {
			err = client.Pause(context.Background(), t.Hash)
			action = "Paused"
		}
		return torrentActionMsg{action: action, name: t.Name, err: err}
	}
}

func (m Model) deleteTorrent(deleteFiles bool) tea.Cmd {
	var t qbit.TorrentInfo
	if m.activeTab == tabDownloads && m.dlCursor < len(m.downloading) {
		t = m.downloading[m.dlCursor]
	} else if m.activeTab == tabCompleted && m.dlCursor < len(m.completed) {
		t = m.completed[m.dlCursor]
	} else {
		return nil
	}

	client := m.qbitClient
	return func() tea.Msg {
		err := client.Delete(context.Background(), t.Hash, deleteFiles)
		action := "Removed"
		if deleteFiles {
			action = "Deleted"
		}
		return torrentActionMsg{action: action, name: t.Name, err: err}
	}
}

func (m Model) moveToPlexMovie() tea.Cmd {
	if m.dlCursor >= len(m.completed) {
		return nil
	}
	t := m.completed[m.dlCursor]
	savePath := t.SavePath

	return func() tea.Msg {
		home, _ := os.UserHomeDir()
		script := filepath.Join(home, "Scripts", "move-to-plex.sh")
		cmd := exec.CommandContext(context.Background(), "/bin/sh", script, savePath, t.Name)
		err := cmd.Run()
		return plexMoveMsg{name: t.Name, err: err}
	}
}

func (m Model) moveToPlexTV() tea.Cmd {
	if m.dlCursor >= len(m.completed) {
		return nil
	}
	t := m.completed[m.dlCursor]
	savePath := t.SavePath

	return func() tea.Msg {
		home, _ := os.UserHomeDir()
		script := filepath.Join(home, "Scripts", "move-to-plex-tv.sh")
		cmd := exec.CommandContext(context.Background(), "/bin/sh", script, savePath, t.Name)
		err := cmd.Run()
		return plexMoveMsg{name: t.Name, err: err}
	}
}

func (m Model) loadFiles() tea.Cmd {
	if m.cursor >= len(m.results) {
		return nil
	}
	idx := m.cursor
	t := &m.results[idx]

	// Find the scraper for this torrent's source
	var src scraper.Scraper
	for _, s := range m.sources {
		if s.Name == t.Source {
			src = s.Scraper
			break
		}
	}
	if src == nil {
		return nil
	}

	return func() tea.Msg {
		err := src.GetFiles(context.Background(), t)
		return filesLoadedMsg{index: idx, err: err}
	}
}

func (m Model) downloadTorrent() tea.Cmd {
	if m.cursor >= len(m.results) {
		return nil
	}
	t := m.results[m.cursor]
	client := m.qbitClient
	savePath := m.cfg.Downloads.Path

	// Find the scraper for this torrent's source
	var src scraper.Scraper
	for _, s := range m.sources {
		if s.Name == t.Source {
			src = s.Scraper
			break
		}
	}

	return func() tea.Msg {
		// Get magnet if not already loaded
		if t.Magnet == "" || !strings.HasPrefix(t.Magnet, "magnet:") {
			if src != nil {
				if err := src.GetFiles(context.Background(), &t); err != nil {
					return torrentAddedMsg{err: err}
				}
			}
		}

		// Some sources provide .torrent URLs instead of magnets
		// qBittorrent can handle both
		if t.Magnet == "" {
			return torrentAddedMsg{err: fmt.Errorf("no download link available")}
		}

		err := client.AddMagnet(context.Background(), t.Magnet, savePath)
		return torrentAddedMsg{name: t.Name, err: err}
	}
}

// View renders the UI
func (m Model) View() string {
	styles := GetStyles()

	var b strings.Builder

	// Logo header (always visible)
	b.WriteString(m.renderLogo())

	// Status bar (mode, status, help) + connection indicators
	b.WriteString(m.renderStatusBar())
	b.WriteString("\n\n")

	// Tab bar
	tabBar := m.renderTabBar()
	b.WriteString(tabBar)
	b.WriteString("\n\n")

	// Main content - logo is ~12 lines, status ~1, tabs ~2
	contentHeight := m.height - 18
	if contentHeight < 5 {
		contentHeight = 5
	}

	// Handle VPN connect mode specially
	if m.mode == viewVPNConnect {
		if m.vpnConnecting {
			b.WriteString(m.spinner.View() + " Connecting to VPN (selecting lowest latency US server)...")
		} else {
			b.WriteString(styles.Error.Render("VPN Required"))
			b.WriteString("\n\n")
			b.WriteString(styles.Muted.Render("Torrent operations require an active VPN connection."))
			b.WriteString("\n\n")
			b.WriteString(styles.HelpDesc.Render("Press Enter to connect to NordVPN"))
			b.WriteString("\n")
			b.WriteString(styles.Muted.Render("Press q to quit"))
		}
	} else {
		// Render based on active tab
		switch m.activeTab {
		case tabSearch:
			b.WriteString(m.renderSearchTab(contentHeight))
		case tabDownloads:
			b.WriteString(m.renderDownloadsTab(contentHeight))
		case tabCompleted:
			b.WriteString(m.renderCompletedTab(contentHeight))
		case tabSources:
			b.WriteString(m.renderSourcesTab(contentHeight))
		}
	}

	// Get the base content
	baseContent := b.String()

	// Overlay modal if active
	if m.showMoveModal {
		return m.overlayModal(baseContent, m.renderMoveModal())
	}
	if m.showSettings {
		return m.overlayModal(baseContent, m.renderSettingsModal())
	}
	if m.confirmingQuit {
		return m.overlayModal(baseContent, m.renderQuitModal())
	}

	return baseContent
}

// overlayModal renders a modal over the base content with the base still visible
func (m Model) overlayModal(base, modal string) string {
	// Safety: if dimensions not set, just return the modal
	if m.width == 0 || m.height == 0 {
		return modal
	}

	baseLines := strings.Split(base, "\n")
	modalLines := strings.Split(modal, "\n")

	// Fixed position: after logo (~12 lines down), centered horizontally
	topOffset := 12
	modalWidth := lipgloss.Width(modal)
	leftOffset := (m.width - modalWidth) / 2
	if leftOffset < 0 {
		leftOffset = 0
	}
	if leftOffset > 500 {
		leftOffset = 0 // Safety bound
	}

	// Overlay modal lines onto base
	for i, modalLine := range modalLines {
		baseIdx := topOffset + i
		if baseIdx >= len(baseLines) {
			// Extend base if needed
			for len(baseLines) <= baseIdx {
				baseLines = append(baseLines, "")
			}
		}

		// Build the line with modal overlaid
		padding := strings.Repeat(" ", leftOffset)
		baseLines[baseIdx] = padding + modalLine
	}

	return strings.Join(baseLines, "\n")
}

// truncateToWidth truncates a string to fit within a given display width
func truncateToWidth(s string, maxWidth int) string {
	var result strings.Builder
	width := 0
	for _, r := range s {
		rWidth := 1
		if r > 127 {
			rWidth = 2 // Rough estimate for wide chars
		}
		if width+rWidth > maxWidth {
			break
		}
		result.WriteRune(r)
		width += rWidth
	}
	return result.String()
}

// renderQuitModal renders the quit confirmation modal
func (m Model) renderQuitModal() string {
	styles := GetStyles()

	modalStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(theme.CurrentPalette.Accent)).
		Background(lipgloss.Color(theme.CurrentPalette.BG)).
		Padding(1, 3)

	modalContent := styles.Title.Render("Quit?") + "\n\n" +
		styles.Muted.Render("Press ") + styles.HelpKey.Render("q") + styles.Muted.Render(" or ") +
		styles.HelpKey.Render("enter") + styles.Muted.Render(" to quit, any other key to cancel")

	return modalStyle.Render(modalContent)
}

// renderMoveModal renders the move to Plex modal
func (m Model) renderMoveModal() string {
	styles := GetStyles()

	// Fixed size modal for consistent window feel
	modalStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(theme.CurrentPalette.Accent)).
		Background(lipgloss.Color(theme.CurrentPalette.BG)).
		Padding(1, 2).
		Width(70).
		Height(18)

	var content strings.Builder
	content.WriteString(styles.Title.Render("Move to Plex"))
	content.WriteString("\n\n")

	// Media type toggle
	movieLabel := " Movie "
	tvLabel := " TV "
	if m.moveMediaType == plex.MediaTypeMovie {
		movieLabel = styles.Title.Render("[Movie]")
		tvLabel = styles.Muted.Render(" TV ")
	} else {
		movieLabel = styles.Muted.Render(" Movie ")
		tvLabel = styles.Title.Render("[TV]")
	}
	content.WriteString(fmt.Sprintf("  Type:        %s  %s\n", movieLabel, tvLabel))

	// Title (editable)
	if m.moveEditing {
		content.WriteString(fmt.Sprintf("  Title:       %s\n", m.moveTitleInput.View()))
	} else {
		titleStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.CurrentPalette.Accent)).
			Bold(true)
		content.WriteString(fmt.Sprintf("  Title:       %s\n", titleStyle.Render(m.moveDetection.Title)))
	}

	// Year/Season info
	if m.moveMediaType == plex.MediaTypeMovie {
		if m.moveDetection.Year > 0 {
			content.WriteString(fmt.Sprintf("  Year:        %d\n", m.moveDetection.Year))
		}
	} else {
		content.WriteString(fmt.Sprintf("  Season:      %d  Episode: %d\n",
			m.moveDetection.Season, m.moveDetection.Episode))
	}

	content.WriteString("\n")

	// Source path
	content.WriteString(fmt.Sprintf("  Source:      %s\n",
		styles.Muted.Render(TruncateString(m.moveSourcePath, 52))))

	// Destination preview
	content.WriteString(fmt.Sprintf("  Destination: %s\n",
		styles.VPNConnected.Render(TruncateString(m.moveDestPreview, 52))))

	// Subtitles count
	if len(m.moveSubtitles) > 0 {
		content.WriteString(fmt.Sprintf("  Subtitles:   %d files found\n", len(m.moveSubtitles)))
	}

	content.WriteString("\n")

	// Cleanup toggle
	cleanupStr := "  [ ] Delete source after move"
	if m.moveCleanup {
		cleanupStr = "  [×] Delete source after move"
	}
	content.WriteString(styles.Muted.Render(cleanupStr))
	content.WriteString("\n\n")

	// Progress bar (if moving)
	if m.moveInProgress {
		content.WriteString(m.renderProgressBar())
		content.WriteString("\n")
	} else if m.moveError != "" {
		content.WriteString(styles.Error.Render("  " + m.moveError))
		content.WriteString("\n")
	}

	// Help text
	if m.moveEditing {
		content.WriteString(styles.Muted.Render("  [esc]Cancel  [enter]Save"))
	} else if m.moveInProgress {
		content.WriteString(styles.Muted.Render("  Transfer in progress..."))
	} else {
		content.WriteString(styles.Muted.Render("  [tab]Type  [i]Edit  [c]Cleanup  [enter]Move  [esc]Cancel"))
	}

	return modalStyle.Render(content.String())
}

// renderProgressBar renders a truecolor gradient progress bar (sunset palette)
func (m Model) renderProgressBar() string {
	width := 50
	filled := int(m.moveProgress * float64(width))
	if filled > width {
		filled = width
	}

	var bar strings.Builder
	bar.WriteString("  [")

	// Render filled portion with sunset gradient (dark rust -> golden yellow)
	for i := 0; i < filled; i++ {
		color := progressGradientColor(i, filled)
		style := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
		bar.WriteString(style.Render("█"))
	}

	// Render empty portion
	emptyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(theme.CurrentPalette.Muted))
	for i := filled; i < width; i++ {
		bar.WriteString(emptyStyle.Render("░"))
	}

	bar.WriteString("] ")

	// Percentage and size
	pct := int(m.moveProgress * 100)
	bar.WriteString(fmt.Sprintf("%3d%%", pct))

	if m.moveTotalBytes > 0 {
		bar.WriteString(fmt.Sprintf(" - %s / %s",
			formatSize(m.moveCopiedBytes),
			formatSize(m.moveTotalBytes)))
	}

	return bar.String()
}

// progressGradientColor returns a color for the progress bar gradient.
// Goes from dark rust (#8B2500) at start to golden yellow (#FFCC00) at end.
func progressGradientColor(pos, total int) string {
	if total <= 0 {
		total = 1
	}
	ratio := float64(pos) / float64(total)

	// Dark rust (#8B2500) -> Orange (#FF6B35) -> Golden yellow (#FFCC00)
	var r, g, b float64

	if ratio < 0.5 {
		// Dark rust to orange
		t := ratio / 0.5
		r = 139 + t*(255-139)
		g = 37 + t*(107-37)
		b = 0 + t*(53-0)
	} else {
		// Orange to golden yellow
		t := (ratio - 0.5) / 0.5
		r = 255 + t*(255-255)
		g = 107 + t*(204-107)
		b = 53 + t*(0-53)
	}

	return fmt.Sprintf("#%02X%02X%02X", int(r), int(g), int(b))
}

// renderSettingsModal renders the settings configuration modal
func (m Model) renderSettingsModal() string {
	styles := GetStyles()

	// Modal container style - fixed size based on largest section (qBittorrent: 4 fields)
	modalStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(theme.CurrentPalette.Accent)).
		Background(lipgloss.Color(theme.CurrentPalette.BG)).
		Padding(1, 2).
		Width(70).
		Height(14)

	// Section tab styles
	activeTabStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.CurrentPalette.Accent)).
		Bold(true).
		Padding(0, 1)
	inactiveTabStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.CurrentPalette.Muted)).
		Padding(0, 1)

	// Section tabs
	sections := []string{"qBittorrent", "Downloads", "VPN", "Plex"}
	var tabBar strings.Builder
	for i, name := range sections {
		if i == m.settingsSection {
			tabBar.WriteString(activeTabStyle.Render("[" + name + "]"))
		} else {
			tabBar.WriteString(inactiveTabStyle.Render(" " + name + " "))
		}
	}

	var content strings.Builder
	content.WriteString(styles.Title.Render("Settings"))
	content.WriteString("\n\n")
	content.WriteString(tabBar.String())
	content.WriteString("\n\n")

	// Field labels for each section
	fieldLabels := map[int][]string{
		0: {"Host", "Port", "Username", "Password"},
		1: {"Download Path"},
		2: {"Status Script", "Connect Script"},
		3: {"Movie Library", "TV Library", "Use Sudo (yes/no)"},
	}

	// Render fields for current section
	fields := settingsSectionFields(m.settingsSection)
	labels := fieldLabels[m.settingsSection]

	for i, fieldIdx := range fields {
		label := labels[i]
		isSelected := i == m.settingsField
		isEditing := isSelected && m.settingsEditing

		// Label
		var labelStr string
		if isSelected {
			labelStr = styles.Title.Render("› " + label + ":")
		} else {
			labelStr = styles.Muted.Render("  " + label + ":")
		}

		// Value
		var valueStr string
		if isEditing {
			valueStr = m.settingsInputs[fieldIdx].View()
		} else {
			val := m.settingsInputs[fieldIdx].Value()
			if val == "" {
				val = "(not set)"
			}
			// Mask password
			if fieldIdx == 3 && val != "(not set)" {
				val = strings.Repeat("•", len(val))
			}
			if isSelected {
				// Use accent color for selected field value
				selectedStyle := lipgloss.NewStyle().
					Foreground(lipgloss.Color(theme.CurrentPalette.Accent)).
					Bold(true)
				valueStr = selectedStyle.Render(val)
			} else {
				valueStr = styles.Muted.Render(val)
			}
		}

		content.WriteString(fmt.Sprintf("%-20s %s\n", labelStr, valueStr))
	}

	// Help text
	content.WriteString("\n")
	if m.settingsEditing {
		content.WriteString(styles.Muted.Render("[esc/enter] Done editing"))
	} else {
		content.WriteString(styles.Muted.Render("[tab]Section [↑↓]Field [i]Edit [enter]Save [esc]Cancel"))
	}

	return modalStyle.Render(content.String())
}

// logoGradientColor returns a hex color for a position in the logo gradient.
// Creates a sunset effect: golden yellow -> orange -> deep rust
func logoGradientColor(col, row, width, height int) string {
	// Normalize positions to 0-1
	xRatio := float64(col) / float64(width)
	yRatio := float64(row) / float64(height)

	// Horizontal gradient - sunset orange palette
	// Golden Yellow (#FFCC00) -> Vibrant Orange (#FF6B35) -> Deep Rust (#8B2500)
	var r, g, b float64

	if xRatio < 0.5 {
		// Golden Yellow to Vibrant Orange
		t := xRatio / 0.5
		r = 255 + t*(255-255)
		g = 204 + t*(107-204)
		b = 0 + t*(53-0)
	} else {
		// Vibrant Orange to Deep Rust
		t := (xRatio - 0.5) / 0.5
		r = 255 + t*(139-255)
		g = 107 + t*(37-107)
		b = 53 + t*(0-53)
	}

	// Vertical fade: brighter at top, darker toward bottom
	brightnessFactor := 1.0 - (yRatio * 0.5)
	r *= brightnessFactor
	g *= brightnessFactor
	b *= brightnessFactor

	// Clamp to valid range
	ri := int(r)
	gi := int(g)
	bi := int(b)
	if ri > 255 {
		ri = 255
	}
	if gi > 255 {
		gi = 255
	}
	if bi > 255 {
		bi = 255
	}
	if ri < 0 {
		ri = 0
	}
	if gi < 0 {
		gi = 0
	}
	if bi < 0 {
		bi = 0
	}

	return fmt.Sprintf("#%02X%02X%02X", ri, gi, bi)
}

func (m Model) renderLogo() string {
	// ASCII art with smooth truecolor gradient
	logo := []string{
		`  ██╗     ███████╗    ████████╗ ██████╗ ██████╗ ██████╗ ███████╗███╗   ██╗████████╗`,
		`  ██║     ██╔════╝    ╚══██╔══╝██╔═══██╗██╔══██╗██╔══██╗██╔════╝████╗  ██║╚══██╔══╝`,
		`  ██║     ███████╗█████╗ ██║   ██║   ██║██████╔╝██████╔╝█████╗  ██╔██╗ ██║   ██║   `,
		`  ██║     ╚════██║╚════╝ ██║   ██║   ██║██╔══██╗██╔══██╗██╔══╝  ██║╚██╗██║   ██║   `,
		`  ███████╗███████║       ██║   ╚██████╔╝██║  ██║██║  ██║███████╗██║ ╚████║   ██║   `,
		`  ╚══════╝╚══════╝       ╚═╝    ╚═════╝ ╚═╝  ╚═╝╚═╝  ╚═╝╚══════╝╚═╝  ╚═══╝   ╚═╝   `,
	}

	var b strings.Builder
	b.WriteString("\n")

	// Render each line with a horizontal truecolor gradient
	for row, line := range logo {
		runes := []rune(line)
		lineLen := len(runes)

		for col, r := range runes {
			// Create a smooth gradient based on position
			color := logoGradientColor(col, row, lineLen, len(logo))
			style := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
			b.WriteString(style.Render(string(r)))
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")

	// Tagline and copyright
	styles := GetStyles()
	tagline := "  Search torrents across multiple sources"
	b.WriteString(styles.Muted.Render(tagline))
	b.WriteString("\n")
	copyright := fmt.Sprintf("  (c) 2025 litescript.net | v%s | [u]check update", version.Version)
	b.WriteString(styles.Muted.Render(copyright))
	b.WriteString("\n\n")

	return b.String()
}

func (m Model) renderTabBar() string {
	styles := GetStyles()

	// Count enabled sources
	enabledSources := 0
	for _, s := range m.sources {
		if s.Enabled {
			enabledSources++
		}
	}

	tabs := []struct {
		name  string
		tab   tabType
		count int
	}{
		{"[1]Search", tabSearch, len(m.results)},
		{"[2]Downloads", tabDownloads, len(m.downloading)},
		{"[3]Completed", tabCompleted, len(m.completed)},
		{"[4]Sources", tabSources, enabledSources},
	}

	var parts []string
	for _, t := range tabs {
		label := t.name
		if t.count > 0 {
			label = fmt.Sprintf("%s(%d)", t.name, t.count)
		}

		if t.tab == m.activeTab {
			parts = append(parts, styles.Title.Render(label))
		} else {
			parts = append(parts, styles.Muted.Render(label))
		}
	}

	tabLine := strings.Join(parts, "  ")
	hint := styles.Muted.Render("Alt+1-4 to switch tabs")

	return tabLine + "\n" + hint
}

func (m Model) renderSearchTab(height int) string {
	styles := GetStyles()
	var b strings.Builder

	// Search bar
	prompt := styles.SearchPrompt.Render("Search: ")
	b.WriteString(prompt + m.searchInput.View())
	b.WriteString("\n")

	switch m.mode {
	case viewSearch:
		if m.err != nil {
			b.WriteString(styles.Error.Render(fmt.Sprintf("Error: %v", m.err)))
		} else if m.searching {
			b.WriteString(m.spinner.View() + " Searching...")
		}
	case viewResults, viewDetails:
		b.WriteString(m.renderResults(height - 1))
	}

	return b.String()
}

// sortTorrents sorts a slice of torrents by the specified column
func sortTorrents(torrents []qbit.TorrentInfo, col int, asc bool) {
	sort.Slice(torrents, func(i, j int) bool {
		var less bool
		switch col {
		case 0: // Name
			less = strings.ToLower(torrents[i].Name) < strings.ToLower(torrents[j].Name)
		case 1: // Size
			less = torrents[i].Size < torrents[j].Size
		case 2: // Done/Progress
			less = torrents[i].Progress < torrents[j].Progress
		case 3: // DL speed
			less = torrents[i].DLSpeed < torrents[j].DLSpeed
		case 4: // UL speed
			less = torrents[i].UPSpeed < torrents[j].UPSpeed
		case 5: // Seeds
			less = torrents[i].NumSeeds < torrents[j].NumSeeds
		case 6: // Leechers
			less = torrents[i].NumLeechers < torrents[j].NumLeechers
		case 7: // ETA
			etaI := calcETA(torrents[i].AmountLeft, torrents[i].DLSpeed)
			etaJ := calcETA(torrents[j].AmountLeft, torrents[j].DLSpeed)
			less = etaI < etaJ
		default:
			less = torrents[i].Name < torrents[j].Name
		}
		if asc {
			return less
		}
		return !less
	})
}

func calcETA(amountLeft, dlSpeed int64) int64 {
	if dlSpeed == 0 {
		return 1<<62 - 1 // Very large number for infinite
	}
	return amountLeft / dlSpeed
}

// findTorrentByHash returns the index and whether a torrent was found by hash
func findTorrentByHash(torrents []qbit.TorrentInfo, hash string) (int, bool) {
	for i, t := range torrents {
		if t.Hash == hash {
			return i, true
		}
	}
	return 0, false
}

// sortSearchResults sorts search results (5 columns: name, size, seeds, leech, health)
func sortSearchResults(results []scraper.Torrent, col int, asc bool) {
	sort.Slice(results, func(i, j int) bool {
		var less bool
		switch col {
		case 0: // Name
			less = strings.ToLower(results[i].Name) < strings.ToLower(results[j].Name)
		case 1: // Size (string comparison - not ideal but works for display)
			less = results[i].Size < results[j].Size
		case 2: // Seeds
			less = results[i].Seeders < results[j].Seeders
		case 3: // Leech
			less = results[i].Leechers < results[j].Leechers
		case 4: // Health
			less = results[i].Health() < results[j].Health()
		default:
			less = results[i].Health() < results[j].Health()
		}
		if asc {
			return less
		}
		return !less
	})
}

// sortCompletedTorrents sorts completed torrents (4 columns: name, size, ratio, uploaded)
func sortCompletedTorrents(torrents []qbit.TorrentInfo, col int, asc bool) {
	sort.Slice(torrents, func(i, j int) bool {
		var less bool
		switch col {
		case 0: // Name
			less = strings.ToLower(torrents[i].Name) < strings.ToLower(torrents[j].Name)
		case 1: // Size
			less = torrents[i].Size < torrents[j].Size
		case 2: // Ratio
			ratioI := float64(torrents[i].UploadedEver) / float64(torrents[i].Size)
			ratioJ := float64(torrents[j].UploadedEver) / float64(torrents[j].Size)
			less = ratioI < ratioJ
		case 3: // Uploaded
			less = torrents[i].UploadedEver < torrents[j].UploadedEver
		default:
			less = torrents[i].Name < torrents[j].Name
		}
		if asc {
			return less
		}
		return !less
	})
}

func (m Model) renderDownloadsTab(height int) string {
	styles := GetStyles()
	var b strings.Builder

	if len(m.downloading) == 0 {
		b.WriteString(styles.Muted.Render("No active downloads"))
		return b.String()
	}

	// Fixed column widths for right-side columns
	sizeW, doneW, dlW, ulW, seedW, leechW, etaW := 8, 7, 11, 11, 5, 6, 8
	rightColsWidth := sizeW + doneW + dlW + ulW + seedW + leechW + etaW + 7 // 7 spaces between
	nameWidth := m.width - 2 - rightColsWidth                               // 2 for prefix
	if nameWidth < 20 {
		nameWidth = 20
	}

	// Build header with per-column styling
	colNames := []string{"NAME", "SIZE", "DONE", "DL", "UL", "SEED", "LEECH", "ETA"}
	colWidths := []int{nameWidth, sizeW, doneW, dlW, ulW, seedW, leechW, etaW}

	var headerRow strings.Builder
	headerRow.WriteString("  ") // prefix
	for i, name := range colNames {
		w := colWidths[i]
		ind := " "
		if i == m.dlSortCol {
			if m.dlSortAsc {
				ind = "▲"
			} else {
				ind = "▼"
			}
		}

		var colText string
		if i == 0 {
			// NAME: left-align, indicator after name
			colText = name + ind + repeat(" ", w-len(name)-1)
		} else {
			// Others: right-align, indicator before name
			colText = " " + repeat(" ", w-len(name)-1) + ind + name
		}

		// Style sorted column differently
		if i == m.dlSortCol {
			headerRow.WriteString(styles.SortedHeader.Render(colText))
		} else {
			headerRow.WriteString(styles.Muted.Render(colText))
		}
	}

	// Add underline border
	headerStyle := lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderForeground(lipgloss.Color(theme.CurrentPalette.Muted))
	b.WriteString(headerStyle.Render(headerRow.String()))
	b.WriteString("\n")

	// Rows
	visibleRows := height - 2
	if visibleRows < 1 {
		visibleRows = 1
	}

	startIdx := 0
	if m.dlCursor >= visibleRows {
		startIdx = m.dlCursor - visibleRows + 1
	}

	endIdx := startIdx + visibleRows
	if endIdx > len(m.downloading) {
		endIdx = len(m.downloading)
	}

	for i := startIdx; i < endIdx; i++ {
		t := m.downloading[i]
		name := TruncateString(t.Name, nameWidth-1)
		progress := fmt.Sprintf("%.1f%%", t.Progress*100)
		dlSpeed := formatSpeed(t.DLSpeed)
		ulSpeed := formatSpeed(t.UPSpeed)
		eta := formatETA(t.AmountLeft, t.DLSpeed)
		size := formatSize(t.Size)
		seeds := fmt.Sprintf("%d", t.NumSeeds)
		leechers := fmt.Sprintf("%d", t.NumLeechers)

		// Build row with same spacing as header
		row := PadRight(name, nameWidth) +
			" " + PadLeft(size, sizeW) +
			" " + PadLeft(progress, doneW) +
			" " + PadLeft(dlSpeed, dlW) +
			" " + PadLeft(ulSpeed, ulW) +
			" " + PadLeft(seeds, seedW) +
			" " + PadLeft(leechers, leechW) +
			" " + PadLeft(eta, etaW)

		isFollowing := m.followingHash == t.Hash
		if i == m.dlCursor {
			if isFollowing {
				b.WriteString(styles.VPNConnected.Render("◉ ") + styles.TableSelected.Render(row))
			} else {
				b.WriteString(styles.TableSelected.Render("› " + row))
			}
		} else {
			if isFollowing {
				b.WriteString(styles.VPNConnected.Render("◉ ") + styles.TableRow.Render(row))
			} else {
				b.WriteString(styles.TableRow.Render("  " + row))
			}
		}
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) renderCompletedTab(height int) string {
	styles := GetStyles()
	var b strings.Builder

	if len(m.completed) == 0 {
		b.WriteString(styles.Muted.Render("No completed torrents"))
		return b.String()
	}

	// Column widths - must match row widths exactly
	// Rows have 2-char prefix ("› " or "  "), so header needs it too
	colWidths := []int{0, 8, 7, 11}           // nameWidth set below, others fixed
	nameWidth := m.width - 2 - 8 - 7 - 11 - 3 // 2=prefix, 3=spaces between cols
	if nameWidth < 20 {
		nameWidth = 20
	}
	colWidths[0] = nameWidth

	colNames := []string{"NAME", "SIZE", "RATIO", "UPLOADED"}

	// Build header with sort indicator - sorted column gets highlighted
	var headerParts []string
	for i, name := range colNames {
		w := colWidths[i]
		ind := " "
		if i == m.compSortCol {
			if m.compSortAsc {
				ind = "▲"
			} else {
				ind = "▼"
			}
		}
		// Build column text
		// NAME (col 0): left-align, arrow after name
		// Others: right-align, arrow prepended
		var colText string
		if i == 0 {
			// NAME: left-align, indicator right after name
			padding := w - len(name) - 1
			if padding < 0 {
				padding = 0
			}
			colText = name + ind + repeat(" ", padding)
		} else {
			// Others: right-align, indicator prepended
			padding := w - len(name) - 1
			if padding < 0 {
				padding = 0
			}
			colText = repeat(" ", padding) + ind + name
		}
		// Apply style - sorted column highlighted, others muted
		if i == m.compSortCol {
			headerParts = append(headerParts, styles.SortedHeader.Render(colText))
		} else {
			headerParts = append(headerParts, styles.Muted.Render(colText))
		}
	}
	// Add 2-char prefix to match row prefix ("› " or "  ")
	header := "  " + strings.Join(headerParts, styles.Muted.Render(" "))
	// Render with border only (no foreground color override)
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderForeground(lipgloss.Color(theme.CurrentPalette.Muted))
	b.WriteString(headerStyle.Render(header))
	b.WriteString("\n")

	// Torrents are already sorted in-place when sort changes or list refreshes

	// Rows
	visibleRows := height - 2
	if visibleRows < 1 {
		visibleRows = 1
	}

	startIdx := 0
	if m.dlCursor >= visibleRows {
		startIdx = m.dlCursor - visibleRows + 1
	}

	endIdx := startIdx + visibleRows
	if endIdx > len(m.completed) {
		endIdx = len(m.completed)
	}

	for i := startIdx; i < endIdx; i++ {
		t := m.completed[i]
		name := TruncateString(t.Name, nameWidth-2) // -2 for "› " prefix
		size := formatSize(t.Size)
		ratio := fmt.Sprintf("%.2f", float64(t.UploadedEver)/float64(t.Size))
		uploaded := formatSize(t.UploadedEver)

		// Match header widths exactly: nameWidth, 8, 7, 11
		// All left-aligned except UPLOADED (right-aligned)
		row := fmt.Sprintf("%s %s %s %s",
			PadRight(name, nameWidth),
			PadRight(size, 8),
			PadRight(ratio, 7),
			PadLeft(uploaded, 11))

		if i == m.dlCursor {
			b.WriteString(styles.TableSelected.Render("› " + row))
		} else {
			b.WriteString(styles.TableRow.Render("  " + row))
		}
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) renderSourcesTab(height int) string {
	styles := GetStyles()
	var b strings.Builder

	// Title and add URL input
	if m.validatingURL {
		// Show animated dots during validation
		dots := [3]string{".", ".", "."}
		for i := 0; i < 3; i++ {
			if i == m.validationDot {
				dots[i] = styles.Title.Render(".")
			} else {
				dots[i] = styles.Muted.Render(".")
			}
		}
		b.WriteString(styles.SearchPrompt.Render("Validating") + dots[0] + dots[1] + dots[2])
		b.WriteString("\n\n")
	} else if m.addingURL {
		prompt := styles.SearchPrompt.Render("Add URL: ")
		b.WriteString(prompt + m.urlInput.View())
		b.WriteString("\n\n")
	} else {
		b.WriteString(styles.PanelTitle.Render("Search Sources"))
		b.WriteString("  ")
		b.WriteString(styles.Muted.Render("[a]Add URL  [enter]Toggle  [x]Remove"))
		b.WriteString("\n\n")
	}

	if len(m.sources) == 0 {
		b.WriteString(styles.Muted.Render("No sources configured. Press 'a' to add one."))
		return b.String()
	}

	// Column widths
	statusWidth := 12
	nameWidth := m.width - statusWidth - 6 // 2=prefix, 4=spacing
	if nameWidth < 20 {
		nameWidth = 20
	}

	// Header with border style like other tables
	header := fmt.Sprintf("  %s %s",
		PadRight("SOURCE", nameWidth),
		PadLeft("STATUS", statusWidth))
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderForeground(lipgloss.Color(theme.CurrentPalette.Muted))
	b.WriteString(headerStyle.Render(header))
	b.WriteString("\n")

	// Rows
	visibleRows := height - 4
	if visibleRows < 1 {
		visibleRows = 1
	}

	startIdx := 0
	if m.srcCursor >= visibleRows {
		startIdx = m.srcCursor - visibleRows + 1
	}

	endIdx := startIdx + visibleRows
	if endIdx > len(m.sources) {
		endIdx = len(m.sources)
	}

	for i := startIdx; i < endIdx; i++ {
		src := m.sources[i]

		// Show warning indicator if source has issues
		name := src.Name
		if src.Warning != "" {
			name = "⚠ " + name
		}
		name = TruncateString(name, nameWidth-2)

		var status string
		var statusStyled string
		if !src.Enabled {
			status = "Disabled"
			statusStyled = styles.Muted.Render(PadLeft(status, statusWidth))
		} else if src.Warning != "" {
			status = "Warning"
			statusStyled = styles.HealthMed.Render(PadLeft(status, statusWidth))
		} else {
			status = "Enabled"
			statusStyled = styles.VPNConnected.Render(PadLeft(status, statusWidth))
		}

		// Build row: prefix + padded name + space + styled status
		namePadded := PadRight(name, nameWidth)
		if i == m.srcCursor {
			b.WriteString(styles.TableSelected.Render("› "+namePadded+" ") + statusStyled)
		} else {
			b.WriteString(styles.TableRow.Render("  "+namePadded+" ") + statusStyled)
		}
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) renderResults(height int) string {
	styles := GetStyles()

	if len(m.results) == 0 {
		return styles.Muted.Render("No results")
	}

	var b strings.Builder

	// Column widths - must match row widths exactly
	// Rows have 2-char prefix ("› " or "  "), so header needs it too
	colWidths := []int{0, 10, 6, 6, 6}                // nameWidth set below, others fixed
	nameWidth := m.width - 2 - 10 - 6 - 6 - 6 - 4 - 2 // 2=prefix, 4=spaces between cols, 2=margin
	if nameWidth < 20 {
		nameWidth = 20
	}
	colWidths[0] = nameWidth

	colNames := []string{"NAME", "SIZE", "SEED", "LEECH", "HEALTH"}

	// Build header with sort indicator - sorted column gets highlighted
	var headerParts []string
	for i, name := range colNames {
		w := colWidths[i]
		ind := " "
		if i == m.searchSortCol {
			if m.searchSortAsc {
				ind = "▲"
			} else {
				ind = "▼"
			}
		}
		// Build column text - indicator right after name, padded to full width
		var colText string
		if i == 0 {
			// NAME: left-align
			padding := w - len(name) - 1
			if padding < 0 {
				padding = 0
			}
			colText = name + ind + repeat(" ", padding)
		} else {
			// Others: right-align
			padding := w - len(name) - 1
			if padding < 0 {
				padding = 0
			}
			colText = repeat(" ", padding) + ind + name
		}
		// Apply style - sorted column highlighted, others muted
		if i == m.searchSortCol {
			headerParts = append(headerParts, styles.SortedHeader.Render(colText))
		} else {
			headerParts = append(headerParts, styles.Muted.Render(colText))
		}
	}
	// Add 2-char prefix to match row prefix ("› " or "  ")
	header := "  " + strings.Join(headerParts, styles.Muted.Render(" "))
	// Render with border only (no foreground color override)
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderForeground(lipgloss.Color(theme.CurrentPalette.Muted))
	b.WriteString(headerStyle.Render(header))
	b.WriteString("\n")

	// Results are already sorted in-place when sort changes

	// Calculate visible range
	visibleRows := height - 3
	if visibleRows < 1 {
		visibleRows = 1
	}

	startIdx := 0
	if m.cursor >= visibleRows {
		startIdx = m.cursor - visibleRows + 1
	}

	endIdx := startIdx + visibleRows
	if endIdx > len(m.results) {
		endIdx = len(m.results)
	}

	// Render rows
	for i := startIdx; i < endIdx; i++ {
		t := m.results[i]
		name := TruncateString(t.Name, nameWidth-2) // -2 for "› " prefix

		// Match header widths exactly
		row := fmt.Sprintf("%s %s %s %s %s",
			PadRight(name, nameWidth),
			PadLeft(t.Size, 10),
			PadLeft(fmt.Sprintf("%d", t.Seeders), 6),
			PadLeft(fmt.Sprintf("%d", t.Leechers), 6),
			HealthBar(t.Health(), 6))

		// Check if this item has been downloaded
		isDownloaded := m.downloaded[t.Name]

		if i == m.cursor {
			if isDownloaded {
				b.WriteString(styles.VPNConnected.Render("✓ ") + styles.TableSelected.Render(row))
			} else {
				b.WriteString(styles.TableSelected.Render("› " + row))
			}
		} else {
			if isDownloaded {
				b.WriteString(styles.VPNConnected.Render("✓ ") + styles.TableRow.Render(row))
			} else {
				b.WriteString(styles.TableRow.Render("  " + row))
			}
		}
		b.WriteString("\n")
	}

	// Files panel (if in details mode and files loaded)
	if m.mode == viewDetails && m.cursor < len(m.results) {
		t := m.results[m.cursor]
		if len(t.Files) > 0 {
			b.WriteString("\n")
			b.WriteString(styles.PanelTitle.Render(fmt.Sprintf("FILES (%d)", len(t.Files))))
			b.WriteString("\n")
			for i, f := range t.Files {
				if i >= 5 {
					b.WriteString(styles.Muted.Render(fmt.Sprintf("  ... and %d more", len(t.Files)-5)))
					break
				}
				line := fmt.Sprintf("  %s", f.Name)
				if f.Size != "" {
					line += fmt.Sprintf("  %s", f.Size)
				}
				b.WriteString(styles.Muted.Render(TruncateString(line, m.width-4)))
				b.WriteString("\n")
			}
		}
	}

	return b.String()
}

func (m Model) renderStatusBar() string {
	styles := GetStyles()

	// Connection indicators
	var vpnStr string
	if m.vpnStatus.Connected {
		vpnStr = styles.VPNConnected.Render("● VPN")
	} else {
		vpnStr = styles.VPNDisconnect.Render("○ VPN")
	}

	var qbitStr string
	if m.qbitOnline {
		qbitStr = styles.VPNConnected.Render("● qBit")
	} else {
		qbitStr = styles.VPNDisconnect.Render("○ qBit")
	}

	// Mode indicator
	var modeStr string
	if m.searchInput.Focused() {
		modeStr = styles.VPNConnected.Render("INPUT")
	} else {
		modeStr = styles.HealthMed.Render("CMD")
	}

	// Context-sensitive help (mode + tab aware)
	var help string
	if m.searchInput.Focused() {
		help = "[esc]CMD [ctrl+u]Clear [enter]Search"
	} else if m.addingURL {
		help = "[esc]Cancel [enter]Add"
	} else {
		switch m.activeTab {
		case tabDownloads:
			help = "[←→]Sort [s]Toggle [f]Follow [p]Pause [x]Remove [q]Quit"
		case tabCompleted:
			help = "[←→]Sort col [s]Toggle sort [m]Plex [x]Remove [q]Quit"
		case tabSources:
			help = "[a]Add [enter]Toggle [x]Remove [q]Quit"
		default:
			if m.mode == viewResults || m.mode == viewDetails {
				help = "[←→]Sort [s]Toggle [enter]Download [d]Details [c]Config [q]Quit"
			} else {
				help = "[/]Search [v]VPN [c]Config [q]Quit"
			}
		}
	}

	// Left side: mode + status message
	var leftPart string
	if m.statusMsg != "" {
		leftPart = modeStr + " │ " + m.statusMsg
	} else {
		leftPart = modeStr
	}

	// Right side: connection status
	rightLine1 := qbitStr + "  " + vpnStr

	// Line 2: context-sensitive shortcuts (right-justified)
	rightLine2 := styles.HelpKey.Render(help)

	// Build line 1
	leftWidth := lipgloss.Width(leftPart)
	rightWidth1 := lipgloss.Width(rightLine1)
	padding1 := m.width - leftWidth - rightWidth1 - 4
	if padding1 < 1 {
		padding1 = 1
	}
	line1 := leftPart + strings.Repeat(" ", padding1) + rightLine1

	// Build line 2 (right-justified)
	rightWidth2 := lipgloss.Width(rightLine2)
	padding2 := m.width - rightWidth2 - 2
	if padding2 < 0 {
		padding2 = 0
	}
	line2 := strings.Repeat(" ", padding2) + rightLine2

	return line1 + "\n" + line2
}
