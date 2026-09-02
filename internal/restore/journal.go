package restore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Event struct {
	Time      time.Time `json:"time"`
	Type      string    `json:"type"`
	Operation string    `json:"operation,omitempty"`
	Message   string    `json:"message,omitempty"`
}

type Journal struct {
	file *os.File
	Path string
}

func NewJournal(stateHome string, now time.Time) (*Journal, error) {
	dir := filepath.Join(stateHome, "omarchy-blueprint", "restores")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "restore-"+now.UTC().Format("20060102T150405.000000000Z")+".jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	return &Journal{file: f, Path: path}, nil
}

func (j *Journal) Write(event Event) error {
	if err := json.NewEncoder(j.file).Encode(event); err != nil {
		return fmt.Errorf("write journal: %w", err)
	}
	return j.file.Sync()
}

func (j *Journal) Close() error { return j.file.Close() }

func StateHome() (string, error) {
	if path := os.Getenv("XDG_STATE_HOME"); path != "" {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state"), nil
}
