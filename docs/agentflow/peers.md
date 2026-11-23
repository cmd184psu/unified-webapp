# Agentflow peers (AF-24)

Agentflows (not agents, not BotChat) talk to each other. Each node mints an
API key on each side and exchanges them. Pairing, storage, outbound client
and versioned status are described here.

## Settings

- `peers_enabled` (default off): master switch. Pairing routes return 404
  when off.
- `node_name` (default empty): this node's name in peer status; empty derives
  from hostname.
- `peer_poll_seconds` (default 60): background poll interval.

## Storage

Table `peers(name, base_url, host_header, enabled, protocol, remote_build,
paired_at, last_seen, last_ok, last_error, insecure_skip_verify,
delegable_json)`.

Outbound keys live ONLY in `0600` files `<data_dir>/peers/<name>.key` (never
in the DB, never in backups, never in any API/log/event). Inbound keys are
stored as sha256 hashes in the `peer_inbound` table (and, for the platform
gate, in the config `auth.api_keys` as `peer:<name>`).

## Routes

Authenticated management (platform gate, module agentflow):

- `GET /api/peers` - list peers (no key material).
- `POST /api/peers/invite {name}` -> `{code, a_url}` (one-time code, >=128
  bits, expires 10 min, single use, stored only as hash).
- `POST /api/peers/accept {a_url, code, name, b_url}`.
- `POST /api/peers/{name}/rotate` -> `{key}`.
- `DELETE /api/peers/{name}` -> revoke both halves (best-effort notify).
- `GET /api/peers/{name}/status` -> test connection.

Unauthenticated peer routes (allow-listed):

- `POST /api/peer/pair` (only unauthenticated peer route; accepts only a
  valid unused code).
- `POST /api/peer/unpair` (best-effort).
- `GET /api/peer/status` (needs a peer key) -> versioned status.

## Pairing handshake

1. Node A: `POST /api/peers/invite {name:'builder'}` -> `{code, a_url}`.
2. Node B: `POST /api/peers/accept {a_url, code, name:'laptop', b_url}`.
   B mints K_B, POSTs to A's `/api/peer/pair` with `{code, node_name, base_url,
   host_header, key:K_B, protocol, build}`. A verifies and burns the code,
   stores K_B as its outbound credential for B, mints K_A, stores
   sha256(K_A) as inbound `peer:<B>`, returns K_A. B stores K_A as its
   outbound credential for A.

Pairing is refused if either node's name is taken or the code is
wrong/expired/used (generic 403, rate-limited: 5 failures per 10 min then
lockout).

## Versioned status

`GET /api/peer/status` returns:
`{protocol:1, node, build, schema, time, load:{1m,cores,free_gb,free_mem_gb},
accepting, backends:[{name,slots,running,rest_until}],
projects:[{slug,run_state,running_stages,open_efforts}],
delegable:[...], rest_until:{"<backend>\u0000<model>": until}}`.

`accepting` is the load-aware start gate (AF-25), and the top-level
`rest_until` map is how nodes share rate-limit rests; see
`docs/agentflow/peers-delegation.md`.

Unknown fields ignored by client. Protocol mismatch yields a clear "upgrade
the older node" message, never a crash.

## Security

Keys never appear in any response other than the pairing exchange itself,
never in logs, events, UI or errors. Keys exist only in 0600 files and as
sha256 hashes. Pairing exchanges keys between the two machines; both must be
trusted.

## Pairing two nodes in 3 commands

On node A:
```
curl -s -X POST $A/api/peers/invite -d '{"name":"builder"}'
```
On node B:
```
curl -s -X POST $B/api/peers/accept -d '{"a_url":"'$A'","code":"<code>","name":"laptop","b_url":"'$B'"}'
```
Revoke on either side:
```
curl -s -X DELETE $A/api/peers/builder
```

## Revoke / rotate

Revoke removes the row, the outbound file and the inbound key on this side,
and tells the other side best-effort (`POST /api/peer/unpair`) so it removes
its half. Rotate mints a new key each way.

## Limitations

Live reload of the platform auth gate's inbound key is not performed by
agentflow (it does not own the auth service); the config file is written by
the admin tooling. Agentflow validates peer keys against its own
`peer_inbound` table, so pairing works without a restart. A drain-safe
reload is used if the platform gate must learn the new key.
EOF
echo done