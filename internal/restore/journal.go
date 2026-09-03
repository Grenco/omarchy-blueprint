package restore

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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

// CreateBackup stores a copy of source beside the journal and returns its path.
func (j *Journal) CreateBackup(operation, source string) (string, error) {
	if operation == "" || filepath.Base(operation) != operation {
		return "", fmt.Errorf("invalid backup operation: %q", operation)
	}
	dir := strings.TrimSuffix(j.Path, ".jsonl") + ".backup"
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	info, err := os.Stat(source)
	if err != nil {
		return "", err
	}
	in, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer in.Close()
	path := filepath.Join(dir, operation)
	out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return "", err
	}
	_, copyErr := io.Copy(out, in)
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if syncErr != nil {
		return "", syncErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	directory, err := os.Open(dir)
	if err != nil {
		return "", err
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return "", err
	}
	return path, nil
}

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
