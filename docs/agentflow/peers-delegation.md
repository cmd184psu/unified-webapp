# Agentflow peer delegation (AF-25)

Nodes delegate an effort (or a set of task keys) to a paired peer. The owning
node stays authoritative for state; the peer only executes.

## Load-aware start gate

A node does not start a new stage when:
- 1-min load > `load_cap_tenths`/10 * cores (default 1.5x cores), or
- free memory < `min_free_mem_gb` (default 2 GB), or
- free disk < `min_free_space_gb` (default 20 GB).

Running stages are never killed for load; a closed gate defers the start to the
next tick. The same gate decides `accepting` in `/api/peer/status`.

## Delegate

`POST /api/peers/{name}/delegate {project, effort, tasks, reason}`. The project
must already be present on the peer (listed in its `delegable` set); agentflow
never auto-clones. The owner records a delegation and the peer runs a shadow
effort. Status: `GET /api/delegations/{id}` and `GET /api/peer/delegations/{id}`.

Cancel from the owner cancels on the peer. If the peer goes offline, the owner
re-queues locally after `delegation_timeout_minutes`.

## Shared rest times

Per-backend `rest_until` lists are exchanged through `/api/peer/status` so one
node's 429 does not burn the other's tries. Per-node slot limits stay local;
set `account_group` to share a slot budget across nodes using the same account.

## Dev demo

Use `scripts/agentflow-peer-dev-test.sh` against dev instances on 8788/8799.
Never use production (8787) or builder.
