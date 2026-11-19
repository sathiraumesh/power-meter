package cpu

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Times is a snapshot of the total CPU time counters from /proc/stat,
// in clock ticks, summed over all cores.
type Times struct {
	// Idle is time spent idle or waiting for I/O.
	Idle uint64
	// Total is all time: busy + idle.
	Total uint64
}

// DefaultProcStat is where the kernel exposes CPU time counters.
const DefaultProcStat = "/proc/stat"

// ReadTimes reads the total CPU time counters from /proc/stat.
func ReadTimes() (Times, error) {
	return readTimes(DefaultProcStat)
}

// readTimes parses the first line of a /proc/stat file:
//
//	cpu  user nice system idle iowait irq softirq steal guest guest_nice
//
// guest and guest_nice are already included in user and nice, so they
// are not added to Total.
func readTimes(path string) (Times, error) {
	f, err := os.Open(path)
	if err != nil {
		return Times{}, fmt.Errorf("cpu: %w", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return Times{}, errors.New("cpu: " + path + " is empty")
	}
	fields := strings.Fields(sc.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return Times{}, fmt.Errorf("cpu: unexpected first line in %s: %q", path, sc.Text())
	}

	var t Times
	values := fields[1:]
	if len(values) > 8 {
		values = values[:8] // user .. steal
	}
	for i, s := range values {
		v, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return Times{}, fmt.Errorf("cpu: parse %s: %w", path, err)
		}
		t.Total += v
		if i == 3 || i == 4 { // idle, iowait
			t.Idle += v
		}
	}
	return t, nil
}

// UsageRatio returns the fraction of CPU time (0.0-1.0) that was busy
// between two snapshots.
func UsageRatio(prev, cur Times) float64 {
	total := cur.Total - prev.Total
	if total == 0 {
		return 0
	}
	idle := cur.Idle - prev.Idle
	return float64(total-idle) / float64(total)
}
