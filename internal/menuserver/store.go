package menuserver

import (
	"os"
	"path/filepath"
	"strings"
)

// Subject is the response shape for one entry in GET /items.
type Subject struct {
	Age       int      `json:"age"`
	Timestamp int      `json:"timestamp"`
	Subject   string   `json:"subject"`
	Entries   []string `json:"entries"`
}

// Store scans dataDir for subject subdirectories containing JSON menu files.
type Store struct {
	dataDir string
}

// NewStore creates a Store backed by dataDir, creating it if necessary.
func NewStore(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, err
	}
	return &Store{dataDir: dataDir}, nil
}

// Subjects lists all subject directories and their .json entries.
// Entries are formatted as "{subject}/{filename}" to match the JS URL pattern
// used in "menus/"+entry.
func (s *Store) Subjects() ([]Subject, error) {
	dirs, err := os.ReadDir(s.dataDir)
	if err != nil {
		return nil, err
	}
	var subjects []Subject
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		name := d.Name()
		files, err := os.ReadDir(filepath.Join(s.dataDir, name))
		if err != nil {
			continue
		}
		var entries []string
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
				continue
			}
			entries = append(entries, name+"/"+f.Name())
		}
		if len(entries) == 0 {
			continue
		}
		subjects = append(subjects, Subject{
			Age:       0,
			Timestamp: 0,
			Subject:   name,
			Entries:   entries,
		})
	}
	return subjects, nil
}

// ReadMenu returns the raw JSON content of {dataDir}/{subject}/{item}.
// It returns an error if the path escapes dataDir.
func (s *Store) ReadMenu(subject, item string) ([]byte, error) {
	abs := filepath.Join(s.dataDir, subject, item)
	rel, err := filepath.Rel(s.dataDir, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return nil, os.ErrInvalid
	}
	return os.ReadFile(abs)
}
