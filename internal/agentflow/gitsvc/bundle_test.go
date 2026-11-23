// Copyright 2026 FreeSociety3 contributors
// SPDX-License-Identifier: Apache-2.0

package gitsvc

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestBundleRoundTrip covers what delegation relies on: a commit named by a
// delegation ref travels in a bundle, comes out in another repository, and a
// branch pattern is included too.
func TestBundleRoundTrip(t *testing.T) {
	svc, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	src := newRepo(t)
	tip := tgit(t, src, "rev-parse", "HEAD")
	tgit(t, src, "branch", "agentflow/7")
	tgit(t, src, "branch", "agentflow-task/7/A")

	const id = "del-test"
	if err := svc.UpdateRef(bg, src, "refs/agentflow-delegated/"+id+"/base", tip); err != nil {
		t.Fatal(err)
	}
	bundlePath := filepath.Join(t.TempDir(), "effort.bundle")
	if err := svc.BundleCreate(bg, src, bundlePath, []string{
		"refs/agentflow-delegated/" + id + "/base",
		"refs/heads/agentflow/7",
		"refs/heads/agentflow-task/7/*",
	}); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(bundlePath); err != nil || st.Size() == 0 {
		t.Fatalf("bundle = %v, %v", st, err)
	}

	dst := newRepo(t)
	refs := []string{
		"+refs/agentflow-delegated/" + id + "/*:refs/agentflow-delegated/" + id + "/*",
		"+refs/heads/agentflow/7:refs/heads/agentflow/7",
		"+refs/heads/agentflow-task/7/*:refs/heads/agentflow-task/7/*",
	}
	if err := svc.BundleFetch(bg, dst, bundlePath, refs...); err != nil {
		t.Fatal(err)
	}
	if got := tgit(t, dst, "rev-parse", "refs/agentflow-delegated/"+id+"/base"); got != tip {
		t.Fatalf("base = %s, want %s", got, tip)
	}
	if got := tgit(t, dst, "rev-parse", "refs/heads/agentflow-task/7/A"); got != tip {
		t.Fatalf("task branch = %s, want %s", got, tip)
	}
}

// TestBundleRefusesForeignRef keeps the peer from writing branches the user
// owns: only agentflow's own namespaces are valid destinations.
func TestBundleRefusesForeignRef(t *testing.T) {
	svc, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := newRepo(t)
	bundlePath := filepath.Join(t.TempDir(), "b.bundle")
	if err := svc.BundleCreate(bg, repo, bundlePath, []string{"refs/heads/main"}); err != nil {
		t.Fatal(err)
	}
	err = svc.BundleFetch(bg, repo, bundlePath, "+refs/heads/main:refs/heads/main")
	if !errors.Is(err, ErrForbiddenRef) {
		t.Fatalf("err = %v, want ErrForbiddenRef", err)
	}
}
