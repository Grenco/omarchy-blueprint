package omarchy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/Grenco/omarchy-blueprint/internal/command"
)

const (
	preinstallCommandLookup = "command -v omarchy-remove-preinstalls"
	preinstallMarkerCheck   = `[ -f "$HOME/.local/state/omarchy/preinstalls-removed" ]`
)

// PreinstallState describes Omarchy's supported preinstall intent and the
// physical presence of every package in the catalogue shipped by the
// installed Omarchy version.
type PreinstallState struct {
	RemovedAll bool
	Items      map[string]bool
}

// DetectPreinstalls derives the catalogue from Omarchy's authoritative
// remove flow rather than carrying a version-sensitive copy in Blueprint.
func DetectPreinstalls(ctx context.Context, runner command.Runner) (PreinstallState, error) {
	path, err := runner.Run(ctx, "sh", "-c", preinstallCommandLookup)
	if err != nil {
		return PreinstallState{}, fmt.Errorf("locate Omarchy preinstall catalogue: %w", err)
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return PreinstallState{}, errors.New("locate Omarchy preinstall catalogue: empty command path")
	}
	script, err := runner.Run(ctx, "cat", path)
	if err != nil {
		return PreinstallState{}, fmt.Errorf("read Omarchy preinstall catalogue: %w", err)
	}
	items, err := parsePreinstallCatalogue(script)
	if err != nil {
		return PreinstallState{}, err
	}

	removedAll, err := commandSucceededOrAbsent(ctx, runner, "sh", "-c", preinstallMarkerCheck)
	if err != nil {
		return PreinstallState{}, fmt.Errorf("detect Omarchy preinstall opt-out: %w", err)
	}
	state := PreinstallState{RemovedAll: removedAll, Items: make(map[string]bool, len(items))}
	for _, item := range items {
		present, err := commandSucceededOrAbsent(ctx, runner, "pacman", "-Q", item)
		if err != nil {
			return PreinstallState{}, fmt.Errorf("detect Omarchy preinstall %q: %w", item, err)
		}
		state.Items[item] = present
	}
	return state, nil
}

func commandSucceededOrAbsent(ctx context.Context, runner command.Runner, name string, args ...string) (bool, error) {
	_, err := runner.Run(ctx, name, args...)
	if err == nil {
		return true, nil
	}
	var runErr *command.RunError
	if errors.As(err, &runErr) && runErr.ExitCode == 1 {
		return false, nil
	}
	return false, err
}

func parsePreinstallCatalogue(script string) ([]string, error) {
	scanner := bufio.NewScanner(strings.NewReader(script))
	inBlock := false
	seen := map[string]bool{}
	var items []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !inBlock {
			if !strings.HasPrefix(line, "omarchy-pkg-drop") {
				continue
			}
			inBlock = true
			line = strings.TrimSpace(strings.TrimPrefix(line, "omarchy-pkg-drop"))
		}
		continued := strings.HasSuffix(line, "\\")
		line = strings.TrimSpace(strings.TrimSuffix(line, "\\"))
		for _, item := range strings.Fields(line) {
			if !validPreinstallID(item) {
				return nil, fmt.Errorf("parse Omarchy preinstall catalogue: invalid package ID %q", item)
			}
			if !seen[item] {
				seen[item] = true
				items = append(items, item)
			}
		}
		if !continued {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("parse Omarchy preinstall catalogue: %w", err)
	}
	if !inBlock || len(items) == 0 {
		return nil, errors.New("parse Omarchy preinstall catalogue: omarchy-pkg-drop package block not found")
	}
	sort.Strings(items)
	return items, nil
}

func validPreinstallID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("@._+-", r) {
			continue
		}
		return false
	}
	return true
}
