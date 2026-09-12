package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"

	"cmd184psu/unified-webapp/internal/grocery"
	"cmd184psu/unified-webapp/internal/menuserver"
	"cmd184psu/unified-webapp/internal/multissh"
	"cmd184psu/unified-webapp/internal/obsidianoid"
	"cmd184psu/unified-webapp/internal/todo"
	"cmd184psu/unified-webapp/internal/utuber"
	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/platform/middleware"
	"cmd184psu/unified-webapp/internal/slideshow"
)

// Dispatcher routes incoming requests to the correct module handler based on
// the Host header. HAProxy is expected to forward the original Host unchanged.
type Dispatcher struct {
	handlers map[string]http.Handler
}

func newDispatcher() *Dispatcher {
	return &Dispatcher{handlers: make(map[string]http.Handler)}
}

func (d *Dispatcher) register(host string, h http.Handler) {
	d.handlers[host] = h
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
	flag.Parse()

	if *flagInit {
		if err := config.WriteDefault(*cfgPath); err != nil {
			log.Fatalf("write default config: %v", err)
		}
		fmt.Printf("Default config written to %s\n", *cfgPath)
		return
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if *flagPort != 0  { cfg.Port    = *flagPort }
	if *flagCert != "" { cfg.TLSCert = *flagCert }
	if *flagKey  != "" { cfg.TLSKey  = *flagKey  }

	dispatch := buildDispatcher(cfg)

	addr    := fmt.Sprintf("0.0.0.0:%d", cfg.Port)
	handler := middleware.Wrap(dispatch)
	useTLS  := cfg.TLSCert != "" && cfg.TLSKey != ""

	if useTLS {
		log.Printf("unified-webapp → https://%s (TLS)", addr)
		log.Fatalf("HTTPS error: %v", http.ListenAndServeTLS(addr, cfg.TLSCert, cfg.TLSKey, handler))
	} else {
		log.Printf("unified-webapp → http://%s", addr)
		log.Fatalf("HTTP error: %v", http.ListenAndServe(addr, handler))
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
func buildDispatcher(cfg *config.Config) *Dispatcher {
	dispatch := newDispatcher()
	built := make(map[string]http.Handler, len(cfg.Routing))
	for host, module := range cfg.Routing {
		h, ok := built[module]
		if !ok {
			var err error
			h, err = buildModule(module, cfg)
			if err != nil {
				log.Printf("ERROR: module %q failed to build and will return 503 on every request: %v", module, err)
				h = unavailableHandler(module, err)
			}
			built[module] = h
		}
		dispatch.register(host, h)
		log.Printf("registered ( http://%s:%d ) → %s", host, cfg.Port, module)
	}
	return dispatch
}

func buildModule(module string, cfg *config.Config) (http.Handler, error) {
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
	case "utuber":
		return utuber.Build(cfg.Utuber)
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
