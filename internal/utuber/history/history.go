package history

import (
	"encoding/json"
	"os"
	"sync"
)

type Entry struct {
	URL        string `json:"url"`
	OutputFile string `json:"output_file"`
	ShowName   string `json:"show_name"`
	Mode       string `json:"mode"`
}

type Log struct {
	path string
	mu   sync.Mutex
	byURL map[string]*Entry
}

func Open(path string) (*Log, error) {
	l := &Log{path: path, byURL: make(map[string]*Entry)}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return l, nil
		}
		return nil, err
	}

	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	for i := range entries {
		l.byURL[entries[i].URL] = &entries[i]
	}
	return l, nil
}

// Lookup returns the prior entry for a URL, or nil if unseen.
func (l *Log) Lookup(url string) *Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.byURL[url]
}

// Record saves a completed download to the history file.
func (l *Log) Record(e Entry) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.byURL[e.URL] = &e

	entries := make([]Entry, 0, len(l.byURL))
	for _, v := range l.byURL {
		entries = append(entries, *v)
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(l.path, data, 0644)
}
