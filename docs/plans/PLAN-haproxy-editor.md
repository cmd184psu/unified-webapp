# HAProxy editor: status

What it is: a module that edits HAProxy from a browser, like smbedit does for Samba. Requirements are in
`docs/frd/FRD-haproxy-editor.md`; how to run it is in `docs/guides/haproxy.md`.

## Built

- CertMachine API additions (list filters, `ETag`, `X-Cert-Id`, `X-Cert-Fingerprint`, `If-None-Match`, `HEAD`).
- The module: model and import of an existing `haproxy.cfg`, generator, Ubuntu/Rocky/macOS drivers, Apply with
  backup and rollback (and it starts HAProxy if it was stopped), certificates pulled from CertMachine and
  checked against their ETag, referential checks, a React UI (services, globals, certificates, backups, log,
  raw, stats), and `backup_dir`.
- Tests: `make test` and `npm run test:web` pass. Everything uses fakes; nothing has been run against a real
  HAProxy.

## Checked by you

- CertMachine extension against the live CertMachine (`a-og-curl-checks.sh`): passed.
- The editor's CertMachine client against the live CertMachine (`TestLiveCertMachineClient`, opt-in): passed.

## Not done

- Trying the editor in a browser, on the Mac and on Linux, against a real HAProxy.
- A settings screen for the module (CertMachine URL and key, paths, backups). Today these are config-file
  only; see P8-9 in `PLAN-phase8-punchlist.md`.
- Per-module API keys (P8-7).
