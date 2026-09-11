#!/usr/bin/env bash
# One-time setup for local module testing.
# Run from the repo root:  bash local-test/setup.sh
# Then start the server:   go run ./cmd/server -config local-test/config.json
#
# Test credentials this profile uses (LOCAL TESTING ONLY):
#   PIN login (todo, slideshow):  1234
#   Admin PIN (admin.test):      424242
#   API key (menuserver, multissh):
#     varOO_vuQyged_rklN3ujsy2tgQAcEs-9Ln13hDIyh0
#   LDAP (obsidianoid, multissh) via glauth (see glauth.cfg):
#     user "chris" / password "ldap-test-1"
set -euo pipefail

cd "$(dirname "$0")/.."   # repo root
LT=local-test

echo "== creating data directories =="
mkdir -p "$LT/data/todo/home" \
         "$LT/data/slideshow/sunsets" "$LT/data/slideshow/mountains" \
         "$LT/data/menuserver/home" \
         "$LT/data/obsidianoid" \
         "$LT/data/vault/Threads" \
         "$LT/data/multissh/uploads" \
         "$LT/data/auth"

echo "== admin PIN file (plaintext PIN, must be chmod 0400) =="
if [ ! -f "$LT/admin.pin" ]; then
  umask 077
  printf '424242\n' > "$LT/admin.pin"
  chmod 0400 "$LT/admin.pin"
  echo "   wrote $LT/admin.pin (PIN: 424242)"
else
  echo "   $LT/admin.pin already exists, leaving it alone"
fi

echo "== seeding sample slideshow images =="
python3 - <<'EOF'
import struct, zlib, os

def png(path, r, g, b, size=64):
    row = b'\x00' + bytes([r, g, b]) * size
    raw = row * size
    def chunk(tag, data):
        c = tag + data
        return struct.pack('>I', len(data)) + c + struct.pack('>I', zlib.crc32(c))
    ihdr = struct.pack('>IIBBBBB', size, size, 8, 2, 0, 0, 0)
    with open(path, 'wb') as f:
        f.write(b'\x89PNG\r\n\x1a\n')
        f.write(chunk(b'IHDR', ihdr))
        f.write(chunk(b'IDAT', zlib.compress(raw)))
        f.write(chunk(b'IEND', b''))

base = 'local-test/data/slideshow'
png(f'{base}/sunsets/orange.png', 235, 120, 40)
png(f'{base}/sunsets/purple.png', 120, 60, 160)
png(f'{base}/mountains/blue.png', 60, 100, 180)
png(f'{base}/mountains/green.png', 60, 150, 90)
print('   4 solid-color PNGs under', base)
EOF

echo "== seeding obsidianoid test vault =="
if [ ! -f "$LT/data/vault/Welcome.md" ]; then
  cat > "$LT/data/vault/Welcome.md" <<'EOF'
# Welcome

This is a **test vault** for obsidianoid.

- Edit this note and watch autosave
- Create a new note with the + button
- Switch to the Threads view for scratch slots
EOF
fi

echo "== seeding a menuserver page =="
if [ ! -f "$LT/data/menuserver/home/links.json" ]; then
  cat > "$LT/data/menuserver/home/links.json" <<'EOF'
{
  "id": "homelinks",
  "title": "Home Links",
  "sites": [
    { "label": "Router", "url": "http://192.168.1.1", "username": "admin", "password": "changeme" },
    { "label": "NAS", "url": "http://nas.local:5000" }
  ],
  "notes": "Test page seeded by local-test/setup.sh - fake credentials."
}
EOF
fi

echo
echo "== /etc/hosts check =="
# The profile uses .test (reserved for exactly this, RFC 6761) rather than
# .local, which belongs to Bonjour/mDNS on macOS and stalls every page load
# ~5s waiting on multicast AAAA lookups. Add both lines so IPv4 and IPv6
# lookups resolve from /etc/hosts without touching a DNS server.
HOSTNAMES="grocery.test todo.test slideshow.test menu.test menuserver.test obsidianoid.test multissh.test admin.test"
MISSING4=""
MISSING6=""
for h in $HOSTNAMES; do
  grep -qE "^127\.0\.0\.1[[:space:]].*[[:space:]]$h([[:space:]]|\$)" /etc/hosts || MISSING4="$MISSING4 $h"
  grep -qE "^::1[[:space:]].*[[:space:]]$h([[:space:]]|\$)" /etc/hosts || MISSING6="$MISSING6 $h"
done
if [ -n "$MISSING4" ] || [ -n "$MISSING6" ]; then
  echo "   Add BOTH lines to /etc/hosts (sudo required):"
  echo
  [ -n "$MISSING4" ] && echo "   127.0.0.1 $HOSTNAMES"
  [ -n "$MISSING6" ] && echo "   ::1 $HOSTNAMES"
  echo
else
  echo "   all .test hostnames present (IPv4 and IPv6)"
fi

echo "== done =="
echo "Start the server:   go run ./cmd/server -config local-test/config.json"
echo "Optional LDAP:      glauth -c local-test/glauth.cfg   (needed for obsidianoid/multissh login)"
echo "Full instructions:  docs/USERGUIDE.md"
