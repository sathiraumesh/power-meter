package cpu

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
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
