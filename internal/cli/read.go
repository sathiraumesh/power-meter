package cli

import (
	"errors"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"sathiraumesh/power-meter/internal/devices/cpu"
)

// readCPU is cpu.Read, swappable in tests.
var readCPU = cpu.Read

func readCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "read",
		Short: "Read each CPU energy file once and print its value",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			readings, err := readCPU()
			if errors.Is(err, cpu.ErrNoMeters) {
				fmt.Fprintln(out, "No CPU power meters found.")
				return nil
			}
			if err != nil {
				return err
			}

			w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tFILE\tENERGY (µJ)")

			failed := 0
			for _, r := range readings {
				name := r.Name
				if r.Parent != "" {
					name = "  └ " + name // show subzones under their parent
				}

				value := fmt.Sprint(r.EnergyUJ)
				if r.Err != nil {
					value = "error: " + r.Err.Error()
					if errors.Is(r.Err, os.ErrPermission) {
						value = "permission denied"
					}
					failed++
				}
				fmt.Fprintf(w, "%s\t%s\t%s\n", name, r.File, value)
			}
			w.Flush()

			if failed > 0 {
				fmt.Fprintln(out, "\nSome files could not be read. Run with sudo.")
			}
			return nil
		},
	}
	return cmd
}

func init() {
	rootCmd.AddCommand(readCmd())
}
