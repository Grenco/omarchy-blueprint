package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/command"
)

// Explicit opt-in: only read-only user-manager catalogue/show commands run.
// No machine-state payload is logged or written to disk. The serial reference
// pins the pre-batching acquisition algorithm, using unchanged normalization.
func TestLiveServicesInventoryEquivalence(t *testing.T) {
	if os.Getenv("BLUEPRINT_SERVICES_LIVE_BENCHMARK") != "1" {
		t.Skip("set BLUEPRINT_SERVICES_LIVE_BENCHMARK=1 for read-only live equivalence/timing")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for run := 0; run < 5; run++ {
		serial := &inventoryTimingRunner{}
		start := time.Now()
		before, err := serialInventoryReference(ctx, Systemctl{Runner: serial})
		if err != nil {
			t.Fatal("serial user-manager observation failed")
		}
		t.Logf("serial run=%d wall=%s calls=%d bytes=%d units=%d", run, time.Since(start), serial.calls, serial.bytes, len(before))
		batched := &inventoryTimingRunner{reverseRecords: true}
		start = time.Now()
		after, err := (Systemctl{Runner: batched}).InspectUserUnits(ctx)
		if err != nil {
			var execution *command.RunError
			if errors.As(err, &execution) {
				t.Fatal("batched user-manager command failed (payload withheld)")
			}
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, after) {
			t.Fatal("normalized inventories differ (or live state changed); no payload logged")
		}
		t.Logf("batch64 reversed-records run=%d wall=%s calls=%d bytes=%d units=%d equal=true", run, time.Since(start), batched.calls, batched.bytes, len(after))
	}
}

type inventoryTimingRunner struct {
	calls, bytes   int
	reverseRecords bool
}

func (r *inventoryTimingRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	output, err := r.RunOutput(ctx, inspectionOutputLimit, name, args...)
	return string(output), err
}

func (r *inventoryTimingRunner) RunOutput(ctx context.Context, limit int64, name string, args ...string) ([]byte, error) {
	if name != "systemctl" || len(args) < 2 || args[0] != "--user" || (args[1] != "show" && args[1] != "list-unit-files" && args[1] != "list-units") {
		return nil, fmt.Errorf("live inventory benchmark refused non-read command")
	}
	output, err := (command.SystemRunner{}).RunOutput(ctx, limit, name, args...)
	r.calls++
	r.bytes += len(output)
	if err == nil && r.reverseRecords && args[1] == "show" {
		blocks := strings.Split(strings.TrimSpace(string(output)), "\n\n")
		for i, j := 0, len(blocks)-1; i < j; i, j = i+1, j-1 {
			blocks[i], blocks[j] = blocks[j], blocks[i]
		}
		output = []byte(strings.Join(blocks, "\n\n") + "\n")
	}
	return output, err
}

func serialInventoryReference(ctx context.Context, s Systemctl) ([]ObservedUnit, error) {
	files, err := s.inspect(ctx, "list-unit-files", "--all", "--no-legend", "--plain")
	if err != nil {
		return nil, err
	}
	loaded, err := s.inspect(ctx, "list-units", "--all", "--no-legend", "--plain")
	if err != nil {
		return nil, err
	}
	identities := map[string]string{}
	for _, line := range strings.Split(files, "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && strings.Contains(f[0], ".") {
			identities[f[0]] = f[1]
		}
	}
	for _, line := range strings.Split(loaded, "\n") {
		f := strings.Fields(line)
		if len(f) > 0 && f[0] == "●" {
			f = f[1:]
		}
		if len(f) > 0 && strings.Contains(f[0], ".") {
			if _, listed := identities[f[0]]; !listed {
				identities[f[0]] = ""
			}
		}
	}
	names := make([]string, 0, len(identities))
	for name := range identities {
		names = append(names, name)
	}
	sort.Strings(names)
	byID := map[string]ObservedUnit{}
	for _, name := range names {
		if strings.Contains(name, "@.") {
			unit := catalogTemplate(name, identities[name])
			byID[unit.Name] = unit
			continue
		}
		output, err := s.inspect(ctx, "show", "--all", "--no-pager", "--property="+strings.Replace(userUnitProperties, "Names,", "", 1), "--", name)
		if err != nil {
			return nil, err
		}
		unit := normalizeObservedUnit(name, identities[name], parseUnitProperties(output))
		byID[unit.Name] = unit
	}
	names = names[:0]
	for name := range byID {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]ObservedUnit, 0, len(names))
	for _, name := range names {
		result = append(result, byID[name])
	}
	return result, nil
}
