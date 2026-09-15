package timetracker_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"cmd184psu/unified-webapp/internal/timetracker"
)

func newTempStore(t *testing.T) (*timetracker.Store, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "timetracker.json")
	s, err := timetracker.New(path)
	if err != nil {
		t.Fatalf("timetracker.New: %v", err)
	}
	return s, path
}

// ── New / load ───────────────────────────────────────────────────────────────

func TestNewMissingFileStartsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "timetracker.json")

	s, err := timetracker.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	snap := s.Snapshot()
	if snap.Customers == nil {
		t.Fatal("Snapshot().Customers is nil, want empty non-nil")
	}
	if len(snap.Customers) != 0 {
		t.Fatalf("want 0 customers, got %d", len(snap.Customers))
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file should not exist before the first mutation, stat err=%v", err)
	}

	if _, err := s.SetAuthor("Chris"); err != nil {
		t.Fatalf("SetAuthor: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file should exist after the first mutation: %v", err)
	}
}

func TestNewCorruptFileFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "timetracker.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	if _, err := timetracker.New(path); err == nil {
		t.Fatal("New should have failed on a corrupt data file")
	}
}

func TestNewLoadsExistingFile(t *testing.T) {
	s, path := newTempStore(t)
	if _, err := s.SetAuthor("Chris"); err != nil {
		t.Fatalf("SetAuthor: %v", err)
	}
	if _, err := s.AppendCustomer(timetracker.Customer{CustomerName: "Acme"}); err != nil {
		t.Fatalf("AppendCustomer: %v", err)
	}

	s2, err := timetracker.New(path)
	if err != nil {
		t.Fatalf("New (reload): %v", err)
	}
	raw := s2.Raw()
	if raw.Author != "Chris" {
		t.Errorf("Author: got %q, want %q", raw.Author, "Chris")
	}
	if len(raw.Customers) != 1 || raw.Customers[0].CustomerName != "Acme" {
		t.Errorf("Customers: got %v", raw.Customers)
	}
}

func TestPersistAcrossReopen(t *testing.T) {
	s, path := newTempStore(t)
	if _, err := s.AppendCustomer(timetracker.Customer{CustomerName: "Acme"}); err != nil {
		t.Fatalf("AppendCustomer: %v", err)
	}
	if _, err := s.UpdateField(0, "jira", "JIRA-1"); err != nil {
		t.Fatalf("UpdateField: %v", err)
	}

	s2, err := timetracker.New(path)
	if err != nil {
		t.Fatalf("New (reload): %v", err)
	}
	raw := s2.Raw()
	if len(raw.Customers) != 1 || raw.Customers[0].Jira != "JIRA-1" {
		t.Errorf("mutation did not persist across reopen: %v", raw.Customers)
	}
}

func TestLoadMigratesLegacyFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "timetracker.json")
	legacy := `{"companyName":"","projectName":"","author":"","version":"","customers":[
		{"customerName":"Acme","slackChannel":"","slackChannelId":"","insightUrl":"drop-me",
		 "workLoadType":"","sfdcUrl":"http://sfdc","supportTunnel":"x","cumulusBucket":"bucket-a","jira":""}]}`
	if err := os.WriteFile(path, []byte(legacy), 0644); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}

	s, err := timetracker.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	raw := s.Raw()
	if len(raw.Customers) != 1 || raw.Customers[0].CustomerName != "Acme" {
		t.Fatalf("legacy load failed: %v", raw.Customers)
	}
	if got := raw.Customers[0].CmsUrl; got != "http://sfdc" {
		t.Errorf("legacy sfdcUrl not migrated to cmsUrl: got %q", got)
	}
	if got := raw.Customers[0].SupportBucket; got != "bucket-a" {
		t.Errorf("legacy cumulusBucket not migrated to supportBucket: got %q", got)
	}

	if _, err := s.SetAuthor("Chris"); err != nil {
		t.Fatalf("SetAuthor: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	for _, key := range []string{"supportTunnel", "insightUrl", "sfdcUrl", "cumulusBucket"} {
		if strings.Contains(string(data), key) {
			t.Errorf("on-disk file still contains legacy key %s after save:\n%s", key, data)
		}
	}
}

// ── Snapshot ─────────────────────────────────────────────────────────────────

func TestSnapshotSortsCaseInsensitively(t *testing.T) {
	s, path := newTempStore(t)
	for _, name := range []string{"zeta", "Alpha", "beta"} {
		if _, err := s.AppendCustomer(timetracker.Customer{CustomerName: name}); err != nil {
			t.Fatalf("AppendCustomer(%q): %v", name, err)
		}
	}

	snap := s.Snapshot()
	if len(snap.Customers) != 3 {
		t.Fatalf("want 3 customers, got %d", len(snap.Customers))
	}
	got := []string{snap.Customers[0].CustomerName, snap.Customers[1].CustomerName, snap.Customers[2].CustomerName}
	want := []string{"Alpha", "beta", "zeta"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sorted order: got %v, want %v", got, want)
			break
		}
	}

	// On-disk file keeps insertion order after save.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	var onDisk timetracker.Data
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatalf("unmarshal on-disk file: %v", err)
	}
	insertion := []string{onDisk.Customers[0].CustomerName, onDisk.Customers[1].CustomerName, onDisk.Customers[2].CustomerName}
	wantInsertion := []string{"zeta", "Alpha", "beta"}
	for i := range wantInsertion {
		if insertion[i] != wantInsertion[i] {
			t.Errorf("on-disk order: got %v, want %v", insertion, wantInsertion)
			break
		}
	}
}

func TestSnapshotIsDeepCopy(t *testing.T) {
	s, _ := newTempStore(t)
	if _, err := s.AppendCustomer(timetracker.Customer{CustomerName: "Acme"}); err != nil {
		t.Fatalf("AppendCustomer: %v", err)
	}

	snap := s.Snapshot()
	snap.Customers[0].CustomerName = "Mutated"
	snap.Customers = append(snap.Customers, timetracker.Customer{CustomerName: "Ghost"})

	snap2 := s.Snapshot()
	if len(snap2.Customers) != 1 {
		t.Fatalf("mutating the first snapshot's slice affected a later Snapshot: %v", snap2.Customers)
	}
	if snap2.Customers[0].CustomerName != "Acme" {
		t.Errorf("mutating the first snapshot's struct affected a later Snapshot: %q", snap2.Customers[0].CustomerName)
	}
}

// ── SetAuthor ────────────────────────────────────────────────────────────────

func TestSetAuthor(t *testing.T) {
	s, path := newTempStore(t)
	if _, err := s.AppendCustomer(timetracker.Customer{CustomerName: "Acme"}); err != nil {
		t.Fatalf("AppendCustomer: %v", err)
	}
	d, err := s.SetAuthor("Chris")
	if err != nil {
		t.Fatalf("SetAuthor: %v", err)
	}
	if d.Author != "Chris" {
		t.Errorf("Author: got %q, want %q", d.Author, "Chris")
	}
	if len(d.Customers) != 1 || d.Customers[0].CustomerName != "Acme" {
		t.Errorf("SetAuthor touched Customers: %v", d.Customers)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	var onDisk timetracker.Data
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if onDisk.Author != "Chris" {
		t.Errorf("persisted Author: got %q, want %q", onDisk.Author, "Chris")
	}
}

// ── UpdateField ──────────────────────────────────────────────────────────────

func TestUpdateFieldEveryField(t *testing.T) {
	cases := []struct {
		field string
		get   func(c timetracker.Customer) string
	}{
		{"customerName", func(c timetracker.Customer) string { return c.CustomerName }},
		{"slackChannel", func(c timetracker.Customer) string { return c.SlackChannel }},
		{"workLoadType", func(c timetracker.Customer) string { return c.WorkLoadType }},
		{"cmsUrl", func(c timetracker.Customer) string { return c.CmsUrl }},
		{"supportBucket", func(c timetracker.Customer) string { return c.SupportBucket }},
		{"jira", func(c timetracker.Customer) string { return c.Jira }},
	}

	for _, tc := range cases {
		t.Run(tc.field, func(t *testing.T) {
			s, _ := newTempStore(t)
			if _, err := s.AppendCustomer(timetracker.Customer{}); err != nil {
				t.Fatalf("AppendCustomer: %v", err)
			}
			want := "value-for-" + tc.field
			d, err := s.UpdateField(0, tc.field, want)
			if err != nil {
				t.Fatalf("UpdateField(%q): %v", tc.field, err)
			}
			if got := tc.get(d.Customers[0]); got != want {
				t.Errorf("%s: got %q, want %q", tc.field, got, want)
			}
		})
	}
}

func TestUpdateFieldRejectsSlackChannelIdAndUnknown(t *testing.T) {
	for _, field := range []string{"slackChannelId", "supportTunnel", "bogus"} {
		t.Run(field, func(t *testing.T) {
			s, _ := newTempStore(t)
			if _, err := s.AppendCustomer(timetracker.Customer{}); err != nil {
				t.Fatalf("AppendCustomer: %v", err)
			}
			if _, err := s.UpdateField(0, field, "x"); !errors.Is(err, timetracker.ErrInvalidField) {
				t.Errorf("field %q: got %v, want ErrInvalidField", field, err)
			}
		})
	}
}

func TestUpdateFieldBounds(t *testing.T) {
	s, _ := newTempStore(t)
	if _, err := s.AppendCustomer(timetracker.Customer{CustomerName: "Acme"}); err != nil {
		t.Fatalf("AppendCustomer: %v", err)
	}
	for _, idx := range []int{-1, -5, 1, 11} {
		if _, err := s.UpdateField(idx, "jira", "x"); !errors.Is(err, timetracker.ErrInvalidIndex) {
			t.Errorf("index %d: got %v, want ErrInvalidIndex", idx, err)
		}
	}
	raw := s.Raw()
	if len(raw.Customers) != 1 || raw.Customers[0].Jira != "" {
		t.Errorf("store mutated despite invalid indices: %v", raw.Customers)
	}
}

// ── AppendCustomer / DeleteCustomer / ReplaceCustomers ──────────────────────

func TestAppendCustomer(t *testing.T) {
	s, path := newTempStore(t)
	if _, err := s.AppendCustomer(timetracker.Customer{CustomerName: "Acme"}); err != nil {
		t.Fatalf("AppendCustomer: %v", err)
	}
	d, err := s.AppendCustomer(timetracker.Customer{CustomerName: "Beta"})
	if err != nil {
		t.Fatalf("AppendCustomer: %v", err)
	}
	if len(d.Customers) != 2 || d.Customers[0].CustomerName != "Acme" || d.Customers[1].CustomerName != "Beta" {
		t.Fatalf("insertion order wrong: %v", d.Customers)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	var onDisk timetracker.Data
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(onDisk.Customers) != 2 || onDisk.Customers[1].CustomerName != "Beta" {
		t.Errorf("persisted customers: %v", onDisk.Customers)
	}
}

func TestDeleteCustomer(t *testing.T) {
	s, _ := newTempStore(t)
	for _, name := range []string{"A", "B", "C"} {
		if _, err := s.AppendCustomer(timetracker.Customer{CustomerName: name}); err != nil {
			t.Fatalf("AppendCustomer(%q): %v", name, err)
		}
	}
	d, err := s.DeleteCustomer(1)
	if err != nil {
		t.Fatalf("DeleteCustomer: %v", err)
	}
	if len(d.Customers) != 2 || d.Customers[0].CustomerName != "A" || d.Customers[1].CustomerName != "C" {
		t.Fatalf("wrong row removed: %v", d.Customers)
	}
}

func TestDeleteCustomerBounds(t *testing.T) {
	s, _ := newTempStore(t)
	if _, err := s.AppendCustomer(timetracker.Customer{CustomerName: "Acme"}); err != nil {
		t.Fatalf("AppendCustomer: %v", err)
	}
	if _, err := s.DeleteCustomer(-1); !errors.Is(err, timetracker.ErrInvalidIndex) {
		t.Errorf("index -1: got %v, want ErrInvalidIndex", err)
	}
	if _, err := s.DeleteCustomer(1); !errors.Is(err, timetracker.ErrInvalidIndex) {
		t.Errorf("index len: got %v, want ErrInvalidIndex", err)
	}
	raw := s.Raw()
	if len(raw.Customers) != 1 {
		t.Errorf("store mutated despite invalid indices: %v", raw.Customers)
	}
}

func TestReplaceCustomers(t *testing.T) {
	s, path := newTempStore(t)
	if _, err := s.AppendCustomer(timetracker.Customer{CustomerName: "Old"}); err != nil {
		t.Fatalf("AppendCustomer: %v", err)
	}
	replacement := []timetracker.Customer{
		{CustomerName: "New1"},
		{CustomerName: "New2"},
	}
	d, err := s.ReplaceCustomers(replacement)
	if err != nil {
		t.Fatalf("ReplaceCustomers: %v", err)
	}
	if len(d.Customers) != 2 || d.Customers[0].CustomerName != "New1" || d.Customers[1].CustomerName != "New2" {
		t.Fatalf("replacement wrong: %v", d.Customers)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	var onDisk timetracker.Data
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(onDisk.Customers) != 2 || onDisk.Customers[0].CustomerName != "New1" {
		t.Errorf("persisted replacement: %v", onDisk.Customers)
	}
}

// ── save() ───────────────────────────────────────────────────────────────────

func TestSaveIsAtomic(t *testing.T) {
	s, path := newTempStore(t)
	if _, err := s.SetAuthor("Chris"); err != nil {
		t.Fatalf("SetAuthor: %v", err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("tmp file should not exist after save, stat err=%v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	var d timetracker.Data
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatalf("file is not valid JSON: %v", err)
	}
}

func TestConcurrentReadersAndWriters(t *testing.T) {
	s, path := newTempStore(t)
	for i := 0; i < 3; i++ {
		if _, err := s.AppendCustomer(timetracker.Customer{CustomerName: "Seed"}); err != nil {
			t.Fatalf("seed AppendCustomer: %v", err)
		}
	}

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			switch i % 4 {
			case 0:
				s.Snapshot()
			case 1:
				raw := s.Raw()
				idx := i % (len(raw.Customers) + 1)
				if idx >= len(raw.Customers) {
					idx = 0
				}
				s.UpdateField(idx, "jira", "concurrent")
			case 2:
				s.AppendCustomer(timetracker.Customer{CustomerName: "New"})
			case 3:
				raw := s.Raw()
				if len(raw.Customers) > 0 {
					s.DeleteCustomer(0)
				}
			}
		}(i)
	}
	wg.Wait()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	var d timetracker.Data
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatalf("file is not valid JSON after concurrent access: %v", err)
	}
}
