package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	DefaultAddr        = "127.0.0.1:5757"
	DefaultDataDirName = ".botchat"
)

type Config struct {
	Addr    string        `json:"addr"`
	DataDir string        `json:"dataDir"`
	Auth    AuthConfig    `json:"auth"`
	Chat    ChatConfig    `json:"chat"`
	Webhook WebhookConfig `json:"webhook"`
}

type ChatConfig struct {
	// MessageRetentionDays is the message TTL. Messages older than this are
	// purged. 0 applies the default (7); a negative value disables the TTL.
	MessageRetentionDays int `json:"messageRetentionDays"`
	// TaskIDPrefix is prepended to task sequence numbers (e.g. "TASK" → TASK-42).
	// Empty applies the default ("TASK").
	TaskIDPrefix string `json:"taskIdPrefix"`
	// ExclusiveTaskPerAssignee, when true, forbids creating a task for an
	// assignee that already has an active (non-terminal) task.
	ExclusiveTaskPerAssignee bool `json:"exclusiveTaskPerAssignee"`
	// CallsignLeaseMinutes marks a callsign "stale" once it has been idle this
	// long. Advisory only (surfaced to operators; never blocks posting). 0
	// disables staleness reporting.
	CallsignLeaseMinutes int `json:"callsignLeaseMinutes"`
	// AssignmentAckMinutes flags a task as "ack overdue" once it has sat in the
	// assigned state without being acknowledged/started this long. Advisory
	// only. 0 disables the warning.
	AssignmentAckMinutes int `json:"assignmentAckMinutes"`
}

// AuthConfig keeps the mode field so an old standalone config still parses.
// The server ignores it. Human sign-in is the platform gate.
type AuthConfig struct {
	Mode string `json:"mode"`
}

func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ".botchat.json"
	}
	return filepath.Join(home, ".botchat.json")
}

func Load(path string) (Config, error) {
	var c Config

	if path != "" {
		data, err := os.ReadFile(path)
		switch {
		case err == nil:
			if err := json.Unmarshal(data, &c); err != nil {
				return Config{}, fmt.Errorf("config: parse %q: %w", path, err)
			}
		case errors.Is(err, fs.ErrNotExist):
		default:
			return Config{}, fmt.Errorf("config: read %q: %w", path, err)
		}
	}

	if v := strings.TrimSpace(os.Getenv("BOTCHAT_ADDR")); v != "" {
		c.Addr = v
	}
	if v := strings.TrimSpace(os.Getenv("BOTCHAT_DATA_DIR")); v != "" {
		c.DataDir = v
	}

	if err := Finalize(&c); err != nil {
		return Config{}, err
	}
	return c, nil
}

// Finalize fills defaults and expands a leading ~ in dataDir. The module
// calls this on the botchat section of ~/.puma-unified.json. Load calls it
// too, after file and environment values are applied. Addr is kept for a
// pasted standalone config and is not used to listen.
func Finalize(c *Config) error {
	if c.DataDir != "" {
		expanded, err := expandHome(c.DataDir)
		if err != nil {
			return err
		}
		c.DataDir = expanded
	}
	applyDefaults(c)
	return nil
}

func expandHome(path string) (string, error) {
	if path == "" || path[0] != '~' {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", fmt.Errorf("config: expand %q: %w", path, err)
	}
	return filepath.Join(home, path[1:]), nil
}

func applyDefaults(c *Config) {
	if c.Addr == "" {
		c.Addr = DefaultAddr
	}
	if c.DataDir == "" {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			c.DataDir = filepath.Join(home, DefaultDataDirName)
		} else {
			c.DataDir = DefaultDataDirName
		}
	}
	if c.Chat.MessageRetentionDays == 0 {
		c.Chat.MessageRetentionDays = 7
	}
	applyAuthDefaults(&c.Auth)
}

func applyAuthDefaults(a *AuthConfig) {
	if a.Mode == "" {
		a.Mode = "none"
	}
}

// WebhookConfig configures the outbound signed webhook BotChat fires on
// actionable task state changes (task_assigned, task_answered, task_done,
// task_accepted, task_ready). The receiver verifies the HMAC-SHA256
// signature in X-BotChat-Signature against the raw body. Leave URL empty
// to disable webhooks.
type WebhookConfig struct {
	URL    string `json:"url"`
	Secret string `json:"secret"`
}
