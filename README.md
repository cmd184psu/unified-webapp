# unified-webapp

Unified Go server for todo-list, slideshow, menuserver, and grocery-list. A single binary replaces four separate services. HAProxy (or `/etc/hosts`) routes hostnames to the correct module; the Go server dispatches on the `Host` header.

---

## How It Works

Every request carries a `Host` header. The server reads that header and looks up the matching module in `host_routing`. If no entry matches, it returns 404. This means the server does nothing useful until `host_routing` is configured.

---

## Quick Start

### 1. Generate the default config

```bash
make init-config
```

This writes `~/.unified-webapp.json` with all keys present and empty `host_routing`.

### 2. Edit the config

Open `~/.unified-webapp.json` and fill in:

- `host_routing` — map hostname → module name
- Module `static_dir` and `data_dir` / `data_file` paths (use absolute paths in production)

Minimal example with all four modules. Multiple hostnames can map to the same module — useful for adding `-test` aliases that won't collide with live services on your network:

```json
{
  "port": 8080,
  "tls_cert": "",
  "tls_key": "",
  "host_routing": {
    "grocery.cmdhome.net":        "grocery",
    "grocery-test.cmdhome.net":   "grocery",
    "todolist.cmdhome.net":       "todo",
    "todo-test.cmdhome.net":      "todo",
    "slideshow.cmdhome.net":      "slideshow",
    "slideshow-test.cmdhome.net": "slideshow",
    "menu.cmdhome.net":           "menuserver",
    "menu-test.cmdhome.net":      "menuserver"
  },
  "grocery": {
    "static_dir": "/opt/unified-webapp/web/grocery",
    "data_file":  "/data/grocery.json",
    "groups": ["Produce", "Meats", "mid store", "back wall", "frozen", "deli area near front"],
    "title": "Grocery List",
    "sync_interval_seconds": 1
  },
  "todo": {
    "static_dir": "/opt/unified-webapp/web/todo",
    "data_dir":   "/data/todo",
    "ext": "json",
    "default_subject": "home",
    "sync_interval_seconds": 1
  },
  "slideshow": {
    "static_dir": "/opt/unified-webapp/web/slideshow",
    "image_dir":  "/data/slideshow",
    "prefix":     "slides",
    "default_subject": ""
  },
  "menuserver": {
    "static_dir": "/opt/unified-webapp/web/menuserver",
    "data_dir":   "/data/menuserver",
    "show_all_pages": false
  }
}
```

### 3. Run

```bash
make run
```

---

## Local Testing (No DNS, No HAProxy)

The server dispatches on the `Host` header, so a plain `http://localhost:8080` in a browser sends `Host: localhost` — which won't match any module unless you add a `"localhost"` entry. Three options:

### Option A — `/etc/hosts` (recommended for browser testing)

Use `-test` hostnames so you don't shadow live services already on your network. Add to `/etc/hosts`:

```
127.0.0.1  grocery-test.cmdhome.net  todo-test.cmdhome.net  slideshow-test.cmdhome.net  menu-test.cmdhome.net
```

Make sure those same names are in `host_routing` in your config (see the example above). Then open any of these in a browser:

```
http://grocery-test.cmdhome.net:8080
http://todo-test.cmdhome.net:8080
http://slideshow-test.cmdhome.net:8080
http://menu-test.cmdhome.net:8080
```

No HAProxy needed. Undo by removing the line from `/etc/hosts`. The production hostnames (`grocery.cmdhome.net`, etc.) continue to work via DNS as normal — the `-test` entries only resolve to localhost on this machine.

### Option B — curl with explicit Host header

No config changes needed. Useful for API testing:

```bash
curl -s -H "Host: todo-test.cmdhome.net"      http://localhost:8080/config/
curl -s -H "Host: grocery-test.cmdhome.net"   http://localhost:8080/items
curl -s -H "Host: slideshow-test.cmdhome.net" http://localhost:8080/items
curl -s -H "Host: menu-test.cmdhome.net"      http://localhost:8080/items
```

### Option C — localhost fallback in config

Add one entry to `host_routing` to make `http://localhost:8080` open a specific module directly in any browser, no `/etc/hosts` required:

```json
"host_routing": {
  "localhost":                  "todo",
  "todo-test.cmdhome.net":      "todo",
  "todolist.cmdhome.net":       "todo",
  ...
}
```

Only one module can be the `localhost` fallback at a time. Change it to switch which module opens at `http://localhost:8080`.

---

## Data Directory Layout

### Grocery

Single JSON file:

```
/data/grocery.json
```

### Todo

One JSON file per list item, grouped by subject directory:

```
/data/todo/
  home/
    shopping.json
    tasks.json
  work/
    standup.json
```

`/items/{subject}/index.json` is generated dynamically — do not create it on disk.

### Slideshow

One subdirectory per subject, images inside:

```
/data/slideshow/
  vacation/
    beach.jpg
    sunset.png
  family/
    birthday.jpg
```

Supported formats: `.jpg`, `.jpeg`, `.png`, `.gif`

### Menuserver

One subdirectory per subject, JSON menu files inside:

```
/data/menuserver/
  home/
    networking.json
    streaming.json
```

Each menu file follows this shape:

```json
{
  "id": "networking",
  "title": "Networking",
  "sites": [
    { "label": "Router", "url": "http://192.168.1.1" }
  ],
  "notes": ""
}
```

---

## Production: HAProxy Configuration

HAProxy terminates TLS, selects the correct certificate via SNI, and forwards requests to the Go server over plain HTTP on localhost. The Go server reads the `Host` header (preserved by HAProxy by default) to dispatch to the correct module. No ACLs or Host-rewriting rules are needed.

### PEM file format

HAProxy expects a single combined PEM per certificate: the full certificate chain followed by the private key, all concatenated into one file.

```bash
# If you have separate files:
cat fullchain.pem privkey.pem > /etc/haproxy/certs/grocery.cmdhome.net.pem

# Repeat for each hostname:
cat fullchain.pem privkey.pem > /etc/haproxy/certs/todo.cmdhome.net.pem
cat fullchain.pem privkey.pem > /etc/haproxy/certs/slideshow.cmdhome.net.pem
cat fullchain.pem privkey.pem > /etc/haproxy/certs/menu.cmdhome.net.pem

chmod 600 /etc/haproxy/certs/*.pem
```

If your CA provides a single combined file (cert + chain + key), you can use it directly.

### /etc/haproxy/haproxy.cfg

```haproxy
global
    log /dev/log local0
    maxconn 4096
    user haproxy
    group haproxy
    daemon

defaults
    log     global
    mode    http
    option  httplog
    option  dontlognull
    timeout connect 5s
    timeout client  30s
    timeout server  30s

#
# Redirect plain HTTP to HTTPS
#
frontend http_front
    bind *:80
    redirect scheme https code 301

#
# HTTPS frontend — HAProxy picks the certificate by SNI
#
frontend https_front
    bind *:443 ssl \
        crt /etc/haproxy/certs/grocery.cmdhome.net.pem \
        crt /etc/haproxy/certs/todo.cmdhome.net.pem \
        crt /etc/haproxy/certs/slideshow.cmdhome.net.pem \
        crt /etc/haproxy/certs/menu.cmdhome.net.pem

    option forwardfor          # adds X-Forwarded-For with real client IP
    default_backend unified_webapp

#
# All four hostnames share the same Go backend.
# HAProxy preserves the Host header — the Go server uses it to dispatch.
#
backend unified_webapp
    server app 127.0.0.1:8080 check
```

### How SNI selection works

When a browser connects to `https://todo.cmdhome.net`, TLS negotiation happens before any HTTP is sent. HAProxy reads the SNI hostname from the TLS handshake and picks the matching certificate automatically. No ACL rules are needed. All four hostnames end up at the same backend; the Go server then reads the `Host` HTTP header to decide which module to invoke.

**Do not add** `http-request set-header Host` or `reqset-header` — those would overwrite the Host header and break dispatch.

### DNS

Point all four A records at the HAProxy host:

```
grocery.cmdhome.net    A  <haproxy-ip>
todo.cmdhome.net       A  <haproxy-ip>
slideshow.cmdhome.net  A  <haproxy-ip>
menu.cmdhome.net       A  <haproxy-ip>
```

### Verify

```bash
# Check HAProxy config is valid before reloading
haproxy -c -f /etc/haproxy/haproxy.cfg

# Reload without dropping connections
systemctl reload haproxy

# Confirm the correct cert is served for each hostname
echo | openssl s_client -connect <haproxy-ip>:443 -servername todo.cmdhome.net 2>/dev/null | openssl x509 -noout -subject
echo | openssl s_client -connect <haproxy-ip>:443 -servername grocery.cmdhome.net 2>/dev/null | openssl x509 -noout -subject
```

---

## Make Targets

| Target | Description |
|---|---|
| `make run` | Run the server with `~/.unified-webapp.json` |
| `make build` | Compile to `./unified-webapp` binary |
| `make test` | Run all tests with race detector |
| `make init-config` | Write default config to `~/.unified-webapp.json` |
| `make clean` | Remove compiled binary |
