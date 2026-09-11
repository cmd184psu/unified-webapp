package obsidianoid

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"cmd184psu/unified-webapp/internal/platform/broker"
	"github.com/fsnotify/fsnotify"
)

// MakeTestBrokers creates n brokers without starting any file watchers.
// Intended for use in tests where file watching is not needed.
func MakeTestBrokers(n int) []*broker.Broker {
	brokers := make([]*broker.Broker, n)
	for i := range brokers {
		brokers[i] = broker.NewBroker(0)
	}
	return brokers
}

// StartVaultWatcher is exported for tests.
func StartVaultWatcher(vaultPath string, b *broker.Broker) (io.Closer, error) {
	return startVaultWatcher(vaultPath, b)
}

// startVaultWatcher watches every directory under vaultPath for .md file changes
// and publishes note-changed events to b. Returns the watcher for cleanup and
// any startup error.
func startVaultWatcher(vaultPath string, b *broker.Broker) (io.Closer, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	// addDirs registers a directory and all its non-hidden subdirectories.
	var addDirs func(root string)
	addDirs = func(root string) {
		filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return nil
			}
			if strings.HasPrefix(d.Name(), ".") && path != root {
				return filepath.SkipDir
			}
			_ = watcher.Add(path)
			return nil
		})
	}
	addDirs(vaultPath)

	go func() {
		defer watcher.Close()
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Has(fsnotify.Create) {
					if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
						addDirs(event.Name)
					}
				}
				if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
					if strings.ToLower(filepath.Ext(event.Name)) == ".md" {
						rel, err := filepath.Rel(vaultPath, event.Name)
						if err == nil {
							b.Publish(fmt.Sprintf(`{"path":%q}`, filepath.ToSlash(rel)))
						}
					}
				}
			case _, ok := <-watcher.Errors:
				if !ok {
					return
				}
			}
		}
	}()

	return watcher, nil
}
