package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"

	"sathiraumesh/power-meter/internal/devices/cpu"
)

// runRead runs "power-meter read" with readCPU replaced by fake, and
// returns what it printed.
func runRead(t *testing.T, fake func() ([]cpu.Reading, error)) (string, error) {
	t.Helper()
	old := readCPU
	readCPU = fake
	t.Cleanup(func() { readCPU = old })

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"read"})
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})
	err := rootCmd.Execute()
	return buf.String(), err
}

func TestReadCmd(t *testing.T) {
	got, err := runRead(t, func() ([]cpu.Reading, error) {
		return []cpu.Reading{
			{Name: "package-0", File: "/sys/class/powercap/intel-rapl:0/energy_uj", EnergyUJ: 48213907761},
			{Name: "core", Parent: "intel-rapl:0", File: "/sys/class/powercap/intel-rapl:0:0/energy_uj", EnergyUJ: 1234},
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"NAME", "FILE", "ENERGY (µJ)",
		"package-0", "/sys/class/powercap/intel-rapl:0/energy_uj", "48213907761",
		"└ core", "/sys/class/powercap/intel-rapl:0:0/energy_uj", "1234",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "sudo") {
		t.Errorf("all files were readable, but output mentions sudo:\n%s", got)
	}
}

func TestReadCmdErrors(t *testing.T) {
	got, err := runRead(t, func() ([]cpu.Reading, error) {
		return []cpu.Reading{
			{Name: "package-0", Err: fmt.Errorf("open: %w", fs.ErrPermission)},
			{Name: "core", Parent: "intel-rapl:0", Err: errors.New("bad value")},
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"permission denied", // permission error shown simply
		"error: bad value",  // any other error shown as is
		"Run with sudo",     // hint because something failed
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

func TestReadCmdNoMeters(t *testing.T) {
	got, err := runRead(t, func() ([]cpu.Reading, error) { return nil, cpu.ErrNoMeters })
	if err != nil {
		t.Fatalf("no meters should not be an error, got %v", err)
	}
	if !strings.Contains(got, "No CPU power meters found.") {
		t.Errorf("got %q", got)
	}
}

func TestReadCmdFailure(t *testing.T) {
	if _, err := runRead(t, func() ([]cpu.Reading, error) { return nil, errors.New("boom") }); err == nil {
		t.Error("expected error, got nil")
	}
}
