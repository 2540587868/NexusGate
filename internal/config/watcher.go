package config

import (
	"context"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

type ConfigChangeCallback func(oldCfg, newCfg *Config)

type Watcher interface {
	Start(ctx context.Context) error
	Stop()
	OnChange(callback ConfigChangeCallback)
}

type FileWatcher struct {
	store     *Store
	interval  time.Duration
	cancel    context.CancelFunc
	mu        sync.Mutex
	callbacks []ConfigChangeCallback
	running   bool
}

func NewFileWatcher(store *Store, interval time.Duration) *FileWatcher {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return &FileWatcher{
		store:    store,
		interval: interval,
	}
}

func (fw *FileWatcher) Start(ctx context.Context) error {
	fw.mu.Lock()
	if fw.running {
		fw.mu.Unlock()
		return nil
	}

	watchCtx, cancel := context.WithCancel(ctx)
	fw.cancel = cancel
	fw.running = true
	fw.mu.Unlock()

	go fw.watchLoop(watchCtx)

	slog.Info("config file watcher started", "file", fw.store.path)
	return nil
}

func (fw *FileWatcher) Stop() {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	if fw.cancel != nil {
		fw.cancel()
		fw.cancel = nil
	}
	fw.running = false
}

func (fw *FileWatcher) OnChange(callback ConfigChangeCallback) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	fw.callbacks = append(fw.callbacks, callback)
}

func (fw *FileWatcher) watchLoop(ctx context.Context) {
	fwWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Error("failed to create fsnotify watcher, falling back to polling", "error", err)
		fw.pollLoop(ctx)
		return
	}
	defer fwWatcher.Close()

	configDir := filepath.Dir(fw.store.path)
	if err := fwWatcher.Add(configDir); err != nil {
		slog.Error("failed to watch config directory, falling back to polling", "error", err, "dir", configDir)
		fw.pollLoop(ctx)
		return
	}

	var debounceTimer *time.Timer
	debounceDelay := 100 * time.Millisecond

	for {
		select {
		case <-ctx.Done():
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			slog.Info("config file watcher stopped")
			fw.mu.Lock()
			fw.running = false
			fw.mu.Unlock()
			return
		case event, ok := <-fwWatcher.Events:
			if !ok {
				return
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Rename) {
				if filepath.Base(event.Name) != filepath.Base(fw.store.path) {
					continue
				}
				if debounceTimer != nil {
					debounceTimer.Stop()
				}
				debounceTimer = time.AfterFunc(debounceDelay, func() {
					fw.reloadConfig()
				})
			}
		case watchErr, ok := <-fwWatcher.Errors:
			if !ok {
				return
			}
			slog.Error("fsnotify watcher error", "error", watchErr)
		}
	}
}

func (fw *FileWatcher) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(fw.interval)
	defer ticker.Stop()

	var lastModTime time.Time

	for {
		select {
		case <-ctx.Done():
			slog.Info("config file watcher stopped (polling)")
			fw.mu.Lock()
			fw.running = false
			fw.mu.Unlock()
			return
		case <-ticker.C:
			modTime, changed := fw.checkFileChange(lastModTime)
			if changed {
				fw.reloadConfig()
				lastModTime = modTime
			} else if !modTime.IsZero() {
				lastModTime = modTime
			}
		}
	}
}

func (fw *FileWatcher) reloadConfig() {
	oldCfg := fw.store.Get()
	if err := fw.store.Load(); err != nil {
		slog.Error("failed to reload config", "error", err)
		return
	}
	newCfg := fw.store.Get()
	fw.notifyCallbacks(oldCfg, newCfg)
}

func (fw *FileWatcher) checkFileChange(lastModTime time.Time) (time.Time, bool) {
	info, err := fw.store.fileInfo()
	if err != nil {
		return time.Time{}, false
	}

	if lastModTime.IsZero() {
		return info.ModTime(), false
	}

	return info.ModTime(), info.ModTime().After(lastModTime)
}

func (fw *FileWatcher) notifyCallbacks(oldCfg, newCfg *Config) {
	fw.mu.Lock()
	callbacks := make([]ConfigChangeCallback, len(fw.callbacks))
	copy(callbacks, fw.callbacks)
	fw.mu.Unlock()

	for _, cb := range callbacks {
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("config change callback panicked", "error", r)
				}
			}()
			cb(oldCfg, newCfg)
		}()
	}
}
