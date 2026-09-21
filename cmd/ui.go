package cmd

import (
	"fmt"

	"github.com/kjez66/sensorview/pkg/config"
	"github.com/kjez66/sensorview/pkg/theme"
	"github.com/spf13/cobra"
)

var uiCmd = &cobra.Command{
	Use:   "ui",
	Short: "Open the local SensorView Management Studio",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		address := "127.0.0.1:19848"
		if cfg.Management != nil && cfg.Management.Address != "" {
			address = cfg.Management.Address
		}
		url := "http://" + address
		if !theme.CanOpenBrowser() {
			fmt.Println(url)
			return nil
		}
		fmt.Printf("Opening SensorView Studio: %s\n", url)
		return theme.OpenBrowser(url)
	},
}

func init() {
	rootCmd.AddCommand(uiCmd)
}
