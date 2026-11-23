// Copyright 2026 FreeSociety3 contributors
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Delegation statuses. An outbound row moves pending -> running -> done,
// cancelled or fallback; an inbound row (the peer's shadow copy) moves
// running -> done or cancelled.
const (
	DelegationPending   = "pending"
	DelegationRunning   = "running"
	DelegationDone      = "done"
	DelegationCancelled = "cancelled"
	DelegationFallback  = "fallback"
)

// Delegation is one effort (or task set) handed to a peer to run. The same
// row exists on both nodes with the same ID: on the owner side Direction is
// "out" and Effort is the source effort, while on the peer side Direction is
// "in", SourceEffort is the owner's effort and Effort is the shadow effort.
type Delegation struct {
	ID           string
	Peer         string
	Project      string
	Effort       int64
	TaskKeys     []string
	Reason       string
	Status       string
	Direction    string
	SourceEffort int64
	RemoteEffort int64
	BaseCommit   string
	Detail       string
	LastContact  time.Time
	Created      time.Time
	Updated      time.Time
}

const delegationCols = `id, peer, project, effort, task_keys, reason, status, direction, source_effort, remote_effort, base_commit, detail, last_contact, created, updated`

func (d *Delegation) loadTaskKeys(raw string) {
	if raw == "" {
		d.TaskKeys = []string{}
		return
	}
	_ = json.Unmarshal([]byte(raw), &d.TaskKeys)
	if d.TaskKeys == nil {
		d.TaskKeys = []string{}
	}
}

func scanDelegation(r interface{ Scan(...any) error }) (Delegation, error) {
	var d Delegation
	var taskKeys string
	var lastContact, created, updated int64
	if err := r.Scan(&d.ID, &d.Peer, &d.Project, &d.Effort, &taskKeys, &d.Reason, &d.Status, &d.Direction, &d.SourceEffort, &d.RemoteEffort, &d.BaseCommit, &d.Detail, &lastContact, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Delegation{}, ErrNotFound
		}
		return Delegation{}, err
	}
	d.loadTaskKeys(taskKeys)
	if lastContact > 0 {
		d.LastContact = time.UnixMilli(lastContact)
	}
	d.Created = time.UnixMilli(created)
	d.Updated = time.UnixMilli(updated)
	return d, nil
}

// InsertDelegation records a new delegation. The ID is assigned by the owner
// node and reused verbatim on the peer, so a repeated delivery is rejected by
// the primary key instead of creating a second shadow effort.
func (s *Store) InsertDelegation(ctx context.Context, d Delegation) error {
	if d.ID == "" {
		return fmt.Errorf("delegation id required")
	}
	if d.TaskKeys == nil {
		d.TaskKeys = []string{}
	}
	if d.Status == "" {
		d.Status = DelegationPending
	}
	if d.Direction == "" {
		d.Direction = "out"
	}
	raw, _ := json.Marshal(d.TaskKeys)
	now := time.Now().UnixMilli()
	if d.Created.IsZero() {
		d.Created = time.UnixMilli(now)
	}
	if d.Updated.IsZero() {
		d.Updated = d.Created
	}
	var lastContact int64
	if !d.LastContact.IsZero() {
		lastContact = d.LastContact.UnixMilli()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO delegations (id, peer, project, effort, task_keys, reason, status, direction, source_effort, remote_effort, base_commit, detail, last_contact, created, updated)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.Peer, d.Project, d.Effort, string(raw), d.Reason, d.Status, d.Direction, d.SourceEffort, d.RemoteEffort, d.BaseCommit, d.Detail, lastContact, d.Created.UnixMilli(), d.Updated.UnixMilli())
	return err
}

func (s *Store) GetDelegation(ctx context.Context, id string) (Delegation, error) {
	return scanDelegation(s.db.QueryRowContext(ctx, `SELECT `+delegationCols+` FROM delegations WHERE id = ?`, id))
}

// ListDelegations lists the rows for one peer, or every row when peer is
// empty, newest first.
func (s *Store) ListDelegations(ctx context.Context, peer string) ([]Delegation, error) {
	q := `SELECT ` + delegationCols + ` FROM delegations`
	var args []any
	if peer != "" {
		q += ` WHERE peer = ?`
		args = append(args, peer)
	}
	q += ` ORDER BY created DESC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Delegation
	for rows.Next() {
		d, err := scanDelegation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// UpdateDelegation sets a row's status and, when non-zero, its remote effort.
func (s *Store) UpdateDelegation(ctx context.Context, id, status string, remoteEffort int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE delegations SET status = ?, remote_effort = CASE WHEN ? > 0 THEN ? ELSE remote_effort END, updated = ? WHERE id = ?`,
		status, remoteEffort, remoteEffort, time.Now().UnixMilli(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// TouchDelegation records a successful status poll of an outbound row.
func (s *Store) TouchDelegation(ctx context.Context, id string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE delegations SET last_contact = ? WHERE id = ?`, at.UnixMilli(), id)
	return err
}

// SetDelegationDetail stores the last note or error shown with a delegation.
func (s *Store) SetDelegationDetail(ctx context.Context, id, detail string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE delegations SET detail = ?, updated = ? WHERE id = ?`, detail, time.Now().UnixMilli(), id)
	return err
}

// SetEffortDelegatedFrom marks an effort as the shadow copy of one the named
// node owns, so the local UI and API say where it came from.
func (s *Store) SetEffortDelegatedFrom(ctx context.Context, effort int64, node string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE efforts SET delegated_from = ? WHERE id = ?`, node, effort)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// EffortDelegatedFrom reports the node an effort was delegated from; empty for
// an effort this node owns.
func (s *Store) EffortDelegatedFrom(ctx context.Context, effort int64) (string, error) {
	var node string
	err := s.db.QueryRowContext(ctx, `SELECT delegated_from FROM efforts WHERE id = ?`, effort).Scan(&node)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return node, err
}
