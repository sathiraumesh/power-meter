// Package rapl reads CPU energy counters exposed by the Linux powercap
// framework under /sys/class/powercap.
//
// Both Intel and AMD (Zen and later) CPUs are exposed through the
// "intel-rapl" control type. Zones are named "intel-rapl:<pkg>" for a
// package and "intel-rapl:<pkg>:<n>" for its subzones:
//
//	intel-rapl:0      package-0
//	intel-rapl:0:0    core      (all CPU cores in the package)
//	intel-rapl:0:1    uncore    (integrated GPU / LLC, Intel client CPUs)
//	intel-rapl:0:2    dram      (memory controller, mostly Intel server CPUs)
//	intel-rapl:1      psys      (whole SoC platform, Intel Skylake+)
//
// Which zones exist depends on the CPU. Reading energy_uj requires root on
// kernels since 5.10 (CVE-2020-8694); the other attributes are world-readable.
package rapl

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// DefaultRoot is where the kernel exposes powercap zones.
const DefaultRoot = "/sys/class/powercap"

// zonePrefix matches top-level zones and subzones, but not the
// "intel-rapl" control-type directory itself or "intel-rapl-mmio" zones,
// which duplicate the MSR-based package counters.
const zonePrefix = "intel-rapl:"

// Domain is the part of the system a zone measures.
type Domain string

const (
	DomainPackage Domain = "package"
	DomainCore    Domain = "core"
	DomainUncore  Domain = "uncore"
	DomainDRAM    Domain = "dram"
	DomainPsys    Domain = "psys"
	DomainUnknown Domain = "unknown"
)

// Zone is a single RAPL powercap zone.
type Zone struct {
	// ID is the sysfs directory name, e.g. "intel-rapl:0:0".
	ID string
	// Name is the kernel-provided zone name, e.g. "package-0" or "core".
	Name string
	// Domain is derived from Name.
	Domain Domain
	// Package is the CPU package (socket) index the zone belongs to,
	// or -1 for zones that are not tied to a package (psys).
	Package int
	// Parent is the ID of the parent zone, or "" for top-level zones.
	Parent string
	// Path is the absolute sysfs path of the zone directory.
	Path string
	// MaxEnergyRangeUJ is the value at which energy_uj wraps to zero.
	MaxEnergyRangeUJ uint64
	// Enabled reports the zone's "enabled" attribute. It refers to power
	// limiting; energy counters are readable even when it is false.
	Enabled bool
}

// ErrNoZones is returned by Discover when no RAPL zones are present, e.g. on
// virtual machines or when the intel_rapl_msr module is not loaded.
var ErrNoZones = errors.New("rapl: no powercap zones found")

// Discover returns all RAPL zones under root (normally DefaultRoot),
// sorted with parents before their subzones.
func Discover(root string) ([]Zone, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNoZones
		}
		return nil, fmt.Errorf("rapl: read %s: %w", root, err)
	}

	var zones []Zone

	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), zonePrefix) {
			continue
		}
		z, err := readZone(filepath.Join(root, e.Name()))
		if err != nil {
			return nil, err
		}
		zones = append(zones, z)
	}
	if len(zones) == 0 {
		return nil, ErrNoZones
	}

	sort.Slice(zones, func(i, j int) bool { return lessID(zones[i].ID, zones[j].ID) })

	// Subzones belong to the same package as their parent.
	pkg := make(map[string]int, len(zones))
	for _, z := range zones {
		if z.Parent == "" {
			pkg[z.ID] = z.Package
		}
	}
	for i := range zones {
		if p, ok := pkg[zones[i].Parent]; ok {
			zones[i].Package = p
		}
	}
	return zones, nil
}

func readZone(path string) (Zone, error) {
	id := filepath.Base(path)
	idx, err := parseID(id)
	if err != nil {
		return Zone{}, err
	}

	name, err := readString(filepath.Join(path, "name"))
	if err != nil {
		return Zone{}, err
	}
	maxRange, err := readUint(filepath.Join(path, "max_energy_range_uj"))
	if err != nil {
		return Zone{}, err
	}
	// "enabled" is optional; treat a missing file as disabled.
	enabled := false
	if s, err := readString(filepath.Join(path, "enabled")); err == nil {
		enabled = s == "1"
	}

	z := Zone{
		ID:               id,
		Name:             name,
		Domain:           domainOf(name),
		Package:          -1,
		Path:             path,
		MaxEnergyRangeUJ: maxRange,
		Enabled:          enabled,
	}
	if len(idx) > 1 {
		z.Parent = zonePrefix + strconv.Itoa(idx[0])
	}
	z.Package = packageOf(z, idx)
	return z, nil
}

// packageOf returns the CPU package index for a top-level zone. The sysfs
// index is not the package number when psys is present (psys can take index
// 0 or 1), so prefer the number in "package-N". Subzones are resolved from
// their parent in Discover.
func packageOf(z Zone, idx []int) int {
	switch z.Domain {
	case DomainPsys:
		return -1
	case DomainPackage:
		if n, err := strconv.Atoi(strings.TrimPrefix(z.Name, "package-")); err == nil {
			return n
		}
		return idx[0]
	default:
		return -1
	}
}

// domainOf maps a kernel zone name to a Domain.
func domainOf(name string) Domain {
	switch {
	case strings.HasPrefix(name, "package-"):
		return DomainPackage
	case name == "core":
		return DomainCore
	case name == "uncore":
		return DomainUncore
	case name == "dram":
		return DomainDRAM
	case name == "psys":
		return DomainPsys
	default:
		return DomainUnknown
	}
}

// parseID turns "intel-rapl:0:1" into [0 1].
func parseID(id string) ([]int, error) {
	parts := strings.Split(strings.TrimPrefix(id, zonePrefix), ":")
	idx := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("rapl: invalid zone id %q", id)
		}
		idx[i] = n
	}
	return idx, nil
}

// lessID orders zone IDs numerically: intel-rapl:0 < intel-rapl:0:0 <
// intel-rapl:0:1 < intel-rapl:1 < intel-rapl:10.
func lessID(a, b string) bool {
	ai, _ := parseID(a)
	bi, _ := parseID(b)
	for k := 0; k < len(ai) && k < len(bi); k++ {
		if ai[k] != bi[k] {
			return ai[k] < bi[k]
		}
	}
	return len(ai) < len(bi)
}

// EnergyUJ returns the zone's cumulative energy counter in microjoules.
// It wraps to zero after MaxEnergyRangeUJ; use Delta to difference readings.
func (z Zone) EnergyUJ() (uint64, error) {
	v, err := readUint(filepath.Join(z.Path, "energy_uj"))
	if errors.Is(err, os.ErrPermission) {
		return 0, fmt.Errorf("rapl: reading %s energy requires root: %w", z.ID, err)
	}
	return v, err
}

// Delta returns the energy consumed between two readings of this zone in
// microjoules, accounting for a single counter wraparound.
func (z Zone) Delta(prev, cur uint64) uint64 {
	if cur >= prev {
		return cur - prev
	}
	return z.MaxEnergyRangeUJ - prev + cur
}

func readString(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("rapl: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}

func readUint(path string) (uint64, error) {
	s, err := readString(path)
	if err != nil {
		return 0, err
	}
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("rapl: parse %s: %w", path, err)
	}
	return v, nil
}
