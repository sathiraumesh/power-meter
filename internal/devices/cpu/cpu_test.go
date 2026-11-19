package cpu

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"
)

func write(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

type row struct {
	Source   Source
	ID       string
	Name     string
	Parent   string
	Readable bool
}

func rows(meters []Meter) []row {
	var out []row
	for _, m := range meters {
		out = append(out, row{m.Source, m.ID, m.Name, m.Parent, m.Readable})
	}
	return out
}

func TestCheck(t *testing.T) {
	raplRoot := t.TempDir()
	write(t, raplRoot, map[string]string{
		"intel-rapl:0/name":                  "package-0",
		"intel-rapl:0/max_energy_range_uj":   "1000",
		"intel-rapl:0/energy_uj":             "500",
		"intel-rapl:0:0/name":                "core",
		"intel-rapl:0:0/max_energy_range_uj": "1000",
		// no energy_uj: simulates a counter we cannot read
	})

	got, err := check(raplRoot)
	if err != nil {
		t.Fatal(err)
	}
	want := []row{
		{SourceRAPL, "intel-rapl:0", "package-0", "", true},
		{SourceRAPL, "intel-rapl:0:0", "core", "intel-rapl:0", false},
	}
	if !reflect.DeepEqual(rows(got), want) {
		t.Errorf("got  %+v\nwant %+v", rows(got), want)
	}
	if got[1].Err == nil {
		t.Error("unreadable meter should carry its error")
	}
}

func TestCheckNothing(t *testing.T) {
	if _, err := check(filepath.Join(t.TempDir(), "missing")); !errors.Is(err, ErrNoMeters) {
		t.Errorf("got %v, want ErrNoMeters", err)
	}
}

func TestRead(t *testing.T) {
	raplRoot := t.TempDir()
	write(t, raplRoot, map[string]string{
		"intel-rapl:0/name":                  "package-0",
		"intel-rapl:0/max_energy_range_uj":   "1000",
		"intel-rapl:0/energy_uj":             "500",
		"intel-rapl:0:0/name":                "core",
		"intel-rapl:0:0/max_energy_range_uj": "1000",
		// no energy_uj: simulates a counter we cannot read
	})

	got, err := read(raplRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d readings, want 2", len(got))
	}

	pkg := got[0]
	if pkg.EnergyUJ != 500 || pkg.Err != nil {
		t.Errorf("package-0: got %d µJ, err %v; want 500, nil", pkg.EnergyUJ, pkg.Err)
	}
	if want := filepath.Join(raplRoot, "intel-rapl:0", "energy_uj"); pkg.File != want {
		t.Errorf("package-0 file: got %q, want %q", pkg.File, want)
	}

	core := got[1]
	if core.Err == nil {
		t.Error("core: expected read error, got nil")
	}
	if core.Parent != "intel-rapl:0" {
		t.Errorf("core parent: got %q, want intel-rapl:0", core.Parent)
	}
}

func TestReadNothing(t *testing.T) {
	if _, err := read(filepath.Join(t.TempDir(), "missing")); !errors.Is(err, ErrNoMeters) {
		t.Errorf("got %v, want ErrNoMeters", err)
	}
}

// fakeMachine is a fake RAPL tree + /proc/stat + clock that tests advance.
type fakeMachine struct {
	t        *testing.T
	raplRoot string
	procStat string
	clock    time.Time
}

func newFakeMachine(t *testing.T) *fakeMachine {
	t.Helper()
	n := &fakeMachine{t: t, raplRoot: t.TempDir(), procStat: filepath.Join(t.TempDir(), "stat"), clock: time.Unix(0, 0)}
	write(t, n.raplRoot, map[string]string{
		"intel-rapl:0/name":                  "package-0",
		"intel-rapl:0/max_energy_range_uj":   "100000000", // wraps at 100 J
		"intel-rapl:0:0/name":                "core",
		"intel-rapl:0:0/max_energy_range_uj": "100000000",
	})
	return n
}

// set writes the counters (µJ) and /proc/stat (busy, idle ticks), then
// advances the clock by d.
func (n *fakeMachine) set(d time.Duration, pkgUJ, coreUJ, busy, idle uint64) {
	n.t.Helper()
	write(n.t, n.raplRoot, map[string]string{
		"intel-rapl:0/energy_uj":   strconv.FormatUint(pkgUJ, 10),
		"intel-rapl:0:0/energy_uj": strconv.FormatUint(coreUJ, 10),
	})
	stat := "cpu  " + strconv.FormatUint(busy, 10) + " 0 0 " + strconv.FormatUint(idle, 10) + " 0 0 0 0\n"
	if err := os.WriteFile(n.procStat, []byte(stat), 0o644); err != nil {
		n.t.Fatal(err)
	}
	n.clock = n.clock.Add(d)
}

func (n *fakeMachine) now() time.Time { return n.clock }

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestMonitor(t *testing.T) {
	n := newFakeMachine(t)
	m, err := newMonitor(n.raplRoot, n.procStat, n.now)
	if err != nil {
		t.Fatal(err)
	}

	// Baseline.
	n.set(0, 10_000_000, 4_000_000, 0, 0)
	if _, err := m.Refresh(); !errors.Is(err, ErrFirstSample) {
		t.Fatalf("first refresh: got %v, want ErrFirstSample", err)
	}

	// Interval 1: 2 s, 25% busy. package +20 J, core +8 J.
	n.set(2*time.Second, 30_000_000, 12_000_000, 25, 75)
	s, err := m.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	if s.Interval != 2*time.Second || !near(s.UsageRatio, 0.25) {
		t.Fatalf("interval %v ratio %v, want 2s 0.25", s.Interval, s.UsageRatio)
	}
	pkg, core := s.Zones[0], s.Zones[1]
	// package: 20 J → active 5 J, idle 15 J; 10 W → active 2.5 W, idle 7.5 W
	check := func(name string, got, want float64) {
		t.Helper()
		if !near(got, want) {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
	check("pkg ActiveEnergy", pkg.ActiveEnergy, 5)
	check("pkg IdleEnergy", pkg.IdleEnergy, 15)
	check("pkg Power", pkg.Power, 10)
	check("pkg ActivePower", pkg.ActivePower, 2.5)
	check("pkg IdlePower", pkg.IdlePower, 7.5)
	check("core ActiveEnergy", core.ActiveEnergy, 2)
	check("core IdleEnergy", core.IdleEnergy, 6)
	if core.Parent != "intel-rapl:0" {
		t.Errorf("core parent %q", core.Parent)
	}

	// Interval 2: 1 s, 100% busy. package wraps: (100-30)+10 = 80 J.
	n.set(time.Second, 10_000_000, 22_000_000, 125, 75)
	s, err = m.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	pkg = s.Zones[0]
	check("ratio 2", s.UsageRatio, 1)
	check("pkg ActiveEnergy 2", pkg.ActiveEnergy, 80)
	check("pkg IdleEnergy 2", pkg.IdleEnergy, 0)
	// Running totals: active 5+80, idle 15+0.
	check("pkg ActiveEnergyTotal", pkg.ActiveEnergyTotal, 85)
	check("pkg IdleEnergyTotal", pkg.IdleEnergyTotal, 15)
}

func TestMonitorUnreadableZone(t *testing.T) {
	n := newFakeMachine(t)
	m, err := newMonitor(n.raplRoot, n.procStat, n.now)
	if err != nil {
		t.Fatal(err)
	}
	coreFile := filepath.Join(n.raplRoot, "intel-rapl:0:0", "energy_uj")

	n.set(0, 0, 0, 0, 0)
	os.Remove(coreFile)
	m.Refresh()

	n.set(time.Second, 4_000_000, 0, 50, 50)
	os.Remove(coreFile)
	s, err := m.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	if s.Zones[0].Err != nil || !near(s.Zones[0].Power, 4) {
		t.Errorf("package: %+v, want 4 W", s.Zones[0])
	}
	if s.Zones[1].Err == nil {
		t.Error("core: expected error, got nil")
	}

	// Core becomes readable: first interval is only its baseline.
	n.set(time.Second, 8_000_000, 1_000_000, 100, 100)
	s, _ = m.Refresh()
	if s.Zones[1].Err != nil || s.Zones[1].Power != 0 {
		t.Errorf("core baseline: %+v, want 0 W no error", s.Zones[1])
	}
	n.set(time.Second, 12_000_000, 3_000_000, 150, 150)
	s, _ = m.Refresh()
	if !near(s.Zones[1].Power, 2) {
		t.Errorf("core: %v W, want 2", s.Zones[1].Power)
	}
}

func TestMonitorNoMeters(t *testing.T) {
	if _, err := newMonitor(filepath.Join(t.TempDir(), "missing"), "/proc/stat", time.Now); !errors.Is(err, ErrNoMeters) {
		t.Errorf("got %v, want ErrNoMeters", err)
	}
}
