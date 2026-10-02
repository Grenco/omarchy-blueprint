// Package diagnostics provides opt-in, request-local stderr timings. It never
// records arguments, outputs, environment values, paths, or machine identities.
package diagnostics

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type contextKey struct{}
type timing struct {
	count        int
	elapsed, max time.Duration
}
type collector struct {
	mu                  sync.Mutex
	once                sync.Once
	closed              bool
	start               time.Time
	out                 io.Writer
	commands, providers map[string]timing
}

func noop() {}

func WithObservation(ctx context.Context, out io.Writer) (context.Context, func()) {
	if os.Getenv("BLUEPRINT_DEBUG_OBSERVATION") != "1" || out == nil {
		return ctx, noop
	}
	c := &collector{start: time.Now(), out: out, commands: map[string]timing{}, providers: map[string]timing{}}
	return context.WithValue(ctx, contextKey{}, c), func() {
		c.once.Do(func() {
			c.mu.Lock()
			defer c.mu.Unlock()
			c.closed = true
			for _, key := range sortedKeys(c.providers) {
				s := c.providers[key]
				fmt.Fprintf(c.out, "provider=%s observations=%d elapsed=%.3fms\n", key, s.count, float64(s.elapsed)/float64(time.Millisecond))
			}
			for _, key := range sortedKeys(c.commands) {
				s := c.commands[key]
				parts := strings.SplitN(key, "/", 2)
				fmt.Fprintf(c.out, "family=%s verb=%s calls=%d elapsed=%.3fms max=%.3fms\n", parts[0], parts[1], s.count, float64(s.elapsed)/float64(time.Millisecond), float64(s.max)/float64(time.Millisecond))
			}
			fmt.Fprintf(c.out, "read-cycle total=%.3fms\n", float64(time.Since(c.start))/float64(time.Millisecond))
		})
	}
}
func sortedKeys(m map[string]timing) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
func active(ctx context.Context) *collector { c, _ := ctx.Value(contextKey{}).(*collector); return c }
func (c *collector) record(observation bool, key string, start time.Time) {
	elapsed := time.Since(start)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	target := c.commands
	if observation {
		target = c.providers
	}
	s := target[key]
	s.count++
	s.elapsed += elapsed
	s.max = max(s.max, elapsed)
	target[key] = s
}

// StartCommand returns nil on the disabled path, without formatting or allocation.
func StartCommand(ctx context.Context, name string, args []string) func() {
	c := active(ctx)
	if c == nil {
		return nil
	}
	key := family(name) + "/" + verb(args)
	start := time.Now()
	return func() { c.record(false, key, start) }
}
func StartObservation(ctx context.Context, provider string) func() {
	c := active(ctx)
	if c == nil {
		return nil
	}
	switch provider {
	case "services", "packages", "metadata":
	default:
		provider = "other"
	}
	start := time.Now()
	return func() { c.record(true, provider, start) }
}
func family(name string) string {
	name = filepath.Base(name)
	switch name {
	case "systemctl", "systemd-analyze", "pacman", "pacman-conf", "mise", "git", "omarchy", "sh", "cat", "tailscale":
		return name
	}
	if strings.HasPrefix(name, "omarchy-") {
		return "omarchy"
	}
	return "other"
}
func verb(args []string) string {
	for _, arg := range args {
		switch arg {
		case "list-unit-files", "list-units", "show", "is-enabled", "status", "verify", "version", "ls", "current", "list", "--repo-list", "DBPath", "-Qq", "-Qqe", "-Qqen", "-Qqem", "-Q", "rev-parse", "diff", "config", "check-ignore", "log", "remote":
			return arg
		}
	}
	return "other"
}
