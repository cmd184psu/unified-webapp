// Copyright 2026 FreeSociety3 contributors
// SPDX-License-Identifier: Apache-2.0

package gitsvc

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// UpdateRef points a ref at a commit, creating it if needed. Delegation uses
// it to give the base commit a name that a bundle can carry.
func (s *Service) UpdateRef(ctx context.Context, repo, ref, commit string) error {
	if !strings.HasPrefix(ref, "refs/agentflow-delegated/") {
		return fmt.Errorf("%w: %q", ErrForbiddenRef, ref)
	}
	if _, err := s.resolveCommit(ctx, repo, commit); err != nil {
		return err
	}
	unlock := s.lockRepo(ctx, repo)
	defer unlock()
	_, err := s.run(ctx, repo, "update-ref", ref, commit)
	return err
}

// BundleCreate writes a git bundle of the given refs to path. It is how
// agentflow moves a repository to a peer (GIT4 forbids push): the file is
// handed over through the peer API, never fetched from GitHub. Refs that do
// not exist are skipped; with none left it reports ErrNotFound rather than
// writing an empty bundle.
func (s *Service) BundleCreate(ctx context.Context, repo, path string, refs []string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("gitsvc: bundle path must be absolute: %q", path)
	}
	var have []string
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" || !strings.HasPrefix(ref, "refs/") {
			continue
		}
		// A trailing * is a branch pattern, which git only accepts through
		// --branches; a plain ref is passed to rev-list as it is.
		if strings.Contains(ref, "*") {
			pattern := strings.TrimPrefix(ref, "refs/heads/")
			if pattern == ref {
				continue
			}
			out, err := s.run(ctx, repo, "for-each-ref", "--format=%(refname)", ref)
			if err != nil || strings.TrimSpace(out) == "" {
				continue
			}
			have = append(have, "--branches="+pattern)
			continue
		}
		if s.refExists(ctx, repo, ref) {
			have = append(have, ref)
		}
	}
	if len(have) == 0 {
		return fmt.Errorf("%w: nothing to bundle", ErrNotFound)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	args := append([]string{"bundle", "create", path}, have...)
	if _, err := s.run(ctx, repo, args...); err != nil {
		return err
	}
	return nil
}

// BundleHeads lists the refs a bundle carries, so a caller can fetch only the
// branches that exist instead of failing on an absent integration branch.
func (s *Service) BundleHeads(ctx context.Context, repo, path string) ([]string, error) {
	out, err := s.run(ctx, repo, "bundle", "list-heads", path)
	if err != nil {
		return nil, err
	}
	var refs []string
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 {
			refs = append(refs, fields[1])
		}
	}
	return refs, nil
}

// BundleFetch imports a bundle's refs into repo under the given refspecs. A
// refspec's destination must be a ref agentflow owns, so a peer can never
// overwrite the user's branches.
func (s *Service) BundleFetch(ctx context.Context, repo, path string, refspecs ...string) error {
	if st, err := os.Stat(path); err != nil || st.IsDir() {
		return fmt.Errorf("%w: bundle %q", ErrNotFound, path)
	}
	for _, spec := range refspecs {
		dest := spec
		if i := strings.LastIndex(spec, ":"); i >= 0 {
			dest = spec[i+1:]
		}
		dest = strings.TrimPrefix(dest, "+")
		if !strings.HasPrefix(dest, "refs/heads/agentflow/") && !strings.HasPrefix(dest, "refs/heads/agentflow-task/") &&
			!strings.HasPrefix(dest, "refs/agentflow-delegated/") {
			return fmt.Errorf("%w: %q", ErrForbiddenRef, spec)
		}
	}
	unlock := s.lockRepo(ctx, repo)
	defer unlock()
	args := append([]string{"fetch", "--no-tags", "--no-write-fetch-head", path}, refspecs...)
	_, err := s.run(ctx, repo, args...)
	return err
}
