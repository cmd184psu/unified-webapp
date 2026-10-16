package store

import (
	"errors"
	"testing"
)

// H6: an install is one row with its progress; the same model cannot be pulled
// twice at once; only Ollama backends take installs; a restart fails the
// installs that were running.
func TestH6InstallRows(t *testing.T) {
	s, _ := openTemp(t)
	for _, b := range []Backend{
		{Name: "gpu", Host: "http://127.0.0.1:11434", APIType: "ollama", Slots: 1},
		{Name: "cloud", Host: "https://api.example.com", APIType: "openai_compatible", Slots: 1},
	} {
		if err := s.CreateBackend(ctx, "t", b); err != nil {
			t.Fatal(err)
		}
	}
	in, err := s.CreateInstall(ctx, "t", "gpu", " qwen3:8b ", 2)
	if err != nil || in.State != InstallRunning || in.Model != "qwen3:8b" || in.Tier != 2 || in.Started == 0 {
		t.Fatalf("install = %+v (%v)", in, err)
	}
	if _, err := s.CreateInstall(ctx, "t", "gpu", "qwen3:8b", 2); !errors.Is(err, ErrConflict) {
		t.Fatalf("a second pull of the same model: %v, want ErrConflict", err)
	}
	if _, err := s.CreateInstall(ctx, "t", "gpu", "other", 0); err != nil {
		t.Fatalf("a different model: %v", err)
	}
	if _, err := s.CreateInstall(ctx, "t", "cloud", "m", 0); !errors.Is(err, ErrInvalid) {
		t.Fatalf("install on a non-ollama backend: %v, want ErrInvalid", err)
	}
	if _, err := s.CreateInstall(ctx, "t", "ghost", "m", 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("install on an unknown backend: %v, want ErrNotFound", err)
	}
	if _, err := s.CreateInstall(ctx, "t", "gpu", "", 0); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty model: %v", err)
	}
	if _, err := s.CreateInstall(ctx, "t", "gpu", "x", 4); !errors.Is(err, ErrInvalid) {
		t.Fatalf("tier 4: %v", err)
	}
	if err := s.SetInstallProgress(ctx, in.ID, "pulling abc", 50, 200); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetInstall(ctx, in.ID)
	if got.Status != "pulling abc" || got.Completed != 50 || got.Total != 200 {
		t.Fatalf("progress = %+v", got)
	}
	if err := s.FinishInstall(ctx, in.ID, InstallDone, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.SetInstallProgress(ctx, in.ID, "late", 1, 1); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetInstall(ctx, in.ID)
	if got.State != InstallDone || got.Finished == 0 || got.Status != "pulling abc" {
		t.Fatalf("a finished install changed: %+v", got)
	}
	if _, err := s.CreateInstall(ctx, "t", "gpu", "qwen3:8b", 2); err != nil {
		t.Fatalf("pulling again after it finished: %v", err)
	}
	list, err := s.ListInstalls(ctx, "gpu", 10)
	if err != nil || len(list) != 3 || list[0].Model != "qwen3:8b" {
		t.Fatalf("list = %+v (%v)", list, err)
	}
	n, err := s.FailRunningInstalls(ctx, "interrupted by a restart")
	if err != nil || n != 2 {
		t.Fatalf("failed %d running installs (%v), want 2", n, err)
	}
	if list, _ = s.ListInstalls(ctx, "", 10); list[0].State != InstallFailed || list[0].Error == "" {
		t.Fatalf("after restart: %+v", list[0])
	}
}
