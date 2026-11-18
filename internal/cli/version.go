package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// version is set at build time:
//
//	go build -ldflags "-X sathiraumesh/power-meter/internal/cli.version=v1.0.0" ./cmd/power-meter
var version = "dev"

func versionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(cmd.OutOrStdout(), version)
		},
	}
	return cmd
}

func init() {
	rootCmd.AddCommand(versionCmd())
}
