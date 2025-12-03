package theme

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher monitors theme config files and triggers refresh on changes
type Watcher struct {
	watcher  *fsnotify.Watcher
	debounce *time.Timer
	mu       sync.Mutex
	onChange func()
	done     chan struct{}
}

// NewWatcher creates a file watcher for theme configs
func NewWatcher(onChange func()) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	w := &Watcher{
		watcher:  fsw,
		onChange: onChange,
		done:     make(chan struct{}),
	}

	// Add watch paths
	home, _ := os.UserHomeDir()
	if home != "" {
		paths := []string{
			filepath.Join(home, ".config", "omarchy", "current", "theme"),
			filepath.Join(home, ".config", "alacritty"),
			filepath.Join(home, ".config", "kitty"),
			filepath.Join(home, ".config", "foot"),
		}

		for _, p := range paths {
			// Watch directory if exists, ignore errors for missing dirs
			if _, err := os.Stat(p); err == nil {
				_ = fsw.Add(p)
			}
		}
	}

	go w.run()

	return w, nil
}

func (w *Watcher) run() {
	for {
		select {
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			// Only care about writes and creates
			if event.Op&(fsnotify.Write|fsnotify.Create) != 0 {
				w.scheduleRefresh()
			}

		case <-w.watcher.Errors:
			// Ignore errors, keep watching

		case <-w.done:
			return
		}
	}
}

// scheduleRefresh debounces rapid file changes (150ms like OmNote)
func (w *Watcher) scheduleRefresh() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.debounce != nil {
		w.debounce.Stop()
	}

	w.debounce = time.AfterFunc(150*time.Millisecond, func() {
		Refresh()
		if w.onChange != nil {
			w.onChange()
		}
	})
}

// Stop closes the watcher
func (w *Watcher) Stop() {
	close(w.done)
	w.watcher.Close()

	w.mu.Lock()
	if w.debounce != nil {
		w.debounce.Stop()
	}
	w.mu.Unlock()
}
