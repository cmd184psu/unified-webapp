package store

import (
	"context"
	"database/sql"
)

// Snapshot is a read-only, internally consistent copy (callers must not
// modify its slices or maps) the configuration
// (settings, backends, projects and role candidates) read in one transaction
// (SV2). The dispatcher takes one per pass and never sees a half-applied
// change.
type Snapshot struct {
	Settings   Settings
	Backends   []Backend
	Projects   []Project
	Candidates []Candidate
	// Effective is each project's settings with its overrides applied, by slug.
	Effective map[string]Settings
}

// Snapshot reads the whole configuration in a single read transaction.
func (s *Store) Snapshot(ctx context.Context) (Snapshot, error) {
	var snap Snapshot
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return snap, err
	}
	defer tx.Rollback()
	if snap.Settings, err = loadSettings(ctx, tx); err != nil {
		return Snapshot{}, err
	}
	if snap.Backends, err = loadBackends(ctx, tx, ""); err != nil {
		return Snapshot{}, err
	}
	if snap.Projects, err = loadProjects(ctx, tx, ""); err != nil {
		return Snapshot{}, err
	}
	if snap.Candidates, err = loadCandidates(ctx, tx); err != nil {
		return Snapshot{}, err
	}
	snap.Effective = make(map[string]Settings, len(snap.Projects))
	for _, p := range snap.Projects {
		if snap.Effective[p.Slug], err = loadEffective(ctx, tx, p.Slug); err != nil {
			return Snapshot{}, err
		}
	}
	return snap, nil
}

// CandidatesFor returns the ordered candidates for role as seen by project
// slug ("" for global): the project's own list when it has one, otherwise the
// global list. Entries on retired backends are dropped. Only explicitly
// listed models appear, so a paid model is never an implicit candidate (MB5).
func (sn Snapshot) CandidatesFor(role, slug string) []Candidate {
	retired := map[string]bool{}
	for _, b := range sn.Backends {
		retired[b.Name] = b.Retired
	}
	pick := func(scope, project string) (live []Candidate, found bool) {
		for _, c := range sn.Candidates {
			if c.Role == role && c.Scope == scope && c.ProjectID == project {
				found = true
				if !retired[c.Backend] {
					live = append(live, c)
				}
			}
		}
		return live, found
	}
	if slug != "" {
		// A project list that exists but is all retired yields nothing; it
		// must not silently fall back to the global list.
		if live, found := pick("project", slug); found {
			return live
		}
	}
	live, _ := pick("global", "")
	return live
}
