// Copyright 2026 FreeSociety3 contributors
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"errors"
	"testing"
	"time"
)

func TestDelegationRoundTrip(t *testing.T) {
	s, _ := openTemp(t)
	newProject(t, s, "proj")
	e := newEffort(t, s, "proj")

	d := Delegation{
		ID:         "del-1",
		Peer:       "builder",
		Project:    "proj",
		Effort:     e.ID,
		TaskKeys:   []string{"A", "B"},
		Reason:     "laptop at capacity",
		Status:     DelegationPending,
		Direction:  "out",
		BaseCommit: "abc123",
	}
	if err := s.InsertDelegation(ctx, d); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetDelegation(ctx, "del-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Peer != "builder" || got.Effort != e.ID || len(got.TaskKeys) != 2 || got.TaskKeys[1] != "B" || got.Direction != "out" || got.BaseCommit != "abc123" {
		t.Fatalf("delegation = %+v", got)
	}
	// A repeated delivery is a primary-key clash, not a second shadow effort.
	if err := s.InsertDelegation(ctx, d); err == nil {
		t.Fatal("a duplicate delegation id was accepted")
	}
	if err := s.UpdateDelegation(ctx, "del-1", DelegationRunning, 42); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetDelegation(ctx, "del-1")
	if got.Status != DelegationRunning || got.RemoteEffort != 42 {
		t.Fatalf("updated = %+v", got)
	}
	if err := s.TouchDelegation(ctx, "del-1", time.UnixMilli(1000)); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetDelegation(ctx, "del-1")
	if got.LastContact.UnixMilli() != 1000 {
		t.Fatalf("last contact = %v", got.LastContact)
	}
	if err := s.SetDelegationDetail(ctx, "del-1", "waiting"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetDelegation(ctx, "del-1")
	if got.Detail != "waiting" {
		t.Fatalf("detail = %q", got.Detail)
	}
	list, err := s.ListDelegations(ctx, "builder")
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if got, err := s.ListDelegations(ctx, "other"); err != nil || len(got) != 0 {
		t.Fatalf("list other = %+v, %v", got, err)
	}
	if err := s.UpdateDelegation(ctx, "missing", DelegationDone, 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing update = %v", err)
	}
}

func TestEffortDelegatedFrom(t *testing.T) {
	s, _ := openTemp(t)
	newProject(t, s, "proj")
	e := newEffort(t, s, "proj")
	if node, err := s.EffortDelegatedFrom(ctx, e.ID); err != nil || node != "" {
		t.Fatalf("fresh effort = %q, %v", node, err)
	}
	if err := s.SetEffortDelegatedFrom(ctx, e.ID, "laptop"); err != nil {
		t.Fatal(err)
	}
	if node, err := s.EffortDelegatedFrom(ctx, e.ID); err != nil || node != "laptop" {
		t.Fatalf("marked effort = %q, %v", node, err)
	}
}
