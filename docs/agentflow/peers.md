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
stored as sha256 hashes in the `peer_inbound` table and mirrored into the
config `auth.api_keys` as `peer:<name>` (scope `agentflow`). The mirror is
written with the same admin writer `-gen-api-key` uses and the auth policy is
swapped immediately, so the platform gate accepts the key on the next request
with no restart.

## Routes

Authenticated management (platform gate, module agentflow):

- `GET /api/peers` - list peers (no key material).
- `POST /api/peers/invite {name}` -> `{code, a_url}` (one-time code, >=128
  bits, expires 10 min, single use, stored only as hash).
- `POST /api/peers/accept {a_url, code, name, b_url, host_header?}`.
- `POST /api/peers/{name}/rotate` -> `{ok:true}`; swaps both halves with the
  peer and never returns a key.
- `DELETE /api/peers/{name}` -> revoke both halves (best-effort notify).
- `GET /api/peers/{name}/status` -> test connection (protocol mismatch says
  "upgrade the older node").

Peer routes (peer key, same key as `auth.api_keys`):

- `POST /api/peer/pair` is the ONLY unauthenticated peer route; it is
  allow-listed at the platform gate for the agentflow module only and accepts
  only a valid unused code.
- `POST /api/peer/unpair` requires the presenting peer key and removes only
  that peer. It never matches by `base_url`.
- `POST /api/peer/rotate` requires the old peer key; takes
  `{inbound_key, outbound_key}` and stores the new inbound hash and outbound
  file for that peer.
- `GET /api/peer/status` requires a peer key -> versioned status.

## Pairing handshake

1. Node A: `POST /api/peers/invite {name:'builder'}` -> `{code, a_url}`.
2. Node B: `POST /api/peers/accept {a_url, code, name:'laptop', b_url}`.
   B mints K_B, stores sha256(K_B) as its inbound `peer:laptop` (and mirrors it
   into `auth.api_keys`), then POSTs to A's `/api/peer/pair` with
   `{code, node_name, base_url, host_header, key:K_B, protocol, build}`. The
   Host header defaults to the request's own `Host` when `host_header` is
   omitted. A verifies and burns the code, stores K_B as its outbound
   credential for B, mints K_A, stores sha256(K_A) as inbound `peer:builder`,
   mirrors it, and returns K_A plus A's Host header (`r.Host` as B called it).
   B stores K_A as its outbound credential for A and that Host header in its
   own row for A. The `host_header` B sent in `accept` describes B, so A stores
   it for calls to B:

   | row           | host header source         |
   |---------------|----------------------------|
   | A's row for B | header B sent in `accept`  |
   | B's row for A | header A returned in `pair` |

Names must match `[A-Za-z0-9][A-Za-z0-9._-]{0,63}`; a separator is rejected
before it can become a key-file path. Pairing is refused if either node's name
is taken or the code is wrong/expired/used (generic 403, rate-limited: 5
failures per 10 min then lockout).

## Versioned status

`GET /api/peer/status` returns:
`{protocol:1, node, build, schema, time, load:{1m,cores,free_gb},
accepting, backends:[{name,slots,running,rest_until}],
projects:[{slug,run_state,running_stages,open_efforts,open_tasks}],
delegable:[...]}`.

`free_gb` is a real `statfs` reading of the data directory, `1m` is the host
load average, `running` counts running stages per backend, and `rest_until`
lists resting model end times. Unknown fields are ignored by the client. A
protocol mismatch yields "upgrade the older node", never a crash.

The background poller (every `peer_poll_seconds`) refreshes each enabled
peer's `last_seen`, `last_ok` and `last_error` through the same status call.

## Security

Keys never appear in any response other than the pairing exchange itself,
never in logs, events, UI or errors. Keys exist only in 0600 files and as
sha256 hashes. Rotate never returns a key. Pairing exchanges keys between the
two machines; both must be trusted.

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

Revoke removes the row, the outbound file, the inbound hash and its
`auth.api_keys` mirror on this side, and tells the other side best-effort
(`POST /api/peer/unpair`, authenticated with the key it still holds). Rotate
mints a new outbound key and a new inbound key, asks the peer to swap both,
then stores its halves locally; a peer that refuses leaves the local keys
unchanged.

## Limitations

Rotate requires the peer to be reachable: the management call is all-or-
nothing, so an offline peer keeps the old pair until it is reachable again.
The mirror write uses the admin config writer, which rewrites the `auth`
member of the config file; every other byte of the file is preserved.
