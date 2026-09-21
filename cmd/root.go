// Package cmd implements the CLI commands for sensorview.
package cmd

import (
	"fmt"
	"os"

	"github.com/kjez66/sensorview/pkg/paths"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "sensorview",
	Short: "Turn a spare tablet or phone into a system monitor",
	Long: `sensorview is a CLI tool that drives a live system dashboard and serves it
to whatever screen you already own.

The usual setup is an old tablet or phone on your network: run 'sensorview serve',
open the printed address in its browser, and prop it up next to your desk. No app
to install on the device, and no dedicated hardware to buy.

AX206-based USB panels (480x320, RGB565) are still supported, inherited from the
upstream project this is derived from - see 'sensorview device select'.`,
	PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
		// Installations that predate the SensorPanel -> SensorView rename keep
		// their config, themes and browser cache under the old directory name.
		if err := paths.MigrateLegacyDirs(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not migrate sensorpanel directories: %v\n", err)
		}
		return nil
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	// Global flags can be added here
	// rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file")
}
