package main

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"

	"cmd184psu/unified-webapp/internal/admin"
	"cmd184psu/unified-webapp/internal/grocery"
	"cmd184psu/unified-webapp/internal/menuserver"
	"cmd184psu/unified-webapp/internal/multissh"
	"cmd184psu/unified-webapp/internal/obsidianoid"
	"cmd184psu/unified-webapp/internal/todo"
	"cmd184psu/unified-webapp/internal/platform/auth"
	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/platform/middleware"
	"cmd184psu/unified-webapp/internal/slideshow"
)

// Dispatcher routes incoming requests to the correct module handler based on
// the Host header. HAProxy is expected to forward the original Host unchanged.
// closers holds the shutdown hooks of modules whose Build started background
// goroutines (their handlers implement io.Closer); Close runs them all.
type Dispatcher struct {
	handlers map[string]http.Handler
	closers  []io.Closer
}

func newDispatcher() *Dispatcher {
	return &Dispatcher{handlers: make(map[string]http.Handler)}
}

func (d *Dispatcher) register(host string, h http.Handler) {
	d.handlers[host] = h
}

// Close stops every module background goroutine the dispatcher's modules
// started. Production never calls it (modules live for the process); tests
// use it so the package can run under a goleak gate.
func (d *Dispatcher) Close() {
	for _, c := range d.closers {
		_ = c.Close()
	}
}

func (d *Dispatcher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := strings.SplitN(r.Host, ":", 2)[0]
	h, ok := d.handlers[host]
	if !ok {
		http.NotFound(w, r)
		return
	}
	h.ServeHTTP(w, r)
}

func main() {
	cfgPath  := flag.String("config",      config.DefaultConfigPath, "Path to config JSON")
	flagPort := flag.Int("port",           0,  "Override port")
	flagCert := flag.String("tls-cert",   "", "Override TLS cert path")
	flagKey  := flag.String("tls-key",    "", "Override TLS key path")
	flagInit := flag.Bool("init-config", false, "Write default config and exit")
	flagHashPin := flag.Bool("hash-pin", false, "Read a PIN from stdin, print its bcrypt hash, and exit")
	flagGenAPIKey := flag.Bool("gen-api-key", false, "Generate a new API key and its config hash, print both, and exit")
	flag.Parse()

	if *flagInit {
		if err := config.WriteDefault(*cfgPath); err != nil {
			log.Fatalf("write default config: %v", err)
		}
		fmt.Printf("Default config written to %s\n", *cfgPath)
		return
	}

	if *flagHashPin {
		hashPINAndExit()
		return
	}

	if *flagGenAPIKey {
		genAPIKeyAndExit()
		return
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	switch cfg.Server.OriginCheck {
	case "", "enforce", "log", "off":
	default:
		log.Fatalf("config: server.origin_check %q is invalid; must be one of \"\", \"enforce\", \"log\", \"off\"", cfg.Server.OriginCheck)
	}
	if *flagPort != 0  { cfg.Port    = *flagPort }
	if *flagCert != "" { cfg.TLSCert = *flagCert }
	if *flagKey  != "" { cfg.TLSKey  = *flagKey  }

	adminRouted := adminIsRouted(cfg.Routing)
	svc, err := auth.FromConfig(cfg.Auth, knownModules, adminRouted)
	if err != nil {
		// Errors from the auth package are already "auth:"-prefixed.
		log.Fatalf("%v", err)
	}

	dispatch := buildDispatcher(cfg, svc)

	addr    := fmt.Sprintf("0.0.0.0:%d", cfg.Port)
	handler := middleware.Wrap(middleware.OriginCheck(cfg.Server.OriginCheck, dispatch))
	useTLS  := cfg.TLSCert != "" && cfg.TLSKey != ""

	srv := newServer(addr, handler)

	if useTLS {
		log.Printf("unified-webapp → https://%s (TLS)", addr)
		log.Fatalf("HTTPS error: %v", srv.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey))
	} else {
		log.Printf("unified-webapp → http://%s", addr)
		log.Fatalf("HTTP error: %v", srv.ListenAndServe())
	}
}

// hashPINAndExit reads a PIN from stdin, prints its bcrypt hash to stdout,
// and exits. When stdin is a terminal, the PIN is prompted for on stderr
// with no echo (term.ReadPassword); otherwise (piped/redirected input) a
// single line is read from stdin instead, so scripted invocations such as
// `go run ./cmd/server -hash-pin <<< "1234"` work without a TTY.
func hashPINAndExit() {
	var pin string
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		fmt.Fprint(os.Stderr, "PIN: ")
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			log.Fatalf("hash-pin: reading PIN: %v", err)
		}
		pin = string(b)
	} else {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			log.Fatalf("hash-pin: reading PIN: %v", err)
		}
		pin = line
	}

	pin = strings.TrimSpace(pin)
	if pin == "" {
		log.Fatalf("hash-pin: PIN must not be empty")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.DefaultCost)
	if err != nil {
		log.Fatalf("hash-pin: %v", err)
	}
	fmt.Println(string(hash))
}

// genAPIKeyAndExit generates a new 32-byte random API key, encodes it as
// base64url without padding (the text a client will send in the
// Authorization/X-API-Key header), and prints it alongside its
// "sha256:<hex>" config hash. The hash is computed over the encoded key
// string itself -- exactly what a client sends -- so it matches what
// auth.checkAPIKey computes from the header value.
func genAPIKeyAndExit() {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		log.Fatalf("gen-api-key: generating key: %v", err)
	}
	key := base64.RawURLEncoding.EncodeToString(raw)

	sum := sha256.Sum256([]byte(key))
	hash := "sha256:" + hex.EncodeToString(sum[:])

	fmt.Printf("key:  %s\n", key)
	fmt.Printf("hash: %s\n", hash)
}

// newServer builds the http.Server used to serve the app.
//
// ReadTimeout and WriteTimeout are deliberately left at zero (no timeout):
// slideshow SSE streams and multissh WebSocket/terminal sessions are
// long-lived connections that can sit idle or stream for hours, and a
// nonzero WriteTimeout would kill every one of them mid-stream. IdleTimeout
// only bounds the time a keep-alive connection may sit between requests, and
// ReadHeaderTimeout bounds how long a client may take to send request
// headers, guarding against slowloris-style attacks without affecting
// long-lived request bodies/responses.
func newServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}

// buildDispatcher builds every routed module and returns the Host-header
// dispatcher for them.
//
// Each module is built once, however many hostnames route to it. Two entries
// pointing at the same module must share one handler: a second instance would
// carry its own copy of that module's state -- uploads staged through one
// hostname would be invisible through the other, and concurrent writes to the
// same data file would clobber each other.
//
// The same table records failures. A module that cannot build gets a handler
// that 503s with the reason, so one bad path takes down that module's
// hostnames and leaves the rest of the binary serving.
func buildDispatcher(cfg *config.Config, svc *auth.Service) *Dispatcher {
	dispatch := newDispatcher()
	built := make(map[string]http.Handler, len(cfg.Routing))
	for host, module := range cfg.Routing {
		h, ok := built[module]
		if !ok {
			hh, err := buildModule(module, cfg, svc)
			if err != nil {
				log.Printf("ERROR: module %q failed to build and will return 503 on every request: %v", module, err)
				h = unavailableHandler(module, err)
			} else {
				if c, ok := hh.(io.Closer); ok {
					dispatch.closers = append(dispatch.closers, c)
				}
				h = middleware.BodyLimit(limitFor(module, cfg), svc.Gate(module, hh))
			}
			built[module] = h
		}
		dispatch.register(host, h)
		log.Printf("registered ( http://%s:%d ) → %s", host, cfg.Port, module)
	}
	return dispatch
}

// defaultBodyLimit caps the request body of every module that has no larger
// need of its own.
const defaultBodyLimit int64 = 1 << 20 // 1 MiB

// limitFor returns the request-body ceiling for a module. multissh streams
// file uploads through the same body and enforces its own precise ceiling at
// cfg.Multissh.MaxUploadBytes; the extra 1 MiB headroom here covers the
// surrounding multipart framing so BodyLimit never clips a legitimate upload
// before multissh's own check gets to report it.
func limitFor(module string, cfg *config.Config) int64 {
	switch module {
	case "multissh":
		return cfg.Multissh.MaxUploadBytes + defaultBodyLimit
	case "admin":
		if cfg.Admin.MaxBodyBytes > 0 {
			return cfg.Admin.MaxBodyBytes
		}
	}
	return defaultBodyLimit
}

// knownModules is the buildModule universe -- exactly the module names the
// switch below handles. auth.FromConfig uses it to validate that every
// module named in auth.modules is one buildDispatcher can actually build.
var knownModules = []string{"grocery", "todo", "slideshow", "menuserver", "obsidianoid", "multissh", "admin"}

// adminIsRouted reports whether "admin" appears among routing's module
// values (config.Config.Routing / host_routing). Both main's boot-time
// auth.FromConfig call and buildModule's "admin" case need this, and they
// must agree, so it lives in one place.
func adminIsRouted(routing map[string]string) bool {
	for _, module := range routing {
		if module == "admin" {
			return true
		}
	}
	return false
}

func buildModule(module string, cfg *config.Config, svc *auth.Service) (http.Handler, error) {
	switch module {
	case "grocery":
		return grocery.Build(cfg.Grocery)
	case "todo":
		return todo.Build(cfg.Todo)
	case "slideshow":
		return slideshow.Build(cfg.Slideshow)
	case "menuserver":
		return menuserver.Build(cfg.Menuserver)
	case "obsidianoid":
		return obsidianoid.Build(cfg.Obsidianoid)
	case "multissh":
		return multissh.Build(cfg.Multissh)
	case "admin":
		return admin.Build(cfg, admin.Deps{
			Service:      svc,
			ConfigPath:   cfg.ConfigPath(),
			KnownModules: knownModules,
			AdminRouted:  adminIsRouted(cfg.Routing),
		})
	default:
		return nil, fmt.Errorf("unknown module %q", module)
	}
}

// unavailableHandler answers every request to a module that failed to build.
// The boot log is easy to miss once the binary comes up healthy, so the reason
// travels in the response body too -- whoever loads the page sees the cause.
func unavailableHandler(module string, cause error) http.Handler {
	msg := cause.Error()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":  "module unavailable",
			"module": module,
			"reason": msg,
		})
	})
}
