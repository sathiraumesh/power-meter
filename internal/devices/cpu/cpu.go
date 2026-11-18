// Package cpu finds the CPU power readings on this machine (RAPL zones)
// and checks whether each can be read.
package cpu

import (
	"errors"

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

// ErrNoMeters is returned when there are no CPU meters, e.g. inside a
// virtual machine.
var ErrNoMeters = errors.New("cpu: no CPU power meters found")

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
