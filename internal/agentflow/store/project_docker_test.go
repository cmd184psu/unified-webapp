package store

import (
	"path/filepath"
	"testing"
)

// TestProjectDockerSettingsRoundTrip: a docker project's sandbox, image,
// network, mounts and SELinux flag are stored and read back (AF-12).
func TestProjectDockerSettingsRoundTrip(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "agentflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	repo := mkRepo(t)
	p := Project{Slug: "oc3", RepoPath: repo, ModelScope: "local",
		Sandbox: "docker", DockerImage: "agentflow-worker:0.1", DockerNetwork: "none",
		DockerMounts: []string{"/Games/Civ3Complete:/civ3:ro", "/tmp/re:/re:rw"}, DockerSELinux: true}
	if err := s.CreateProject(ctx, "t", p); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetProject(ctx, "oc3")
	if err != nil {
		t.Fatal(err)
	}
	if got.Sandbox != "docker" || got.DockerImage != "agentflow-worker:0.1" || got.DockerNetwork != "none" || !got.DockerSELinux {
		t.Fatalf("after create: %+v", got)
	}
	if len(got.DockerMounts) != 2 || got.DockerMounts[0] != "/Games/Civ3Complete:/civ3:ro" {
		t.Fatalf("mounts: %v", got.DockerMounts)
	}
	got.Sandbox, got.DockerImage, got.DockerMounts, got.DockerSELinux = "host", "", nil, false
	if err := s.UpdateProject(ctx, "t", got); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetProject(ctx, "oc3")
	if err != nil {
		t.Fatal(err)
	}
	if got.Sandbox != "host" || len(got.DockerMounts) != 0 || got.DockerSELinux {
		t.Fatalf("after clearing: %+v", got)
	}
}

// TestProjectDockerSettingsValidation: a mode, image, network or mount that
// cannot work is refused naming the field.
func TestProjectDockerSettingsValidation(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "agentflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	repo := mkRepo(t)
	for _, c := range []struct {
		name  string
		p     Project
		field string
	}{
		{"mode", Project{Sandbox: "podman"}, "sandbox"},
		{"image", Project{Sandbox: "docker", DockerImage: "worker"}, "docker_image"},
		{"network", Project{Sandbox: "docker", DockerNetwork: "bridge"}, "docker_network"},
		{"mount", Project{Sandbox: "docker", DockerMounts: []string{"relative:/x"}}, "docker_mounts[0]"},
	} {
		t.Run(c.name, func(t *testing.T) {
			c.p.Slug, c.p.RepoPath, c.p.ModelScope = "p-"+c.name, repo, "local"
			c.p.Slug = "d" + c.name[:1]
			wantField(t, s.CreateProject(ctx, "t", c.p), c.field)
		})
	}
}

// TestProjectDefaultSandboxIsHost: an unset mode is the host sandbox, so
// projects created before AF-12 keep running exactly as they did.
func TestProjectDefaultSandboxIsHost(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "agentflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.CreateProject(ctx, "t", Project{Slug: "plain", RepoPath: mkRepo(t), ModelScope: "local"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetProject(ctx, "plain")
	if err != nil {
		t.Fatal(err)
	}
	if got.Sandbox != "host" {
		t.Fatalf("sandbox = %q, want host", got.Sandbox)
	}
}
