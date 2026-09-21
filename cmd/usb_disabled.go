//go:build !usb

package cmd

import (
	"github.com/kjez66/sensorview/pkg/config"
	"github.com/spf13/cobra"
)

// usbSupportNote describes USB panel availability in the root command help.
const usbSupportNote = "This build has no USB panel support. To drive an AX206 USB panel as well,\n" +
	"rebuild with 'go build -tags usb' - that needs a C toolchain and libusb."

// The run, device, panel and benchmark commands all drive an AX206 USB panel
// through gousb, which needs cgo and libusb. Builds without the "usb" tag leave
// those out, but still register the command names so the CLI explains how to get
// them back instead of reporting an unknown command.
func usbDisabledCmd(use, short string) *cobra.Command {
	return &cobra.Command{
		Use:                use,
		Short:              short + " (unavailable in this build)",
		Long:               short + ".\n\n" + config.ErrUSBUnsupported.Error() + "\n\nTo drive a display without a USB panel, use 'sensorview serve' instead.",
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return config.ErrUSBUnsupported
		},
	}
}

func init() {
	rootCmd.AddCommand(usbDisabledCmd("run", "Run the dashboard on a USB panel"))
	rootCmd.AddCommand(usbDisabledCmd("device", "Manage USB display devices"))
	rootCmd.AddCommand(usbDisabledCmd("panel", "Control a USB display panel"))
	rootCmd.AddCommand(usbDisabledCmd("benchmark", "Benchmark USB panel transfer speed"))
}
