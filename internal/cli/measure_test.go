package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"time"

	"sathiraumesh/power-meter/internal/devices/cpu"
)

// fakeMonitor returns ErrFirstSample, then the given samples in order.
type fakeMonitor struct {
	samples []cpu.Sample
	calls   int
}

func (f *fakeMonitor) Refresh() (cpu.Sample, error) {
	f.calls++
	if f.calls == 1 {
		return cpu.Sample{}, cpu.ErrFirstSample
	}
	return f.samples[f.calls-2], nil
}

// runMeasure runs "power-meter measure <args>" with a fake monitor and no
// real waiting, and returns what it printed and the intervals waited.
// If ctrlCAfter >= 0, the wait after that many samples acts like Ctrl+C.
func runMeasure(t *testing.T, mon monitor, monErr error, ctrlCAfter int, args ...string) (string, []time.Duration, error) {
	t.Helper()
	oldNew, oldWait := newMonitor, wait
	var waited []time.Duration
	newMonitor = func() (monitor, error) { return mon, monErr }
	wait = func(_ context.Context, d time.Duration) bool {
		if ctrlCAfter >= 0 && len(waited) == ctrlCAfter {
			return false
		}
		waited = append(waited, d)
		return true
	}

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs(append([]string{"measure"}, args...))
	t.Cleanup(func() {
		newMonitor, wait = oldNew, oldWait
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
		if c, _, err := rootCmd.Find([]string{"measure"}); err == nil {
			c.Flags().Set("interval", "5s")
			c.Flags().Set("count", "0")
		}
	})
	err := rootCmd.Execute()
	return buf.String(), waited, err
}

func sample(ratio float64, pkg, core cpu.ZoneUsage) cpu.Sample {
	pkg.Name, core.Name, core.Parent = "package-0", "core", "intel-rapl:0"
	return cpu.Sample{
		Time:       time.Date(2026, 11, 18, 12, 0, 5, 0, time.UTC),
		Interval:   2 * time.Second,
		UsageRatio: ratio,
		Zones:      []cpu.ZoneUsage{pkg, core},
	}
}

func TestMeasureCmd(t *testing.T) {
	mon := &fakeMonitor{samples: []cpu.Sample{
		sample(0.25,
			cpu.ZoneUsage{Power: 10, ActivePower: 2.5, IdlePower: 7.5, ActiveEnergyTotal: 5, IdleEnergyTotal: 15, EnergyTotal: 20},
			cpu.ZoneUsage{Power: 4, ActivePower: 1, IdlePower: 3, ActiveEnergyTotal: 2, IdleEnergyTotal: 6, EnergyTotal: 8}),
		sample(1,
			cpu.ZoneUsage{Power: 80, ActivePower: 80, IdlePower: 0, ActiveEnergyTotal: 85, IdleEnergyTotal: 15, EnergyTotal: 100},
			cpu.ZoneUsage{Power: 30, ActivePower: 30, IdlePower: 0, ActiveEnergyTotal: 32, IdleEnergyTotal: 6, EnergyTotal: 38}),
	}}

	out, waited, err := runMeasure(t, mon, nil, -1, "--interval", "2s", "--count", "2")
	if err != nil {
		t.Fatal(err)
	}
	if mon.calls != 3 {
		t.Errorf("Refresh called %d times, want 3 (baseline + 2)", mon.calls)
	}
	if len(waited) != 2 || waited[0] != 2*time.Second {
		t.Errorf("waited %v, want [2s 2s]", waited)
	}
	for _, want := range []string{
		"12:00:05  interval 2s  CPU usage 25.0%",
		"ZONE", "POWER", "ACTIVE", "IDLE", "ACTIVE TOTAL", "IDLE TOTAL", "TOTAL",
		"package-0", "10.00 W", "2.50 W", "7.50 W", "5.00 J", "15.00 J", "20.00 J",
		"└ core", "4.00 W", "1.00 W", "3.00 W",
		"CPU usage 100.0%", "80.00 W", "85.00 J", "100.00 J",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "sudo") {
		t.Errorf("all zones readable, but output mentions sudo:\n%s", out)
	}
}

func TestMeasureCmdErrors(t *testing.T) {
	s := sample(0.5, cpu.ZoneUsage{}, cpu.ZoneUsage{})
	s.Zones[0].Err = fmt.Errorf("open: %w", fs.ErrPermission)
	s.Zones[1].Err = errors.New("bad value")

	out, _, err := runMeasure(t, &fakeMonitor{samples: []cpu.Sample{s}}, nil, -1, "--count", "1")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"permission denied", "error: bad value", "Run with sudo"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestMeasureCmdNoMeters(t *testing.T) {
	out, _, err := runMeasure(t, nil, cpu.ErrNoMeters, -1, "--count", "1")
	if err != nil {
		t.Fatalf("no meters should not be an error, got %v", err)
	}
	if !strings.Contains(out, "No CPU power meters found.") {
		t.Errorf("got %q", out)
	}
}

func TestMeasureCmdIntervalSeconds(t *testing.T) {
	for v, want := range map[string]time.Duration{
		"1":     time.Second,
		"0.5":   500 * time.Millisecond,
		"2s":    2 * time.Second,
		"500ms": 500 * time.Millisecond,
	} {
		mon := &fakeMonitor{samples: []cpu.Sample{{}, {}}}
		_, waited, err := runMeasure(t, mon, nil, -1, "--interval", v, "--count", "1")
		if err != nil {
			t.Fatalf("--interval %s: %v", v, err)
		}
		if len(waited) == 0 || waited[0] != want {
			t.Errorf("--interval %s: waited %v, want %v", v, waited, want)
		}
	}
}

func TestMeasureCmdBadInterval(t *testing.T) {
	for _, v := range []string{"0", "0s", "-1", "-1s", "soon"} {
		if _, _, err := runMeasure(t, &fakeMonitor{}, nil, -1, "--interval", v, "--count", "1"); err == nil {
			t.Errorf("--interval %s: expected error, got nil", v)
		}
	}
}

func TestMeasureCmdStopsOnCtrlC(t *testing.T) {
	// --count 0 runs forever; Ctrl+C after 1 sample must exit cleanly.
	mon := &fakeMonitor{samples: []cpu.Sample{sample(0.1, cpu.ZoneUsage{Power: 5}, cpu.ZoneUsage{Power: 1})}}
	out, _, err := runMeasure(t, mon, nil, 1, "--count", "0")
	if err != nil {
		t.Fatalf("Ctrl+C should exit cleanly, got %v", err)
	}
	if strings.Count(out, "CPU usage") != 1 {
		t.Errorf("want exactly 1 sample printed, got:\n%s", out)
	}
}
