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
	// Legacy pshelper data files use sfdcUrl and cumulusBucket for what are
	// now cmsUrl and supportBucket; map them so the values survive the first
	// save under the new keys. (insightUrl and the removed remote-access
	// field remain silently dropped.) New keys win when both are present.
	var legacy struct {
		Customers []struct {
			SfdcUrl       string `json:"sfdcUrl"`
			CumulusBucket string `json:"cumulusBucket"`
		} `json:"customers"`
	}
	// The legacy decode can fail partway (e.g. a non-string legacy value)
	// even though the lenient decode above succeeded, so bound the loop by
	// what it actually produced.
	_ = json.Unmarshal(raw, &legacy)
	for i := range min(len(d.Customers), len(legacy.Customers)) {
		if d.Customers[i].CmsUrl == "" {
			d.Customers[i].CmsUrl = legacy.Customers[i].SfdcUrl
		}
		if d.Customers[i].SupportBucket == "" {
			d.Customers[i].SupportBucket = legacy.Customers[i].CumulusBucket
		}
	}
	sortCustomers(d.Customers)
	s.data = d
	return s, nil
}

// sortCustomers sorts customers case-insensitively by CustomerName. The store
// keeps this order canonical everywhere — in memory, on disk, in GET /data,
// and in mutation responses — so the indexes the frontend derives from
// GET /data address the same customers that /update and /delete mutate.
func sortCustomers(cs []Customer) {
	sort.SliceStable(cs, func(i, j int) bool {
		return strings.ToLower(cs[i].CustomerName) < strings.ToLower(cs[j].CustomerName)
	})
}

// Snapshot returns a deep copy of the data. Customers are already in the
// canonical sorted order (see sortCustomers).
func (s *Store) Snapshot() Data {
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

// UpdateField updates a single field of the customer at index (in the
// canonical sorted order) and persists it. Unknown fields (including slackChannelId and the removed legacy
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
	case "workLoadType":
		c.WorkLoadType = value
	case "cmsUrl":
		c.CmsUrl = value
	case "supportBucket":
		c.SupportBucket = value
	case "jira":
		c.Jira = value
	default:
		return Data{}, ErrInvalidField
	}
	// A customerName change can move the customer's sorted position, so
	// restore the canonical order before persisting and responding.
	sortCustomers(s.data.Customers)
	return deepCopyData(s.data), s.save()
}

// AppendCustomer adds a customer, restores the canonical sorted order, and
// persists it.
func (s *Store) AppendCustomer(c Customer) (Data, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Customers = append(s.data.Customers, c)
	sortCustomers(s.data.Customers)
	return deepCopyData(s.data), s.save()
}

// DeleteCustomer removes the customer at index (in the canonical sorted
// order) and persists it. An out-of-range index returns ErrInvalidIndex
// without mutating the store. Removal preserves the sorted order.
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
	sortCustomers(cp)
	s.data.Customers = cp
	return deepCopyData(s.data), s.save()
}

// save writes the store to filePath atomically via a temp file plus rename
// (FR-F3). Must be called with the lock held. File contents use the same
// canonical sorted order as everything else.
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
