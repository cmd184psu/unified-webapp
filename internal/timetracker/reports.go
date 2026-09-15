package timetracker

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	_ "modernc.org/sqlite"
)

// TimeBlock is one selected 15-minute slot in the time selector.
type TimeBlock struct {
	Hour    int `json:"hour"`
	Quarter int `json:"quarter"`
}

// Report is one customer's report for one day, including the time-selector
// state so rewinding restores the whole view.
type Report struct {
	CustomerName string      `json:"customerName"`
	Date         string      `json:"date"` // YYYY-MM-DD
	Body         string      `json:"body"`
	TimeBlocks   []TimeBlock `json:"timeBlocks"`
}

// ErrInvalidReport marks a report whose customer name or date fails
// validation.
var ErrInvalidReport = errors.New("invalid report")

var dateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// ReportStore persists per-customer per-day reports in SQLite, keyed by
// (customer_name, report_date).
type ReportStore struct {
	db *sql.DB
}

// NewReportStore opens (creating if needed) the SQLite database at path and
// ensures the schema exists.
func NewReportStore(path string) (*ReportStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening report db: %w", err)
	}
	// The pure-Go driver serializes writes itself, but a single connection
	// avoids SQLITE_BUSY between concurrent request goroutines entirely.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS reports (
			customer_name TEXT NOT NULL,
			report_date   TEXT NOT NULL,
			body          TEXT NOT NULL DEFAULT '',
			time_blocks   TEXT NOT NULL DEFAULT '[]',
			updated_at    TEXT NOT NULL,
			PRIMARY KEY (customer_name, report_date)
		)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrating report db: %w", err)
	}
	return &ReportStore{db: db}, nil
}

// Close releases the database handle.
func (r *ReportStore) Close() error { return r.db.Close() }

func validateKey(customer, date string) error {
	if customer == "" || !dateRe.MatchString(date) {
		return ErrInvalidReport
	}
	return nil
}

// Get returns the report for (customer, date). exists is false when no row
// is stored; the returned Report still carries the key and empty content so
// callers can render a blank editor.
func (r *ReportStore) Get(customer, date string) (rep Report, exists bool, err error) {
	rep = Report{CustomerName: customer, Date: date, TimeBlocks: []TimeBlock{}}
	if err := validateKey(customer, date); err != nil {
		return rep, false, err
	}
	var blocksJSON string
	row := r.db.QueryRow(
		`SELECT body, time_blocks FROM reports WHERE customer_name=? AND report_date=?`,
		customer, date)
	switch err := row.Scan(&rep.Body, &blocksJSON); {
	case errors.Is(err, sql.ErrNoRows):
		return rep, false, nil
	case err != nil:
		return rep, false, err
	}
	if err := json.Unmarshal([]byte(blocksJSON), &rep.TimeBlocks); err != nil {
		// A corrupt blob loses the selection, not the report text.
		rep.TimeBlocks = []TimeBlock{}
	}
	return rep, true, nil
}

// Neighbors returns the nearest stored report dates strictly before and
// after date for customer. Either is "" when none exists on that side.
func (r *ReportStore) Neighbors(customer, date string) (prev, next string, err error) {
	if err := validateKey(customer, date); err != nil {
		return "", "", err
	}
	scan := func(query string, dest *string) error {
		err := r.db.QueryRow(query, customer, date).Scan(dest)
		if errors.Is(err, sql.ErrNoRows) {
			*dest = ""
			return nil
		}
		return err
	}
	if err := scan(`SELECT report_date FROM reports WHERE customer_name=? AND report_date<? ORDER BY report_date DESC LIMIT 1`, &prev); err != nil {
		return "", "", err
	}
	if err := scan(`SELECT report_date FROM reports WHERE customer_name=? AND report_date>? ORDER BY report_date ASC LIMIT 1`, &next); err != nil {
		return "", "", err
	}
	return prev, next, nil
}

// Save upserts a report. A report with an empty body and no time blocks is
// deleted instead, so abandoned blanks never clutter the rewind history.
func (r *ReportStore) Save(rep Report) error {
	if err := validateKey(rep.CustomerName, rep.Date); err != nil {
		return err
	}
	if rep.Body == "" && len(rep.TimeBlocks) == 0 {
		_, err := r.db.Exec(
			`DELETE FROM reports WHERE customer_name=? AND report_date=?`,
			rep.CustomerName, rep.Date)
		return err
	}
	blocks := rep.TimeBlocks
	if blocks == nil {
		blocks = []TimeBlock{}
	}
	blocksJSON, err := json.Marshal(blocks)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(`
		INSERT INTO reports (customer_name, report_date, body, time_blocks, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(customer_name, report_date) DO UPDATE SET
			body=excluded.body, time_blocks=excluded.time_blocks, updated_at=excluded.updated_at`,
		rep.CustomerName, rep.Date, rep.Body, string(blocksJSON),
		time.Now().UTC().Format(time.RFC3339))
	return err
}

// RenameCustomer moves every report from oldName to newName so a customer
// rename keeps its report history. If both names hold a report for the same
// date, the renamed customer's row wins (OR REPLACE).
func (r *ReportStore) RenameCustomer(oldName, newName string) error {
	if oldName == "" || newName == "" || oldName == newName {
		return nil
	}
	_, err := r.db.Exec(
		`UPDATE OR REPLACE reports SET customer_name=? WHERE customer_name=?`,
		newName, oldName)
	return err
}
