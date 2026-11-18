package cli

import (
	"errors"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"sathiraumesh/power-meter/internal/devices/cpu"
)

// deviceCheck checks one kind of device and prints what it found.
type deviceCheck struct {
	name string
	run  func(out io.Writer) error
}

// deviceChecks are run in order by the check command.
// To add a device (e.g. NIC), write a checkNIC function and add it here.
var deviceChecks = []deviceCheck{
	{name: "CPU", run: checkCPU},
}

func checkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check which power meters are available",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			for i, d := range deviceChecks {
				if i > 0 {
					fmt.Fprintln(out)
				}
				fmt.Fprintf(out, "== %s ==\n", d.name)
				if err := d.run(out); err != nil {
					return fmt.Errorf("%s: %w", d.name, err)
				}
			}
			return nil
		},
	}
	return cmd
}

func checkCPU(out io.Writer) error {
	meters, err := cpu.Check()
	if errors.Is(err, cpu.ErrNoMeters) {
		fmt.Fprintln(out, "No CPU power meters found.")
		return nil
	}
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SOURCE\tID\tNAME\tREADABLE")

	unreadable := 0
	for _, m := range meters {
		name := m.Name
		if m.Parent != "" {
			name = "  └ " + name // show subzones under their parent
		}

		readable := "yes"
		if !m.Readable {
			readable = "no"
			unreadable++
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", m.Source, m.ID, name, readable)
	}
	w.Flush()

	if unreadable > 0 {
		fmt.Fprintln(out, "\nSome meters are not readable. Run with sudo.")
	}
	return nil
}

func init() {
	rootCmd.AddCommand(checkCmd())
}
