package rapl

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type fakeZone struct {
	id, name, maxRange, energy, enabled string
}

// writeSysfs builds a fake /sys/class/powercap tree under a temp dir.
func writeSysfs(t *testing.T, zones []fakeZone) string {
	t.Helper()
	root := t.TempDir()
	// Control-type directory and an MMIO zone, both of which must be ignored.
	for _, d := range []string{"intel-rapl", "intel-rapl-mmio:0"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, z := range zones {
		dir := filepath.Join(root, z.id)
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		files := map[string]string{
			"name":                z.name,
			"max_energy_range_uj": z.maxRange,
			"energy_uj":           z.energy,
			"enabled":             z.enabled,
		}
		for f, v := range files {
			if v == "" {
				continue
			}
			if err := os.WriteFile(filepath.Join(dir, f), []byte(v+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	return root
}

func summarize(zones []Zone) []Zone {
	out := make([]Zone, len(zones))
	for i, z := range zones {
		z.Path = ""
		out[i] = z
	}
	return out
}

func TestDiscoverAMDClient(t *testing.T) {
	// Layout of an AMD Ryzen 7 PRO 8840U.
	root := writeSysfs(t, []fakeZone{
		{"intel-rapl:0", "package-0", "65532610987", "1000", "0"},
		{"intel-rapl:0:0", "core", "65532610987", "400", "0"},
	})

	got, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []Zone{
		{ID: "intel-rapl:0", Name: "package-0", Domain: DomainPackage, Package: 0, MaxEnergyRangeUJ: 65532610987},
		{ID: "intel-rapl:0:0", Name: "core", Domain: DomainCore, Package: 0, Parent: "intel-rapl:0", MaxEnergyRangeUJ: 65532610987},
	}
	if !reflect.DeepEqual(summarize(got), want) {
		t.Errorf("got  %+v\nwant %+v", summarize(got), want)
	}
}

func TestDiscoverIntelWithPsys(t *testing.T) {
	// psys takes index 0, so package-0 is intel-rapl:1; written out of
	// order to check sorting (including 10 after 2).
	root := writeSysfs(t, []fakeZone{
		{"intel-rapl:1:2", "dram", "262143328850", "", "1"},
		{"intel-rapl:0", "psys", "262143328850", "", "1"},
		{"intel-rapl:1", "package-0", "262143328850", "", "1"},
		{"intel-rapl:1:0", "core", "262143328850", "", "1"},
		{"intel-rapl:1:1", "uncore", "262143328850", "", "1"},
		{"intel-rapl:10", "package-1", "262143328850", "", ""},
		{"intel-rapl:2", "mystery", "1", "", "1"},
	})

	got, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		ID      string
		Domain  Domain
		Package int
		Parent  string
		Enabled bool
	}
	var rows []row
	for _, z := range got {
		rows = append(rows, row{z.ID, z.Domain, z.Package, z.Parent, z.Enabled})
	}
	want := []row{
		{"intel-rapl:0", DomainPsys, -1, "", true},
		{"intel-rapl:1", DomainPackage, 0, "", true},
		{"intel-rapl:1:0", DomainCore, 0, "intel-rapl:1", true},
		{"intel-rapl:1:1", DomainUncore, 0, "intel-rapl:1", true},
		{"intel-rapl:1:2", DomainDRAM, 0, "intel-rapl:1", true},
		{"intel-rapl:2", DomainUnknown, -1, "", true},
		{"intel-rapl:10", DomainPackage, 1, "", false},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("got  %+v\nwant %+v", rows, want)
	}
}

func TestDiscoverNoZones(t *testing.T) {
	if _, err := Discover(filepath.Join(t.TempDir(), "missing")); !errors.Is(err, ErrNoZones) {
		t.Errorf("missing root: got %v, want ErrNoZones", err)
	}
	if _, err := Discover(writeSysfs(t, nil)); !errors.Is(err, ErrNoZones) {
		t.Errorf("empty root: got %v, want ErrNoZones", err)
	}
}

func TestDiscoverMalformed(t *testing.T) {
	tests := map[string]fakeZone{
		"bad id":        {"intel-rapl:x", "package-0", "100", "", "1"},
		"missing name":  {"intel-rapl:0", "", "100", "", "1"},
		"bad max range": {"intel-rapl:0", "package-0", "abc", "", "1"},
	}
	for name, z := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Discover(writeSysfs(t, []fakeZone{z})); err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestEnergyUJ(t *testing.T) {
	root := writeSysfs(t, []fakeZone{{"intel-rapl:0", "package-0", "100", "42", "1"}})
	zones, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := zones[0].EnergyUJ()
	if err != nil {
		t.Fatal(err)
	}
	if got != 42 {
		t.Errorf("got %d, want 42", got)
	}
}

func TestDelta(t *testing.T) {
	z := Zone{MaxEnergyRangeUJ: 1000}
	tests := []struct {
		name            string
		prev, cur, want uint64
	}{
		{"increasing", 100, 350, 250},
		{"unchanged", 500, 500, 0},
		{"wraparound", 900, 50, 150},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := z.Delta(tt.prev, tt.cur); got != tt.want {
				t.Errorf("Delta(%d, %d) = %d, want %d", tt.prev, tt.cur, got, tt.want)
			}
		})
	}
}
