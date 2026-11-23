# Agentflow peer delegation (AF-25)

Nodes delegate an effort (or a set of task keys) to a paired peer. The owning
node stays authoritative for state; the peer only executes. Work that must have
a single writer — coordination, seat feeding, reviews and merges — stays local.

## Load-aware start gate

A node does not start a new stage when:

- 1-min load > `load_cap_tenths`/10 * cores (default 1.5x cores), or
- free memory < `min_free_mem_gb` (default 2 GB), or
- free disk < `min_free_space_gb` (default 20 GB).

A zero limit turns that limit off, except the load cap, which falls back to
1.5x cores. Running stages are never killed for load; a closed gate defers the
start and it goes as soon as the gate clears. The same gate decides `accepting`
in `/api/peer/status`, so a peer's status tells the owner whether work will
start now or wait.

## Delegate

`POST /api/peers/{name}/delegate {project, effort, tasks?, reason?}` — `tasks`
is optional and defaults to the whole approved queue. Only an effort whose FRD,
plan and queue are approved can be delegated.

The owner first asks the peer for its status. A project the peer does not have
is refused (`409`, with the peer's `delegable` list); agentflow never
auto-clones a repository for a peer. The effort is then shipped as:

- the same JSON export `GET /api/efforts/{id}/export` produces, and
- a git bundle carrying the base commit (named
  `refs/agentflow-delegated/<id>/base`), the effort's integration branch and
  its task branches.

Both travel in the request body to `POST /api/peer/delegate`; nothing is ever
fetched from GitHub and nothing is pushed. The peer verifies the bundle hash,
fetches it into `refs/agentflow-delegated/<id>/…`, imports the effort as a
shadow and marks it delegated-from the owner. When the load gate is open it
starts the run; otherwise the delegation stays `pending` and the peer loop
starts it when the gate clears.

Delivery is idempotent by delegation ID: a repeated delivery returns the
existing shadow instead of creating a second effort.

### Status and cancel

- Owner: `GET /api/delegations`, `GET /api/delegations/{id}` (polls the peer
  and falls back to stored state), `POST /api/peers/{name}/delegations/{id}/cancel`.
- Peer: `GET /api/peer/delegations/{id}`, `POST /api/peer/delegations/{id}/cancel`,
  both authenticated with the inbound peer key.

Cancel from the owner stops the run on the peer, abandons the shadow effort so
the project is free again, and records `cancelled` on both nodes. A shadow
effort that completes on its own reports `done`.

### Results

`GET /api/peer/result/{id}` bundles the shadow effort's integration and task
branches. `POST /api/delegations/{id}/collect` on the owner fetches that bundle
into the owner's own branch names (`agentflow/<owner effort>` and
`agentflow-task/<owner effort>/…`), so verify and merge run locally as usual.

### Fallback

If the peer stops answering for `delegation_timeout_minutes` (default 30), the
owner marks the delegation `fallback` and re-queues the effort on its own
dispatcher. The effort never runs in two places without the owner recording
which node holds it.

## Shared rest times

Per-backend `rest_until` lists are exchanged through `/api/peer/status`, both
directions, on every peer loop pass. A rest that ends later wins; a model this
node does not have is ignored, so one node's 429 does not burn the other's
tries. Per-node slot limits stay local; there is no cross-node slot sharing.

## Settings

| Key | Default | Meaning |
|---|---|---|
| `load_cap_tenths` | 15 | load cap in tenths of cores |
| `min_free_mem_gb` | 2 | free-memory floor |
| `min_free_space_gb` | 20 | free-disk floor |
| `delegation_timeout_minutes` | 30 | peer silence before fallback |

## Dev demo

Use `scripts/agentflow-peer-dev-test.sh` against two dev instances on
8788/8799. Never use production (8787) or builder.
