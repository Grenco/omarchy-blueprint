package services

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// This double models systemctl's process boundary, not the inventory parser.
// It deliberately returns records in reverse operand order.
type batchRunner struct {
	files, loaded string
	records       map[string]string
	shows         [][]string
	calls         int
	probe         func(context.Context, []string) (string, error)
}

func (r *batchRunner) Run(ctx context.Context, command string, args ...string) (string, error) {
	r.calls++
	if command != "systemctl" || len(args) < 2 || args[0] != "--user" {
		return "", fmt.Errorf("unexpected command")
	}
	switch args[1] {
	case "list-unit-files":
		return r.files, nil
	case "list-units":
		return r.loaded, nil
	case "show":
		separator := -1
		for i, arg := range args {
			if arg == "--" {
				separator = i
				break
			}
		}
		if separator < 0 {
			return "", fmt.Errorf("missing operand separator")
		}
		names := append([]string(nil), args[separator+1:]...)
		r.shows = append(r.shows, names)
		if r.probe != nil {
			return r.probe(ctx, names)
		}
		var records []string
		for i := len(names) - 1; i >= 0; i-- {
			if strings.Contains(names[i], "@.") {
				return "", fmt.Errorf("naked template queried")
			}
			record, ok := r.records[names[i]]
			if !ok {
				return "", fmt.Errorf("unknown operand")
			}
			records = append(records, strings.TrimSpace(record))
		}
		return strings.Join(records, "\n\n") + "\n", nil
	default:
		return "", fmt.Errorf("mutation or unexpected verb")
	}
}

func TestInspectUserUnitsCommandCountBounded(t *testing.T) {
	for _, size := range []int{0, 1, 63, 64, 65, 512, 513} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			r := &batchRunner{records: map[string]string{}, files: "backup@.service static -\n"}
			for i := 0; i < size; i++ {
				name := fmt.Sprintf("unit-%03d.service", i)
				r.files += name + " disabled -\n"
				r.records[name] = "Id=" + name + "\nNames=" + name + "\nUnitFileState=disabled\n"
			}
			got, err := (Systemctl{Runner: r}).InspectUserUnits(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != size+1 {
				t.Fatalf("units=%d want=%d", len(got), size+1)
			}
			want := 2 + (size+63)/64
			if r.calls != want {
				t.Fatalf("process count=%d want=%d; per-unit shows regressed", r.calls, want)
			}
			for _, batch := range r.shows {
				if len(batch) == 0 || len(batch) > 64 {
					t.Fatalf("unbounded batch: %d", len(batch))
				}
			}
		})
	}
}

func TestInspectUserUnitsBatchesPreserveNormalization(t *testing.T) {
	r := &batchRunner{
		files:  "worker@.service static -\nworker@home.service enabled -\nmask.service masked -\nlinked.service linked -\nruntime.service enabled-runtime -\ngenerated.service generated -\n",
		loaded: "● transient.service loaded active running\nloaded.target loaded inactive dead\n",
		records: map[string]string{
			"worker@home.service": "Id=worker@home.service\nNames=worker@home.service\nUnitFileState=enabled\nLoadState=loaded\nActiveState=active\nType=oneshot\nResult=success\nFragmentPath=/home/test/.config/systemd/user/worker@.service\nDropInPaths=/b.conf /a.conf /a.conf\nRequires=z.target a.target\nWants=a.target b.target\nBindsTo=c.target\nPartOf=d.target\nTriggers=e.target\nTriggeredBy=f.timer e.target\n",
			"mask.service":        "Id=mask.service\nNames=mask.service\nUnitFileState=masked\nFragmentPath=/dev/null\n",
			"linked.service":      "Id=linked.service\nNames=linked.service\nUnitFileState=linked\nFragmentPath=/home/test/linked.service\nSourcePath=/opt/source.service\n",
			"runtime.service":     "Id=runtime.service\nNames=runtime.service\nUnitFileState=enabled-runtime\nFragmentPath=/home/test/runtime.service\n",
			"generated.service":   "Id=generated.service\nNames=generated.service\nUnitFileState=generated\nFragmentPath=/run/user/1000/systemd/generator/generated.service\n",
			"transient.service":   "Id=transient.service\nNames=transient.service\nTransient=yes\nUnitFileState=transient\nFragmentPath=/run/user/1000/systemd/transient/transient.service\n",
			"loaded.target":       "Id=loaded.target\nNames=loaded.target\nLoadState=loaded\n",
		},
	}
	got, err := (Systemctl{Runner: r}).InspectUserUnits(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	byID := map[string]ObservedUnit{}
	for _, unit := range got {
		names = append(names, unit.Name)
		byID[unit.Name] = unit
	}
	wantNames := []string{"generated.service", "linked.service", "loaded.target", "mask.service", "runtime.service", "transient.service", "worker@.service", "worker@home.service"}
	if !reflect.DeepEqual(names, wantNames) {
		t.Fatalf("unstable order: %v", names)
	}
	u := byID["worker@home.service"]
	if !u.Persistent || !u.TopologyKnown || !u.ObservedActive || u.InstanceOf != "worker@.service" || u.ServiceType != "oneshot" || u.ExecutionResult != "success" || !reflect.DeepEqual(u.DropInPaths, []string{"/a.conf", "/b.conf"}) || !reflect.DeepEqual(u.RelatedUnits, []string{"a.target", "b.target", "c.target", "d.target", "e.target", "f.timer", "z.target"}) || !reflect.DeepEqual(u.TriggeredBy, []string{"e.target", "f.timer"}) {
		t.Fatalf("topology changed: %+v", u)
	}
	if !byID["mask.service"].Persistent || byID["linked.service"].LinkedSource != "/opt/source.service" || !byID["runtime.service"].Runtime || !byID["generated.service"].Generated || !byID["transient.service"].Transient || byID["loaded.target"].TopologyKnown || byID["worker@.service"].TopologyKnown || !byID["worker@.service"].Template {
		t.Fatal("catalogue/source semantics changed")
	}
	if byID["loaded.target"].DropInPaths == nil || byID["loaded.target"].RelatedUnits == nil {
		t.Fatal("empty slice semantics changed")
	}
	if r.calls != 3 {
		t.Fatalf("calls=%d want=3", r.calls)
	}
}

func TestInspectUserUnitsMapsCanonicalAliasesAndCatalogueFallback(t *testing.T) {
	r := &batchRunner{files: "a-alias.service enabled -\nz-real.service disabled -\n", records: map[string]string{
		"a-alias.service": "Id=z-real.service\nNames=z-real.service a-alias.service\nUnitFileState=\n",
		"z-real.service":  "Id=z-real.service\nNames=a-alias.service z-real.service\nUnitFileState=\n",
	}}
	got, err := (Systemctl{Runner: r}).InspectUserUnits(context.Background())
	if err != nil || len(got) != 1 || got[0].Name != "z-real.service" || got[0].RawUnitFileState != "disabled" {
		t.Fatalf("sorted operand fallback lost: %+v err=%v", got, err)
	}
	if r.calls != 3 {
		t.Fatalf("alias observations were not batched: %d", r.calls)
	}
	// systemd may coalesce aliases into a single canonical record.
	r.probe = func(context.Context, []string) (string, error) {
		return "Id=z-real.service\nNames=a-alias.service z-real.service\n", nil
	}
	got, err = (Systemctl{Runner: r}).InspectUserUnits(context.Background())
	if err != nil || len(got) != 1 || got[0].RawUnitFileState != "disabled" {
		t.Fatalf("coalesced aliases lost: %+v err=%v", got, err)
	}
}

func TestInspectUserUnitsRejectsInvalidIdentityEvidence(t *testing.T) {
	for _, output := range []string{
		"LoadState=loaded\n", "Id=\n", "Id=one.service\nId=one.service\n", "Id=bad name.service\n",
		"Id=/bad.service\n", "Id=nosuffix\n", "Id=one.service\nmalformed\n",
		"Id=other.service\nNames=other.service\n", "Id=one.service\n\nId=extra.service\n",
		"Id=one.service\nNames=shared.service\n\nId=other.service\nNames=one.service shared.service\n",
	} {
		t.Run(fmt.Sprintf("%q", output), func(t *testing.T) {
			r := &batchRunner{files: "one.service disabled -\n", probe: func(context.Context, []string) (string, error) { return output, nil }}
			got, err := (Systemctl{Runner: r}).InspectUserUnits(context.Background())
			if err == nil || got != nil {
				t.Fatalf("invalid inventory accepted: %+v err=%v", got, err)
			}
		})
	}
}

func TestDuplicateAliasRecordsCompareNormalizedFacts(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		for _, across := range []bool{false, true} {
			t.Run(fmt.Sprintf("conflict=%t/across=%t", conflict, across), func(t *testing.T) {
				r := &batchRunner{files: "a-alias.service enabled -\nz-real.service disabled -\n", records: map[string]string{}}
				r.records["a-alias.service"] = "Id=z-real.service\nNames=a-alias.service z-real.service\nRequires=b.target a.target\nUnitFileState=\n"
				r.records["z-real.service"] = "Id=z-real.service\nNames=z-real.service a-alias.service\nRequires=a.target b.target\nUnitFileState=\n"
				if conflict {
					r.records["z-real.service"] += "ActiveState=active\n"
				}
				if across {
					for i := 0; i < 64; i++ {
						n := fmt.Sprintf("m-%02d.service", i)
						r.files += n + " static -\n"
						r.records[n] = "Id=" + n + "\n"
					}
				}
				got, err := (Systemctl{Runner: r}).InspectUserUnits(context.Background())
				if conflict {
					if err == nil || got != nil {
						t.Fatal("conflicting canonical facts accepted")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				last := got[len(got)-1]
				if last.Name != "z-real.service" || last.RawUnitFileState != "disabled" || !reflect.DeepEqual(last.RelatedUnits, []string{"a.target", "b.target"}) {
					t.Fatalf("normalized alias facts changed: %+v", last)
				}
			})
		}
	}
}

func TestInspectUserUnitsBatchFailureIsFatal(t *testing.T) {
	for _, kind := range []string{"command", "partial", "limit"} {
		t.Run(kind, func(t *testing.T) {
			r := &batchRunner{}
			for i := 0; i < 65; i++ {
				r.files += fmt.Sprintf("u-%03d.service disabled -\n", i)
			}
			r.probe = func(context.Context, []string) (string, error) {
				switch kind {
				case "command":
					return "Id=u-000.service\n", errors.New("failed batch")
				case "limit":
					return strings.Repeat("x", inspectionOutputLimit+1), nil
				default:
					return "Id=u-000.service\n", nil
				}
			}
			got, err := (Systemctl{Runner: r}).InspectUserUnits(context.Background())
			if err == nil || got != nil || len(r.shows) != 1 {
				t.Fatalf("partial inventory or later probes: units=%d err=%v shows=%d", len(got), err, len(r.shows))
			}
		})
	}
}

func TestInspectUserUnitsBatchCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	r := &batchRunner{}
	for i := 0; i < 65; i++ {
		r.files += fmt.Sprintf("u-%03d.service disabled -\n", i)
	}
	r.probe = func(ctx context.Context, _ []string) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	}
	done := make(chan error, 1)
	go func() {
		got, err := (Systemctl{Runner: r}).InspectUserUnits(ctx)
		if got != nil {
			err = fmt.Errorf("partial inventory returned")
		}
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	if len(r.shows) != 1 {
		t.Fatal("continued probes after cancellation")
	}
}

func TestInspectUserUnitsAlreadyCanceledDoesNotProbe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := &batchRunner{}
	got, err := (Systemctl{Runner: r}).InspectUserUnits(ctx)
	if !errors.Is(err, context.Canceled) || got != nil || r.calls != 0 {
		t.Fatalf("canceled observation probed: calls=%d err=%v", r.calls, err)
	}
}

func TestInspectUserUnitsCancellationRejectsLateSuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := &batchRunner{files: "one.service static -\n"}
	r.probe = func(context.Context, []string) (string, error) { cancel(); return "Id=one.service\n", nil }
	got, err := (Systemctl{Runner: r}).InspectUserUnits(ctx)
	if !errors.Is(err, context.Canceled) || got != nil {
		t.Fatalf("late successful result accepted: %+v err=%v", got, err)
	}
}

func TestInspectUserUnitsRejectsAliasCollisionAcrossBatches(t *testing.T) {
	r := &batchRunner{records: map[string]string{}}
	for i := 0; i < 65; i++ {
		n := fmt.Sprintf("unit-%03d.service", i)
		r.files += n + " static -\n"
		r.records[n] = "Id=" + n + "\n"
	}
	r.records["unit-000.service"] += "Names=shared.service\n"
	r.records["unit-064.service"] += "Names=shared.service\n"
	got, err := (Systemctl{Runner: r}).InspectUserUnits(context.Background())
	if err == nil || got != nil {
		t.Fatal("cross-batch identity collision accepted")
	}
}

func TestParseUnitRecordsBoundariesAndIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, output string
		count        int
		fail         bool
	}{
		{"multiple", "Id=one.service\nNames=one.service\n\n \t\nId=two.target\n\n", 2, false},
		{"escaped", "Id=dev-disk-by\\x20id.device\nNames=dev-disk-by\\x20id.device\n", 1, false},
		{"quoted-escaped", "Id=dev-disk-by\\x20id.device\nNames=\"dev-disk-by\\\\x20id.device\" alias.device\n", 1, false},
		{"unclosed-quote", "Id=one.service\nNames=\"one.service\n", 0, true},
		{"leading-hyphen", "Id=-valid.scope\n", 1, false},
		{"empty", "\n \n", 0, false},
		{"missing", "LoadState=loaded\n", 0, true},
		{"empty-id", "Id=\n", 0, true},
		{"duplicate-id", "Id=one.service\nId=one.service\n", 0, true},
		{"duplicate-names", "Id=one.service\nNames=one.service\nNames=one.service\n", 0, true},
		{"malformed", "Id=one.service\nnot-a-property\n", 0, true},
		{"invalid-alias", "Id=one.service\nNames=../two.service\n", 0, true},
		{"invalid-escape", "Id=bad\\xGG.device\n", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseUnitRecords(tc.output)
			if (err != nil) != tc.fail || (!tc.fail && len(got) != tc.count) {
				t.Fatalf("records=%d error=%v", len(got), err)
			}
		})
	}
}

func TestMapUnitRecordsReorderedAliases(t *testing.T) {
	records, err := parseUnitRecords("Id=z.service\nNames=z.service alias.service\nUnitFileState=disabled\n\nId=a.target\nNames=a.target\n")
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := mapUnitRecords([]string{"a.target", "alias.service", "z.service"}, records)
	if err != nil || mapped["alias.service"]["Id"] != "z.service" || mapped["a.target"]["Id"] != "a.target" || len(mapped) != 3 {
		t.Fatalf("identity mapping failed: %v", err)
	}
	for _, names := range [][]string{{"a.target"}, {"missing.service"}} {
		if _, err := mapUnitRecords(names, records); err == nil {
			t.Fatalf("accepted unrelated/missing record for %v", names)
		}
	}
}

func TestMapUnitRecordsQuotedEscapedAlias(t *testing.T) {
	records, err := parseUnitRecords("Id=real.device\nNames=real.device \"dev-disk-by\\\\x20id.device\"\n")
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := mapUnitRecords([]string{`dev-disk-by\x20id.device`}, records)
	if err != nil || mapped[`dev-disk-by\x20id.device`]["Id"] != "real.device" {
		t.Fatalf("quoted escaped alias lost: %v", err)
	}
}
