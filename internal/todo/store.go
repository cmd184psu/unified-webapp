package todo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Store manages per-subject JSON file storage under a single data directory.
// Each subject is a subdirectory; each list is a .json file within it.
type Store struct {
	dataDir string
	mu      sync.Mutex
	locks   map[string]*sync.RWMutex
}

// NewStore creates a Store backed by dataDir, creating it if necessary.
func NewStore(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, err
	}
	return &Store{dataDir: dataDir, locks: make(map[string]*sync.RWMutex)}, nil
}

// subjectLock returns (lazy-creating) the per-subject RWMutex.
func (s *Store) subjectLock(subject string) *sync.RWMutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.locks[subject]; !ok {
		s.locks[subject] = &sync.RWMutex{}
	}
	return s.locks[subject]
}

// Subjects lists all subject directories and their .json entries.
// index.json is always prepended to each subject's entry list.
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
		lk := s.subjectLock(name)
		lk.RLock()
		files, err := os.ReadDir(filepath.Join(s.dataDir, name))
		lk.RUnlock()
		if err != nil {
			continue
		}
		entries := []string{name + "/index.json"}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") || f.Name() == "index.json" {
				continue
			}
			entries = append(entries, name+"/"+f.Name())
		}
		subjects = append(subjects, Subject{Age: 0, Timestamp: 0, Subject: name, Entries: entries})
	}
	return subjects, nil
}

// GenerateIndex returns a dynamically generated index for subject, listing all
// non-index .json files in that subject directory.
func (s *Store) GenerateIndex(subject string) ([]byte, error) {
	lk := s.subjectLock(subject)
	lk.RLock()
	files, err := os.ReadDir(filepath.Join(s.dataDir, subject))
	lk.RUnlock()
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	idx := IndexFile{
		Title: "Index of " + subject,
		List:  []IndexItem{},
	}
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") || f.Name() == "index.json" {
			continue
		}
		idx.List = append(idx.List, IndexItem{
			JSON: subject + "/" + f.Name(),
			Name: f.Name(),
			Skip: false,
		})
	}
	return json.Marshal(idx)
}

// emptyList is the initial content written to newly created list files.
var emptyList = []byte(`{"title":"untitled","list":[]}`)

// ReadFile returns the contents of {dataDir}/{subject}/{item}.
// If the file does not exist it is created with empty list content first.
func (s *Store) ReadFile(subject, item string) ([]byte, error) {
	path := filepath.Join(s.dataDir, subject, item)
	lk := s.subjectLock(subject)

	lk.RLock()
	data, err := os.ReadFile(path)
	lk.RUnlock()
	if err == nil {
		return data, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}

	// File absent — create it under write lock (double-check after acquiring).
	lk.Lock()
	defer lk.Unlock()
	data, err = os.ReadFile(path)
	if err == nil {
		return data, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, emptyList, 0644); err != nil {
		return nil, err
	}
	return emptyList, nil
}

// WriteFile atomically saves data to {dataDir}/{subject}/{item}.
func (s *Store) WriteFile(subject, item string, data []byte) error {
	path := filepath.Join(s.dataDir, subject, item)
	lk := s.subjectLock(subject)
	lk.Lock()
	defer lk.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// MoveFile moves {dataDir}/{subject}/{item} to {dataDir}/{newSubject}/{item}.
// Locks are acquired in sorted order to prevent deadlock.
func (s *Store) MoveFile(subject, item, newSubject string) error {
	if subject == newSubject {
		return nil
	}
	// Determine lock acquisition order.
	first, second := subject, newSubject
	if first > second {
		first, second = second, first
	}
	la := s.subjectLock(first)
	lb := s.subjectLock(second)
	la.Lock()
	defer la.Unlock()
	lb.Lock()
	defer lb.Unlock()

	src := filepath.Join(s.dataDir, subject, item)
	dst := filepath.Join(s.dataDir, newSubject, item)
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	return os.Rename(src, dst)
}
