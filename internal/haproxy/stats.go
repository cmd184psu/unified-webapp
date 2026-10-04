package haproxy

// Read-only HAProxy stats client (FRD §7, §12). The app dials the stats socket
// directly as its own user (the generated `stats socket` line hands it to that
// user at level user) and only ever sends `show stat` and `show info`.

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// statsTimeout bounds one socket exchange.
const statsTimeout = 3 * time.Second

// ErrStatsUnavailable means the stats socket cannot be used right now (missing
// file, refused connection, timeout). Its message is plain, UI-ready text.
type ErrStatsUnavailable struct {
	Reason string
	Err    error
}

func (e *ErrStatsUnavailable) Error() string {
	return "HAProxy statistics are not available: " + e.Reason + ". HAProxy may not have been applied yet, or it is stopped."
}
func (e *ErrStatsUnavailable) Unwrap() error { return e.Err }

// StatRow is one line of `show stat`.
type StatRow struct {
	Proxy       string `json:"proxy"`
	Server      string `json:"server"`
	Kind        string `json:"kind"` // frontend | backend | server | listener
	Status      string `json:"status"`
	SessionsCur int64  `json:"sessionsCur"`
	SessionRate int64  `json:"sessionRate"`
	BytesIn     int64  `json:"bytesIn"`
	BytesOut    int64  `json:"bytesOut"`
	CheckStatus string `json:"checkStatus"`
	LastCheck   string `json:"lastCheck"`
}

// StatsInfo is the subset of `show info` the UI and verifier use.
type StatsInfo struct {
	Version   string `json:"version"`
	UptimeSec int64  `json:"uptimeSec"`
	CurrConns int64  `json:"currConns"`
	Pid       int64  `json:"pid"`
}

var statKinds = map[string]string{"0": "frontend", "1": "backend", "2": "server", "3": "listener"}

// statsQuery sends one read-only command and returns the reply. Any other
// command is refused before anything touches the socket.
func statsQuery(ctx context.Context, path string, timeout time.Duration, cmd string) (string, error) {
	if cmd != "show stat" && cmd != "show info" {
		return "", fmt.Errorf("stats command %q is not allowed (read-only client)", cmd)
	}
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		reason := "cannot connect to the stats socket"
		if errors.Is(err, net.ErrClosed) || strings.Contains(err.Error(), "no such file") {
			reason = "the stats socket does not exist"
		} else if strings.Contains(err.Error(), "refused") {
			reason = "the stats socket refused the connection"
		} else if strings.Contains(err.Error(), "permission denied") {
			reason = "permission denied on the stats socket"
		}
		return "", &ErrStatsUnavailable{Reason: reason, Err: err}
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	// Closing on ctx cancel unblocks a read that would otherwise wait for the deadline.
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	if _, err := conn.Write([]byte(cmd + "\n")); err != nil {
		return "", &ErrStatsUnavailable{Reason: "the stats socket did not accept the request", Err: err}
	}
	b, err := io.ReadAll(conn)
	if err != nil {
		return "", &ErrStatsUnavailable{Reason: "the stats socket did not answer in time", Err: err}
	}
	return string(b), nil
}

// FetchStats reads `show info` and `show stat` from the socket at path.
func FetchStats(ctx context.Context, path string, timeout time.Duration) (StatsInfo, []StatRow, error) {
	infoText, err := statsQuery(ctx, path, timeout, "show info")
	if err != nil {
		return StatsInfo{}, nil, err
	}
	statText, err := statsQuery(ctx, path, timeout, "show stat")
	if err != nil {
		return StatsInfo{}, nil, err
	}
	rows, err := ParseStatCSV(statText)
	if err != nil {
		return StatsInfo{}, nil, &ErrStatsUnavailable{Reason: "the stats socket returned an unexpected reply", Err: err}
	}
	return ParseStatInfo(infoText), rows, nil
}

// ParseStatCSV parses `show stat` output by column name (never by offset).
func ParseStatCSV(text string) ([]StatRow, error) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "#") {
		return nil, errors.New("show stat output has no # header line")
	}
	text = strings.TrimSpace(strings.TrimPrefix(text, "#"))
	r := csv.NewReader(strings.NewReader(text))
	r.FieldsPerRecord = -1
	recs, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("show stat: %w", err)
	}
	if len(recs) == 0 {
		return nil, errors.New("show stat output is empty")
	}
	idx := map[string]int{}
	for i, name := range recs[0] {
		idx[strings.TrimSpace(name)] = i
	}
	get := func(rec []string, col string) string {
		if i, ok := idx[col]; ok && i < len(rec) {
			return rec[i]
		}
		return ""
	}
	num := func(rec []string, col string) int64 {
		n, _ := strconv.ParseInt(get(rec, col), 10, 64)
		return n
	}
	rows := []StatRow{}
	for _, rec := range recs[1:] {
		if len(rec) == 0 || (len(rec) == 1 && strings.TrimSpace(rec[0]) == "") {
			continue
		}
		kind, ok := statKinds[get(rec, "type")]
		if !ok {
			continue
		}
		row := StatRow{
			Proxy: get(rec, "pxname"), Kind: kind, Status: get(rec, "status"),
			SessionsCur: num(rec, "scur"), SessionRate: num(rec, "rate"),
			BytesIn: num(rec, "bin"), BytesOut: num(rec, "bout"),
			CheckStatus: get(rec, "check_status"), LastCheck: get(rec, "last_chk"),
		}
		if kind == "server" || kind == "listener" {
			row.Server = get(rec, "svname")
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// ParseStatInfo parses the `Key: value` lines of `show info`.
func ParseStatInfo(text string) StatsInfo {
	var in StatsInfo
	for _, line := range strings.Split(text, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch strings.TrimSpace(k) {
		case "Version":
			in.Version = v
		case "Uptime_sec":
			in.UptimeSec, _ = strconv.ParseInt(v, 10, 64)
		case "CurrConns":
			in.CurrConns, _ = strconv.ParseInt(v, 10, 64)
		case "Pid":
			in.Pid, _ = strconv.ParseInt(v, 10, 64)
		}
	}
	return in
}

// StatsVerifier is the post-reload Verifier (FRD §7 step 5): the service must
// be active AND the stats socket must answer `show info` with a version. When
// the socket is not available (first run before any config, macOS pre-bootstrap)
// it falls back to the service-active check alone and logs that fact.
type StatsVerifier struct {
	Driver  Driver
	Log     *OpLog
	Timeout time.Duration
}

func (v StatsVerifier) Verify(ctx context.Context) error {
	if err := (statusVerifier{driver: v.Driver}).Verify(ctx); err != nil {
		return err
	}
	timeout := v.Timeout
	if timeout <= 0 {
		timeout = statsTimeout
	}
	text, err := statsQuery(ctx, v.Driver.StatsSocketPath(), timeout, "show info")
	if err != nil {
		var ue *ErrStatsUnavailable
		if errors.As(err, &ue) {
			if v.Log != nil {
				v.Log.Addf("verify: stats socket not available (%s); verified by the service-active check only", ue.Reason)
			}
			return nil
		}
		return err
	}
	if ParseStatInfo(text).Version == "" {
		return errors.New("stats socket answered but reported no HAProxy version")
	}
	return nil
}
