package timetracker

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Store is a thread-safe, JSON-backed timetracker data store.
type Store struct {
	mu       sync.RWMutex
	data     Data
	filePath string
}

// New creates (or loads) a Store backed by filePath. A missing file starts
// empty-but-valid (§3.5 fresh boot) without failing; a present-but-invalid
// JSON file returns an error. Unknown JSON fields (from the removed legacy
// remote-access feature) are silently ignored by decoding into the current
// struct shapes.
func New(filePath string) (*Store, error) {
	s := &Store{filePath: filePath, data: Data{Customers: []Customer{}}}
	raw, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, fmt.Errorf("loading timetracker store: %w", err)
	}
	var d Data
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("loading timetracker store: %w", err)
	}
	if d.Customers == nil {
		d.Customers = []Customer{}
	}
	s.data = d
	return s, nil
}

// Snapshot returns a deep copy of the data with customers sorted
// case-insensitively by CustomerName. Used by GET /data and CSV export. The
// stored insertion order is never mutated by this call.
func (s *Store) Snapshot() Data {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d := deepCopyData(s.data)
	sort.SliceStable(d.Customers, func(i, j int) bool {
		return strings.ToLower(d.Customers[i].CustomerName) < strings.ToLower(d.Customers[j].CustomerName)
	})
	return d
}

// Raw returns a deep copy of the data in file (insertion) order. Used as the
// mutation-response body.
func (s *Store) Raw() Data {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return deepCopyData(s.data)
}

// SetAuthor updates the author field and persists it.
func (s *Store) SetAuthor(value string) (Data, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Author = value
	return deepCopyData(s.data), s.save()
}

// UpdateField updates a single field of the customer at index and persists
// it. Unknown fields (including slackChannelId and the removed legacy
// remote-access field) return ErrInvalidField; an out-of-range index returns
// ErrInvalidIndex (FR-F2). Neither error mutates the store.
func (s *Store) UpdateField(index int, field, value string) (Data, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if index < 0 || index >= len(s.data.Customers) {
		return Data{}, ErrInvalidIndex
	}
	c := &s.data.Customers[index]
	switch field {
	case "customerName":
		c.CustomerName = value
	case "slackChannel":
		c.SlackChannel = value
	case "insightUrl":
		c.InsightUrl = value
	case "workLoadType":
		c.WorkLoadType = value
	case "sfdcUrl":
		c.SfdcUrl = value
	case "cumulusBucket":
		c.CumulusBucket = value
	case "jira":
		c.Jira = value
	default:
		return Data{}, ErrInvalidField
	}
	return deepCopyData(s.data), s.save()
}

// AppendCustomer appends a customer to the end of the list and persists it.
func (s *Store) AppendCustomer(c Customer) (Data, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Customers = append(s.data.Customers, c)
	return deepCopyData(s.data), s.save()
}

// DeleteCustomer removes the customer at index and persists it. An
// out-of-range index returns ErrInvalidIndex without mutating the store.
func (s *Store) DeleteCustomer(index int) (Data, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if index < 0 || index >= len(s.data.Customers) {
		return Data{}, ErrInvalidIndex
	}
	s.data.Customers = append(s.data.Customers[:index], s.data.Customers[index+1:]...)
	return deepCopyData(s.data), s.save()
}

// ReplaceCustomers replaces the entire customer list and persists it. Used
// by CSV import after full validation (FR-F4).
func (s *Store) ReplaceCustomers(customers []Customer) (Data, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]Customer, len(customers))
	copy(cp, customers)
	s.data.Customers = cp
	return deepCopyData(s.data), s.save()
}

// save writes the store to filePath atomically via a temp file plus rename
// (FR-F3). Must be called with the lock held. File contents keep insertion
// order; they are never sorted on disk.
func (s *Store) save() error {
	if err := os.MkdirAll(filepath.Dir(s.filePath), 0750); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.filePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.filePath)
}

// deepCopyData returns a copy of d whose Customers slice is independent of
// d's. Customer is a value type (all string fields), so copying the slice
// elements is sufficient for a full deep copy.
func deepCopyData(d Data) Data {
	customers := make([]Customer, len(d.Customers))
	copy(customers, d.Customers)
	d.Customers = customers
	return d
}
