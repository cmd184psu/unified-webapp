# AgentFlow in containers (AF-12)

A project may run every stage — worker, verifier, reviewer — inside a container
instead of the host sandbox. This is how a toolchain that does not exist on the
host becomes usable: the C# SDK, .NET, and Ghidra with its linux_arm_64
decompiler all live in the worker image.

The host sandbox stays the default. A project opts in with `sandbox: "docker"`.

## Project settings

| Field | Default | Meaning |
|---|---|---|
| `sandbox` | `host` | `host` (bwrap / Seatbelt) or `docker`. |
| `docker_image` | `agentflow-worker:0.1` | Image `name:tag` for every stage of the project. |
| `docker_network` | `none` | `none`, `provider` or `full` (see below). |
| `docker_mounts` | `[]` | Extra binds: `"src:dst"` or `"src:dst:rw"`, absolute paths, read-only unless `rw`. |
| `docker_selinux` | `false` | Relabel binds (`:z`) on SELinux hosts. |

`toolchains`, `feedback_env` and `verify_in_copy` still apply; a docker project
usually needs none of them, because the image already has the SDK.

## Host environment

| Variable | Default | Meaning |
|---|---|---|
| `AGENTFLOW_DOCKER_IDLE` | `15m` | How long the runtime may sit with no agentflow container before it is stopped (macOS). |
| `AGENTFLOW_DOCKER_PROXY` | — | Host proxy URL used by a `provider` stage. Required when `docker_network` is `provider`. |
| `AGENTFLOW_DOCKER_RUNNER` | — | Path of the model runner **inside the image**. Only needed when model stages themselves run in containers (see below). |

## What the container gets

- The worktree at its **host path**, read-write for a worker and read-only for
  verify, review and planning stages. The git admin dir (`<repo>/.git` and
  `<repo>/.git/worktrees/<id>`) and the worktree's `.git` file are read-only:
  commits stay on the host.
- The stage home at `/home/agent`, which is `HOME`. Everything a build writes
  (Go module cache, NuGet packages already vendored in the image, `obj/`) stays
  in that directory. `~/.cache` is a container-local tmpfs, not the host mount:
  Ghidra cannot create its XDG cache on virtiofs (even as root) and refuses to
  start, so the cache is per-stage. Set `XDG_CACHE_HOME` through `feedback_env`
  only if a project really needs a different one.
- No server environment: the same allowlist as the host sandbox (`HOME`, `LANG`,
  `TERM`, `GIT_*`, `XDG_*`, `GOCACHE`, `GOMODCACHE`, `GOPATH`, `GOTOOLCHAIN`)
  plus what the stage passes. Git and forge credentials, `~/.ssh` and the
  server's data directory are not mounted.
- What the stage passes (its backend's API key among it) is named on the
  `docker run` command line (`--env NAME`) and its value travels in the docker
  CLI's own environment, so it never appears in `ps` or a log.
- No network by default.

Containers are named `af-<project>-<effort>-<task>-<stage>` and labelled
`agentflow=1`, so a cancel can `docker kill` the exact container and cleanup can
find leftovers. A model stage, which the engine does not tie to a task, is named
`af-<project>-0-0-stage-<id>`.

Every bind source (the worktree, the stage home, the repository's `.git`, extra
mounts) must be inside a directory the Colima VM shares, or it would be an empty
directory in the container. The sandbox reads the `mounts:` of
`~/.colima/default/colima.yaml` and refuses a stage whose paths are not shared,
naming the path. For an instance that means its data directory and its projects'
repositories. A native Linux daemon sees every path, so there is no check there.

## Network modes

- **none** — `--network none`. A Ghidra report task or anything that only reads
  files. This is the default and is what the integration test proves.
- **provider** — bridge networking with `HTTP(S)_PROXY` pointing at
  `AGENTFLOW_DOCKER_PROXY` and `host.docker.internal` mapped to the host. Use it
  when a stage must reach its model provider. The proxy allowlist is the network
  control; Docker cannot filter individual hosts without an egress gateway.
- **full** — plain bridge networking. Use sparingly.

The image is configured per project, so the same server can run one project on
the host and another in `agentflow-worker:0.1`.

## Runtime lifecycle

The owner does not want a VM running unless work needs it.

**macOS (Colima).** Before the first container the manager checks
`docker info`; only when it fails does it run `colima start` (no flags: it reuses
`~/.colima/default/colima.yaml`) and wait for the daemon, two minutes by default.
After the last agentflow container has been gone for `AGENTFLOW_DOCKER_IDLE`,
`colima stop` runs. Starts and stops are serialized, never at login. A VM that
cannot start is logged as `ATTENTION:` and fails the stage with the reason
instead of retrying silently; the task ends as needing attention.

**Linux.** Docker is native. "Start when needed" is a `docker info` check; the
daemon is never started, stopped, installed or reconfigured. An unreachable
daemon reports how to start it (`docker.socket`, or `DOCKER_HOST` for rootless
docker/podman) and fails the stage.

Containers are not children of the server. A cancel calls `docker kill` by name,
and `--rm` removes the container when it exits. A stage whose name a crashed run
left behind is replaced, not duplicated: the deterministic name is the adoption
key.

## Model stages in containers

A docker project's model stages run in the same image. The image must then carry
the runner (opencode) and the project must name it:

```sh
AGENTFLOW_DOCKER_RUNNER=/usr/local/bin/opencode
```

Without it the adapter mounts the host binary at `/sandbox/bin/opencode`; that
works on a Linux host and fails clearly on macOS, whose binary cannot run in a
Linux container. Verify and report tasks — the reason to use a container at
all — do not need the runner.

## Rebuilding the image

The image lives outside this repository, at `~/work/agentflow-image`:

```sh
docker buildx build --load \
  --platform linux/arm64,linux/amd64 \
  -t agentflow-worker:0.1 \
  --build-arg GHIDRA_URL=<Ghidra 12.2 zip URL> \
  ~/work/agentflow-image
```

The Dockerfile installs Ubuntu 24.04, Go, .NET 8, OpenJDK 25, Ghidra 12.2 and
tmux. TARGETARCH selects the Go/JDK arch; on arm64 only, it builds Ghidra's
decompiler from the bundled source into `os/linux_arm_64`, because the release
ships x86_64 natives only. Use `--load` for one platform on a workstation;
`--push`/registry for a multi-arch image on the LAN.

Ghidra headless needs `--hostname localhost` (the container sets it), a
`user.name` for its project-owner check, and a settings directory it can
create. Java cannot create that directory on the virtiofs mount, so point
Ghidra's home at container-local storage through `feedback_env`:

```
feedback_env: {
  "JAVA_TOOL_OPTIONS": "-Duser.home=/tmp/ghidra-home -Duser.name=agentflow",
  "XDG_CONFIG_HOME": "/tmp/ghidra-config"
}
```

`XDG_CONFIG_HOME` matters too: Ghidra prefers it over `user.home`, and the
sandbox sets it to the host-backed stage home.

A working report command (the project directory must exist first):

```sh
mkdir -p /tmp/ghidra-home /tmp/ghidra-config "$HOME/ghidra" &&
  analyzeHeadless "$HOME/ghidra" af12 -import /bin/true -noanalysis
```

## Linux specifics

- Bind mounts keep real ownership: containers run with `--user <host uid>:<host
  gid>`, so build output is not root-owned. macOS uses the image's user;
  Colima/virtiofs maps ownership, and .NET's first-run mutex needs a real passwd
  entry, so an arbitrary uid there is not an option.
- SELinux hosts set `docker_selinux` to add `:z`.
- The image builds for arm64 and amd64; nothing is hard-coded to `/Users` or
  `/opt/homebrew`.
- The existing bwrap sandbox stays the default; a project selects docker.

## Seat mode (AF-11)

A seat runs its tmux session inside the stage's container, not beside it. The
interface is `dockervm.Manager.AttachCommand(ctx, container, session)`, which
ensures the runtime first and returns `docker exec -it <container> tmux attach -t
<session>`. AF-11 owns the session name; this task only owns the runtime and the
name.

## Tests

Unit tests use fake `docker`/`colima` scripts and a fake driver. The container
integration test is skipped unless asked for:

```sh
AGENTFLOW_DOCKER_IT=1 go test ./internal/agentflow/sandbox -run Integration -v
```

It runs a Go build, a C# `dotnet build -m:1`, proves the host HOME and a
non-mounted sibling path are invisible, and proves `--network none` blocks
egress.
