package timetracker

import (
	"net/http"
	"os"
	"path/filepath"

	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/platform/static"
)

// closer pairs the module's router with the report database connection
// NewReportStore opens, so the process owner can release it (io.Closer is
// the dispatcher's optional shutdown hook). NewReportStore starts a
// database/sql connection-pool goroutine that only a Close call stops.
type closer struct {
	http.Handler
	reports *ReportStore
}

func (c *closer) Close() error {
	return c.reports.Close()
}

// Build returns a ready-to-use http.Handler for the timetracker module.
// The caller is responsible for wrapping it with middleware (e.g. CORS).
// The handler implements io.Closer to release the report database
// connection pool NewReportStore opens.
func Build(cfg config.TimetrackerConfig) (http.Handler, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.DataFile), 0750); err != nil {
		return nil, err
	}
	s, err := New(cfg.DataFile)
	if err != nil {
		return nil, err
	}
	reportDB := cfg.ReportDB
	if reportDB == "" {
		reportDB = filepath.Join(filepath.Dir(cfg.DataFile), "timetracker-reports.db")
	}
	reports, err := NewReportStore(reportDB)
	if err != nil {
		return nil, err
	}
	h := NewHandler(s, reports)

	mux := http.NewServeMux()
	h.Register(mux)
	mux.Handle("/", static.NewHandler(cfg.StaticDir))

	return &closer{Handler: mux, reports: reports}, nil
}
