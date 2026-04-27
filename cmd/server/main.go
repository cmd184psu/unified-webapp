package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"

	"cmd184psu/unified-webapp/internal/grocery"
	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/platform/middleware"
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

	dispatch := newDispatcher()

	for host, module := range cfg.Routing {
		h, err := buildModule(module, cfg)
		if err != nil {
			log.Fatalf("build module %q for host %q: %v", module, host, err)
		}
		dispatch.register(host, h)
		log.Printf("registered %s → %s", host, module)
	}

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

func buildModule(module string, cfg *config.Config) (http.Handler, error) {
	switch module {
	case "grocery":
		return grocery.Build(cfg.Grocery)
	default:
		return nil, fmt.Errorf("unknown module %q", module)
	}
}
