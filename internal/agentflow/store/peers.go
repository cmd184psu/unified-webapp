// Copyright 2026 FreeSociety3 contributors
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type Peer struct {
	Name               string
	BaseURL            string
	HostHeader         string
	Enabled            bool
	Protocol           int
	RemoteBuild        string
	RemoteSchema       int
	PairedAt           int64
	LastSeen           int64
	LastOK             int64
	LastError          string
	InsecureSkipVerify bool
	Delegable          []string
}

func (p *Peer) loadDelegable(raw string) {
	if raw == "" {
		p.Delegable = []string{}
		return
	}
	_ = json.Unmarshal([]byte(raw), &p.Delegable)
	if p.Delegable == nil {
		p.Delegable = []string{}
	}
}

func (s *Store) ListPeers(ctx context.Context) ([]Peer, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, base_url, host_header, enabled, protocol, remote_build, remote_schema, paired_at, last_seen, last_ok, last_error, insecure_skip_verify, delegable_json FROM peers ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Peer
	for rows.Next() {
		var p Peer
		var enabled, insecure int
		var delegable string
		if err := rows.Scan(&p.Name, &p.BaseURL, &p.HostHeader, &enabled, &p.Protocol, &p.RemoteBuild, &p.RemoteSchema, &p.PairedAt, &p.LastSeen, &p.LastOK, &p.LastError, &insecure, &delegable); err != nil {
			return nil, err
		}
		p.Enabled = enabled == 1
		p.InsecureSkipVerify = insecure == 1
		p.loadDelegable(delegable)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) GetPeer(ctx context.Context, name string) (Peer, error) {
	row := s.db.QueryRowContext(ctx, `SELECT name, base_url, host_header, enabled, protocol, remote_build, remote_schema, paired_at, last_seen, last_ok, last_error, insecure_skip_verify, delegable_json FROM peers WHERE name = ?`, name)
	var p Peer
	var enabled, insecure int
	var delegable string
	if err := row.Scan(&p.Name, &p.BaseURL, &p.HostHeader, &enabled, &p.Protocol, &p.RemoteBuild, &p.RemoteSchema, &p.PairedAt, &p.LastSeen, &p.LastOK, &p.LastError, &insecure, &delegable); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Peer{}, ErrNotFound
		}
		return Peer{}, err
	}
	p.Enabled = enabled == 1
	p.InsecureSkipVerify = insecure == 1
	p.loadDelegable(delegable)
	return p, nil
}

func (s *Store) UpsertPeer(ctx context.Context, p Peer) error {
	delegable, _ := json.Marshal(p.Delegable)
	if p.Delegable == nil {
		delegable = []byte("[]")
	}
	enabled := 0
	if p.Enabled {
		enabled = 1
	}
	insecure := 0
	if p.InsecureSkipVerify {
		insecure = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO peers (name, base_url, host_header, enabled, protocol, remote_build, remote_schema, paired_at, last_seen, last_ok, last_error, insecure_skip_verify, delegable_json)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(name) DO UPDATE SET base_url=excluded.base_url, host_header=excluded.host_header, enabled=excluded.enabled, protocol=excluded.protocol, remote_build=excluded.remote_build, remote_schema=excluded.remote_schema, paired_at=excluded.paired_at, last_seen=excluded.last_seen, last_ok=excluded.last_ok, last_error=excluded.last_error, insecure_skip_verify=excluded.insecure_skip_verify, delegable_json=excluded.delegable_json`,
		p.Name, p.BaseURL, p.HostHeader, enabled, p.Protocol, p.RemoteBuild, p.RemoteSchema, p.PairedAt, p.LastSeen, p.LastOK, p.LastError, insecure, string(delegable))
	return err
}

func (s *Store) DeletePeer(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM peers WHERE name = ?`, name)
	return err
}

func (s *Store) UpdatePeerStatus(ctx context.Context, name string, lastSeen, lastOK int64, lastError, remoteBuild string, remoteSchema, protocol int, delegable []string) error {
	delegableB, _ := json.Marshal(delegable)
	if delegable == nil {
		delegableB = []byte("[]")
	}
	_, err := s.db.ExecContext(ctx, `UPDATE peers SET last_seen=?, last_ok=?, last_error=?, remote_build=?, remote_schema=?, protocol=?, delegable_json=? WHERE name=?`,
		lastSeen, lastOK, lastError, remoteBuild, remoteSchema, protocol, string(delegableB), name)
	return err
}

// pairing codes
func (s *Store) CreatePairingCode(ctx context.Context, name, codeHash string, expires int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO peer_pairing_codes (code_hash, name, expires, used, created) VALUES (?,?,?,0,?)`, codeHash, name, expires, time.Now().Unix())
	return err
}

// CheckPairingCode validates a code without burning it, so a caller can check
// the target name before the code is spent. ConsumePairingCode still burns it
// atomically, so two racing pairs leave one winner.
func (s *Store) CheckPairingCode(ctx context.Context, codeHash string) (string, error) {
	var name string
	var expires int64
	var used int
	err := s.db.QueryRowContext(ctx, `SELECT name, expires, used FROM peer_pairing_codes WHERE code_hash = ?`, codeHash).Scan(&name, &expires, &used)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("invalid code")
	}
	if err != nil {
		return "", err
	}
	if used == 1 {
		return "", fmt.Errorf("code used")
	}
	if time.Now().Unix() > expires {
		return "", fmt.Errorf("code expired")
	}
	return name, nil
}

func (s *Store) ConsumePairingCode(ctx context.Context, codeHash string) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var name string
	var expires int64
	var used int
	err = tx.QueryRowContext(ctx, `SELECT name, expires, used FROM peer_pairing_codes WHERE code_hash = ?`, codeHash).Scan(&name, &expires, &used)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("invalid code")
		}
		return "", err
	}
	if used == 1 {
		return "", fmt.Errorf("code used")
	}
	if time.Now().Unix() > expires {
		return "", fmt.Errorf("code expired")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE peer_pairing_codes SET used = 1 WHERE code_hash = ?`, codeHash); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return name, nil
}

func (s *Store) StoreInboundKey(ctx context.Context, peerName, keyHash string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO peer_inbound (peer_name, key_hash) VALUES (?,?) ON CONFLICT(peer_name) DO UPDATE SET key_hash=excluded.key_hash`, peerName, keyHash)
	return err
}

func (s *Store) DeleteInboundKey(ctx context.Context, peerName string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM peer_inbound WHERE peer_name = ?`, peerName)
	return err
}

func (s *Store) InboundKeyHash(ctx context.Context, peerName string) (string, error) {
	var h string
	err := s.db.QueryRowContext(ctx, `SELECT key_hash FROM peer_inbound WHERE peer_name = ?`, peerName).Scan(&h)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return h, err
}

func (s *Store) ListInboundPeers(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT peer_name, key_hash FROM peer_inbound`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var n, h string
		if err := rows.Scan(&n, &h); err != nil {
			return nil, err
		}
		out[n] = h
	}
	return out, rows.Err()
}

// ValidateInboundKey checks presented key against any inbound peer; returns peer name.
func (s *Store) ValidateInboundKey(ctx context.Context, presented string) (string, bool) {
	hash := sha256Hex(presented)
	m, err := s.ListInboundPeers(ctx)
	if err != nil {
		return "", false
	}
	for name, h := range m {
		if h == hash {
			return name, true
		}
	}
	return "", false
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
