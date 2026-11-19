// Package cpu finds the CPU power readings on this machine (RAPL zones),
// checks whether each can be read, and monitors power over time split into
// active and idle by CPU usage.
package cpu

import (
	"errors"
	"path/filepath"
	"time"

	"sathiraumesh/power-meter/internal/devices/cpu/rapl"
)

// Source is where a CPU power reading comes from.
type Source string

const (
	SourceRAPL Source = "rapl"
)

// Meter is one CPU power reading found on this machine.
type Meter struct {
	// Source is where the reading comes from (currently always rapl).
	Source Source
	// ID is the source's own ID, e.g. "intel-rapl:0".
	ID string
	// Name describes what it measures, e.g. "package-0" or "core".
	Name string
	// Parent is the ID of the RAPL zone this is a subzone of, or "".
	Parent string
	// Readable is true if a value could be read right now.
	Readable bool
	// Err is why it could not be read (often: needs root), or nil.
	Err error
}

// Reading is one value read from a CPU meter's energy file.
type Reading struct {
	Source Source
	ID     string
	Name   string
	Parent string
	// File is the file the value was read from, e.g.
	// "/sys/class/powercap/intel-rapl:0/energy_uj".
	File string
	// EnergyUJ is the counter value in microjoules (0 if Err is set).
	EnergyUJ uint64
	// Err is why the file could not be read (often: needs root), or nil.
	Err error
}

// Sample is the CPU power for one monitoring interval, with each
// zone's energy split into active and idle by CPU usage (as Kepler does).
type Sample struct {
	// Time is when the sample was taken.
	Time time.Time
	// Interval is the time since the previous sample.
	Interval time.Duration
	// UsageRatio is total CPU usage over the interval, 0.0-1.0.
	UsageRatio float64
	// Zones holds one entry per RAPL zone.
	Zones []ZoneUsage
}

// ZoneUsage is one zone's energy and power for an interval.
//
//	active = delta energy × usage ratio
//	idle   = delta energy − active
type ZoneUsage struct {
	ID     string
	Name   string
	Parent string

	// Power is the zone's average power over the interval, in watts.
	Power       float64
	ActivePower float64
	IdlePower   float64

	// ActiveEnergy and IdleEnergy are this interval's energy, in joules.
	ActiveEnergy float64
	IdleEnergy   float64

	// ActiveEnergyTotal and IdleEnergyTotal are running totals since the
	// monitor started, in joules. EnergyTotal is their sum.
	ActiveEnergyTotal float64
	IdleEnergyTotal   float64
	EnergyTotal       float64

	// Err is why the zone could not be read this interval (often: needs
	// root), or nil.
	Err error
}

// Monitor samples the CPU zones and usage at each Refresh.
type Monitor struct {
	zones    []rapl.Zone
	procStat string
	now      func() time.Time

	primed    bool
	prevAt    time.Time
	prevTimes Times
	prevUJ    map[string]uint64  // last raw counter per zone ID
	active    map[string]float64 // running active total (J) per zone ID
	idle      map[string]float64 // running idle total (J) per zone ID
}

// ErrNoMeters is returned when there are no CPU meters, e.g. inside a
// virtual machine.
var ErrNoMeters = errors.New("cpu: no CPU power meters found")

// ErrFirstSample is returned by the first Refresh. It only records a
// baseline; call Refresh again after the interval to get a sample.
var ErrFirstSample = errors.New("cpu: first sample is the baseline, refresh again for readings")

// Check returns the CPU power meters on this machine and whether each is
// readable. It returns ErrNoMeters if there are none.
func Check() ([]Meter, error) {
	return check(rapl.DefaultRoot)
}

func check(raplRoot string) ([]Meter, error) {
	meters, err := checkRAPL(raplRoot)
	if err != nil {
		return nil, err
	}
	if len(meters) == 0 {
		return nil, ErrNoMeters
	}
	return meters, nil
}

func checkRAPL(root string) ([]Meter, error) {
	zones, err := rapl.Discover(root)
	if errors.Is(err, rapl.ErrNoZones) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var meters []Meter
	for _, z := range zones {
		_, readErr := z.EnergyUJ()
		meters = append(meters, Meter{
			Source:   SourceRAPL,
			ID:       z.ID,
			Name:     z.Name,
			Parent:   z.Parent,
			Readable: readErr == nil,
			Err:      readErr,
		})
	}
	return meters, nil
}

// Read reads the energy file of every CPU meter once.
// It returns ErrNoMeters if there are none.
func Read() ([]Reading, error) {
	return read(rapl.DefaultRoot)
}

func read(raplRoot string) ([]Reading, error) {
	zones, err := rapl.Discover(raplRoot)
	if errors.Is(err, rapl.ErrNoZones) {
		return nil, ErrNoMeters
	}
	if err != nil {
		return nil, err
	}

	var readings []Reading
	for _, z := range zones {
		uj, readErr := z.EnergyUJ()
		readings = append(readings, Reading{
			Source:   SourceRAPL,
			ID:       z.ID,
			Name:     z.Name,
			Parent:   z.Parent,
			File:     filepath.Join(z.Path, "energy_uj"),
			EnergyUJ: uj,
			Err:      readErr,
		})
	}
	return readings, nil
}

// NewMonitor finds the CPU zones. It returns ErrNoMeters if
// there are none.
func NewMonitor() (*Monitor, error) {
	return newMonitor(rapl.DefaultRoot, DefaultProcStat, time.Now)
}

func newMonitor(raplRoot, procStat string, now func() time.Time) (*Monitor, error) {
	zones, err := rapl.Discover(raplRoot)
	if errors.Is(err, rapl.ErrNoZones) {
		return nil, ErrNoMeters
	}
	if err != nil {
		return nil, err
	}
	return &Monitor{
		zones:    zones,
		procStat: procStat,
		now:      now,
		prevUJ:   map[string]uint64{},
		active:   map[string]float64{},
		idle:     map[string]float64{},
	}, nil
}

// Refresh reads every zone's energy counter and the CPU usage, and returns
// the sample for the time since the previous Refresh. The first call only
// records a baseline and returns ErrFirstSample.
func (m *Monitor) Refresh() (Sample, error) {
	times, err := readTimes(m.procStat)
	if err != nil {
		return Sample{}, err
	}
	at := m.now()

	if !m.primed {
		for _, z := range m.zones {
			if uj, err := z.EnergyUJ(); err == nil {
				m.prevUJ[z.ID] = uj
			}
		}
		m.primed, m.prevAt, m.prevTimes = true, at, times
		return Sample{}, ErrFirstSample
	}

	interval := at.Sub(m.prevAt)
	secs := interval.Seconds()
	ratio := UsageRatio(m.prevTimes, times)

	s := Sample{Time: at, Interval: interval, UsageRatio: ratio}
	for _, z := range m.zones {
		zu := ZoneUsage{ID: z.ID, Name: z.Name, Parent: z.Parent}

		uj, err := z.EnergyUJ()
		prev, hadPrev := m.prevUJ[z.ID]
		switch {
		case err != nil:
			zu.Err = err
			delete(m.prevUJ, z.ID)
		case !hadPrev:
			// Became readable this interval: baseline only.
			m.prevUJ[z.ID] = uj
		default:
			m.prevUJ[z.ID] = uj
			delta := float64(z.Delta(prev, uj)) / 1e6 // joules

			zu.ActiveEnergy = delta * ratio
			zu.IdleEnergy = delta - zu.ActiveEnergy
			if secs > 0 {
				zu.Power = delta / secs
				zu.ActivePower = zu.Power * ratio
				zu.IdlePower = zu.Power - zu.ActivePower
			}
			m.active[z.ID] += zu.ActiveEnergy
			m.idle[z.ID] += zu.IdleEnergy
		}
		zu.ActiveEnergyTotal = m.active[z.ID]
		zu.IdleEnergyTotal = m.idle[z.ID]
		zu.EnergyTotal = zu.ActiveEnergyTotal + zu.IdleEnergyTotal
		s.Zones = append(s.Zones, zu)
	}

	m.prevAt, m.prevTimes = at, times
	return s, nil
}
