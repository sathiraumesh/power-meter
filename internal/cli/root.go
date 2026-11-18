package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// version is set at build time:
//
//	go build -ldflags "-X sathiraumesh/power-meter/internal/cli.version=v1.0.0" ./cmd/power-meter
var version = "dev"

var rootCmd = &cobra.Command{
	Use:     "power-meter",
	Short:   "power-meter CLI",
	Version: version, // enables the --version / -v flag
}

func init() {
	rootCmd.SetVersionTemplate("{{.Version}}\n")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
