package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"sathiraumesh/power-meter/internal/devices/cpu"
)

// monitor is what the measure command needs from cpu.Monitor.
type monitor interface {
	Refresh() (cpu.Sample, error)
}

// seconds is a duration flag that also accepts a plain number of seconds,
// so "-i 1" means 1s. Values with a unit ("500ms", "2m") work as usual.
type seconds time.Duration

func (s *seconds) String() string { return time.Duration(*s).String() }
func (s *seconds) Type() string   { return "duration" }

func (s *seconds) Set(v string) error {
	if n, err := strconv.ParseFloat(v, 64); err == nil {
		*s = seconds(n * float64(time.Second))
		return nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fmt.Errorf("use seconds (e.g. 1, 0.5) or a duration (e.g. 500ms, 2m)")
	}
	*s = seconds(d)
	return nil
}

// newMonitor and wait are swappable in tests.
var (
	newMonitor = func() (monitor, error) { return cpu.NewMonitor() }
	wait       = func(ctx context.Context, d time.Duration) bool {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(d):
			return true
		}
	}
)

func measureCmd() *cobra.Command {
	var (
		interval = seconds(5 * time.Second)
		count    int
	)

	cmd := &cobra.Command{
		Use:   "measure",
		Short: "Measure CPU energy per zone, split into active and idle",
		Long: `Sample every RAPL zone at a fixed interval and split each zone's energy
into active and idle by total CPU usage (from /proc/stat), as Kepler does:

  active = zone energy × CPU usage ratio
  idle   = zone energy − active

Runs until --count samples are printed, or until Ctrl+C when --count is 0.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if interval <= 0 {
				return errors.New("--interval must be greater than 0")
			}
			out := cmd.OutOrStdout()

			m, err := newMonitor()
			if errors.Is(err, cpu.ErrNoMeters) {
				fmt.Fprintln(out, "No CPU power meters found.")
				return nil
			}
			if err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()

			// First refresh only records the baseline.
			if _, err := m.Refresh(); err != nil && !errors.Is(err, cpu.ErrFirstSample) {
				return err
			}

			for i := 0; count == 0 || i < count; i++ {
				if !wait(ctx, time.Duration(interval)) {
					return nil // Ctrl+C
				}
				s, err := m.Refresh()
				if err != nil {
					return err
				}
				if i > 0 {
					fmt.Fprintln(out)
				}
				printSample(out, s)
			}
			return nil
		},
	}

	cmd.Flags().VarP(&interval, "interval", "i", "time between samples in seconds, e.g. 1, 0.5, 500ms")
	cmd.Flags().IntVarP(&count, "count", "c", 0, "number of samples to print (0 = until Ctrl+C)")
	return cmd
}

func printSample(out io.Writer, s cpu.Sample) {
	fmt.Fprintf(out, "%s  interval %s  CPU usage %.1f%%\n",
		s.Time.Format("15:04:05"), s.Interval.Round(time.Millisecond), s.UsageRatio*100)

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ZONE\tPOWER\tACTIVE\tIDLE\tACTIVE TOTAL\tIDLE TOTAL\tTOTAL\t")

	failed := 0
	for _, z := range s.Zones {
		name := z.Name
		if z.Parent != "" {
			name = "  └ " + name // show subzones under their parent
		}
		if z.Err != nil {
			msg := "error: " + z.Err.Error()
			if errors.Is(z.Err, os.ErrPermission) {
				msg = "permission denied"
			}
			fmt.Fprintf(w, "%s\t%s\t\t\t\t\t\t\n", name, msg)
			failed++
			continue
		}
		fmt.Fprintf(w, "%s\t%.2f W\t%.2f W\t%.2f W\t%.2f J\t%.2f J\t%.2f J\t\n",
			name, z.Power, z.ActivePower, z.IdlePower, z.ActiveEnergyTotal, z.IdleEnergyTotal, z.EnergyTotal)
	}
	w.Flush()

	if failed > 0 {
		fmt.Fprintln(out, "Some zones could not be read. Run with sudo.")
	}
}

func init() {
	rootCmd.AddCommand(measureCmd())
}
