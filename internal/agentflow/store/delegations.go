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

// Delegation is one effort (or task set) handed to a peer to run.
type Delegation struct {
	ID           string
	Peer         string
	Project      string
	Effort       int64
	TaskKeys     []string
	Reason       string
	Status       string
	RemoteEffort int64
	Created      time.Time
	Updated      time.Time
}

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

func (s *Store) InsertDelegation(ctx context.Context, d Delegation) error {
	if d.ID == "" {
		return fmt.Errorf("delegation id required")
	}
	if d.TaskKeys == nil {
		d.TaskKeys = []string{}
	}
	raw, _ := json.Marshal(d.TaskKeys)
	now := time.Now().UnixMilli()
	if d.Created.IsZero() {
		d.Created = time.UnixMilli(now)
	}
	if d.Updated.IsZero() {
		d.Updated = d.Created
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO delegations (id, peer, project, effort, task_keys, reason, status, remote_effort, created, updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.Peer, d.Project, d.Effort, string(raw), d.Reason, d.Status, d.RemoteEffort, d.Created.UnixMilli(), d.Updated.UnixMilli())
	return err
}

func (s *Store) GetDelegation(ctx context.Context, id string) (Delegation, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, peer, project, effort, task_keys, reason, status, remote_effort, created, updated FROM delegations WHERE id = ?`, id)
	var d Delegation
	var taskKeys string
	var created, updated int64
	if err := row.Scan(&d.ID, &d.Peer, &d.Project, &d.Effort, &taskKeys, &d.Reason, &d.Status, &d.RemoteEffort, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Delegation{}, ErrNotFound
		}
		return Delegation{}, err
	}
	d.loadTaskKeys(taskKeys)
	d.Created = time.UnixMilli(created)
	d.Updated = time.UnixMilli(updated)
	return d, nil
}

func (s *Store) ListDelegations(ctx context.Context, peer string) ([]Delegation, error) {
	q := `SELECT id, peer, project, effort, task_keys, reason, status, remote_effort, created, updated FROM delegations`
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
		var d Delegation
		var taskKeys string
		var created, updated int64
		if err := rows.Scan(&d.ID, &d.Peer, &d.Project, &d.Effort, &taskKeys, &d.Reason, &d.Status, &d.RemoteEffort, &created, &updated); err != nil {
			return nil, err
		}
		d.loadTaskKeys(taskKeys)
		d.Created = time.UnixMilli(created)
		d.Updated = time.UnixMilli(updated)
		out = append(out, d)
	}
	return out, rows.Err()
}

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
