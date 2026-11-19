package cpu

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "stat")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadTimes(t *testing.T) {
	// user nice system idle iowait irq softirq steal guest guest_nice
	p := writeFile(t, "cpu  100 10 50 800 20 5 5 10 99 99\ncpu0 1 2 3 4 5 6 7 8 9 10\n")
	got, err := readTimes(p)
	if err != nil {
		t.Fatal(err)
	}
	// Total = 100+10+50+800+20+5+5+10 = 1000 (guest fields ignored)
	// Idle  = 800+20 = 820
	if got.Total != 1000 || got.Idle != 820 {
		t.Errorf("got %+v, want Total 1000, Idle 820", got)
	}
}

func TestReadTimesOldKernel(t *testing.T) {
	// Very old kernels only have the first 4 or 7 fields.
	p := writeFile(t, "cpu  100 0 100 800 0 0 0\n")
	got, err := readTimes(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 1000 || got.Idle != 800 {
		t.Errorf("got %+v, want Total 1000, Idle 800", got)
	}
}

func TestReadTimesInvalid(t *testing.T) {
	for name, content := range map[string]string{
		"empty":      "",
		"wrong line": "intr 1 2 3 4 5\n",
		"bad number": "cpu  1 2 x 4 5\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := readTimes(writeFile(t, content)); err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
	if _, err := readTimes(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("missing file: expected error, got nil")
	}
}

func TestUsageRatio(t *testing.T) {
	tests := []struct {
		name      string
		prev, cur Times
		want      float64
	}{
		{"all idle", Times{Idle: 0, Total: 0}, Times{Idle: 100, Total: 100}, 0},
		{"all busy", Times{Idle: 50, Total: 100}, Times{Idle: 50, Total: 200}, 1},
		{"quarter busy", Times{Idle: 0, Total: 0}, Times{Idle: 75, Total: 100}, 0.25},
		{"no time passed", Times{Idle: 5, Total: 10}, Times{Idle: 5, Total: 10}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := UsageRatio(tt.prev, tt.cur); math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
