package models

import (
	"encoding/json"
	"time"
)

type LaneConfig struct {
	Name  string `json:"name"`
	Width int    `json:"width"`
}

type Config struct {
	Port      int          `json:"port"`
	UIEnabled bool         `json:"ui_enabled"`
	DBPath    string       `json:"db_path"`
	Lanes     []LaneConfig `json:"lanes"`
}

type Task struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	LaneName        string `json:"lane_name"`
	Enabled         bool   `json:"enabled"`
	Paused          bool   `json:"paused"`
	CooldownSeconds int    `json:"cooldown_seconds"`
	Repeat          bool   `json:"repeat"`
	Command         string `json:"command"`
	Position        int    `json:"position"`
	Sudo            bool   `json:"sudo"`
	OutputFile      string `json:"output_file,omitempty"`
	// Kind, Label, Payload and PayloadVersion identify and drive a
	// Go-function ("func") task (empty Kind = ordinary shell task).
	Kind           string          `json:"kind,omitempty"`
	Label          string          `json:"label,omitempty"`
	Payload        json.RawMessage `json:"payload,omitempty"`
	PayloadVersion int             `json:"payload_version,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type TaskExecution struct {
	ID              int64      `json:"id"`
	TaskID          int64      `json:"task_id"`
	TaskName        string     `json:"task_name,omitempty"`
	ScheduledAt     *time.Time `json:"scheduled_at"`
	StartedAt       *time.Time `json:"started_at"`
	FinishedAt      *time.Time `json:"finished_at"`
	Status          string     `json:"status"`
	ErrorMessage    *string    `json:"error_message"`
	WorkerID        *string    `json:"worker_id"`
	DurationMs      *int64     `json:"duration_ms"`
	ScheduleDelayMs *int64     `json:"schedule_delay_ms"`
	// Pid and Suspended are in-memory only (from worker.ProcessRegistry),
	// merged into the API response for a running execution — never persisted,
	// since the process (and any suspended state) is gone on restart.
	Pid       *int `json:"pid,omitempty"`
	Suspended bool `json:"suspended,omitempty"`
	// ProgressPct/ProgressLabel are the last persisted progress report for a
	// func-task execution (nil = never reported or indeterminate). Result is
	// the func callback's JSON result, set only on success.
	ProgressPct   *int            `json:"progress_pct,omitempty"`
	ProgressLabel string          `json:"progress_label,omitempty"`
	Result        json.RawMessage `json:"result,omitempty"`
}

// Progress is a single progress report: Pct 0-100, or nil for indeterminate.
type Progress struct {
	Pct   *int
	Label string
}

type Lane struct {
	Name      string     `json:"name"`
	Width     int        `json:"width"`
	Paused    bool       `json:"paused"`
	PausedAt  *time.Time `json:"paused_at,omitempty"`
	PausedBy  string     `json:"paused_by,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	// Owner, Hidden and RetentionDays support a module-owned lane (e.g.
	// a module): Owner names the owning module ("" = a regular, config/UI-managed
	// lane), Hidden excludes it from every TM HTTP route/board event, and
	// RetentionDays (0 = never) drives the func-task pruner.
	Owner         string `json:"owner,omitempty"`
	Hidden        bool   `json:"-"`
	RetentionDays int    `json:"retention_days,omitempty"`
}

type LaneStatus struct {
	Lane
	RunningCount int `json:"running_count"`
}

type MetricSummary struct {
	TaskName string `json:"task_name"`
	// Kind is set (and TaskName holds the kind, not the task name) when this
	// row aggregates every func task of that kind in the lane (P17); empty
	// for a shell task's per-task row.
	Kind          string     `json:"kind,omitempty"`
	GroupName     string     `json:"group_name"`
	SuccessCount  int        `json:"success_count"`
	FailedCount   int        `json:"failed_count"`
	CanceledCount int        `json:"canceled_count"`
	AvgDurationMs *float64   `json:"avg_duration_ms"`
	MinDurationMs *int64     `json:"min_duration_ms"`
	MaxDurationMs *int64     `json:"max_duration_ms"`
	AvgDelayMs    *float64   `json:"avg_schedule_delay_ms"`
	LastExecution *time.Time `json:"last_execution"`
}
