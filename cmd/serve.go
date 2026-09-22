package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/kjez66/sensorview/pkg/config"
	"github.com/kjez66/sensorview/pkg/display"
	"github.com/kjez66/sensorview/pkg/theme"
)

var (
	serveAddr           string
	serveInterval       float64
	serveOpts           []string
	serveManagement     bool
	serveManagementAddr string
)

var serveCmd = &cobra.Command{
	Use:   "serve [name]",
	Short: "Serve a built theme over HTTP with live sensor data",
	Long: `Serve a built theme so any browser can act as the panel.

This needs no USB device, no Node toolchain and no headless browser: it serves
the theme's dist/ directory and streams sensor readings over a WebSocket on the
same port. Build the theme first with 'sensorview theme build <name>'.

By default it listens on loopback only. To use a phone or tablet as the panel,
bind every interface:

  sensorview serve trofeo --addr 0.0.0.0:19847

The addresses to open are printed on startup. Note that the sensor feed has no
authentication, so anyone who can reach that port can read your system metrics;
bind wider only on a network you trust.

The Management Studio for editing native themes also starts, on
127.0.0.1:19848 by default and never on the network. Pass --management=false
to leave it out.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		themeName, err := resolveServeTheme(args)
		if err != nil {
			return err
		}

		t, err := theme.Load(themeName)
		if err != nil {
			if err == theme.ErrThemeNotFound {
				return fmt.Errorf("theme '%s' not found (use 'theme list' to see available themes)", themeName)
			}
			return fmt.Errorf("failed to load theme '%s': %w", themeName, err)
		}

		sensorOptions, err := serveSensorOptions()
		if err != nil {
			return err
		}

		// Set before Run, which is what calls OnReady.
		studioURL := ""
		srv, err := display.New(display.Options{
			DistDir:       t.DistDir(),
			Address:       serveAddr,
			Interval:      time.Duration(serveInterval * float64(time.Second)),
			SensorOptions: sensorOptions,
			OnReady: func(ready *display.Server) {
				printServeBanner(themeName, ready, studioURL)
			},
		})
		if err != nil {
			return err
		}

		cfg, _ := config.Load()
		enabled, address := studioSettings(cfg,
			serveManagement, cmd.Flags().Changed("management"),
			serveManagementAddr, cmd.Flags().Changed("management-address"))
		if enabled {
			// A Studio that cannot start, typically because run or another serve
			// already holds the port, is not a reason to stop serving the panel.
			manager, err := startStudio(address, srv.Collector(), themeName, studioModeServe)
			if err != nil {
				fmt.Printf("[serve] Warning: Management Studio unavailable: %v\n", err)
			} else {
				defer manager.Close()
				studioURL = manager.URL()
			}
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigChan
			fmt.Println("\nShutting down...")
			cancel()
		}()

		return srv.Run(ctx)
	},
}

// resolveServeTheme picks the theme named on the command line, or the selected
// one from config.
func resolveServeTheme(args []string) (string, error) {
	if len(args) > 0 {
		return args[0], nil
	}

	themeName, _ := config.GetTheme()
	if themeName == "" {
		return "", fmt.Errorf("no theme specified and no theme selected in config\nUse: sensorview serve <name>")
	}
	return themeName, nil
}

// serveSensorOptions merges the configured sensor options with any --opt flags.
func serveSensorOptions() (map[string]interface{}, error) {
	var sensorOptions map[string]interface{}

	if cfg, err := config.Load(); err == nil && cfg.SensorOptions != nil {
		sensorOptions = make(map[string]interface{}, len(cfg.SensorOptions))
		for key, value := range cfg.SensorOptions {
			sensorOptions[key] = value
		}
	}

	if len(serveOpts) == 0 {
		return sensorOptions, nil
	}
	if sensorOptions == nil {
		sensorOptions = make(map[string]interface{}, len(serveOpts))
	}

	for _, opt := range serveOpts {
		key, value, ok := strings.Cut(opt, "=")
		if !ok {
			return nil, fmt.Errorf("invalid option format %q (expected key=value)", opt)
		}
		// A comma-separated value is a list, matching the other commands.
		if strings.Contains(value, ",") {
			sensorOptions[key] = strings.Split(value, ",")
		} else {
			sensorOptions[key] = value
		}
	}

	return sensorOptions, nil
}

// printServeBanner reports where the panel can be opened.
func printServeBanner(themeName string, srv *display.Server, studioURL string) {
	fmt.Printf("Serving theme: %s\n", themeName)
	if local := srv.LocalURL(); local != "" {
		fmt.Printf("[serve] Local:     %s\n", local)
	}

	if !srv.Exposed() {
		fmt.Println("[serve] Listening on loopback only; pass --addr 0.0.0.0:19847 to use another device as the panel")
	}
	for _, entry := range srv.URLs() {
		fmt.Printf("[serve] Phone/LAN: %s  (%s)\n", entry.URL, entry.Interface)
	}

	if srv.Exposed() {
		fmt.Println("[serve] Warning: sensor readings are served without authentication to anyone on this network")
	}
	if studioURL != "" {
		fmt.Printf("[serve] Studio:    %s  (this machine only)\n", studioURL)
	}
	fmt.Println("[serve] Press Ctrl+C to stop")
}

func init() {
	serveCmd.Flags().StringVar(&serveAddr, "addr", display.DefaultAddress, "Address to listen on (use 0.0.0.0:19847 to allow other devices)")
	serveCmd.Flags().Float64VarP(&serveInterval, "interval", "i", 1.0, "Sensor update interval in seconds")
	serveCmd.Flags().StringSliceVarP(&serveOpts, "opt", "o", nil, "Sensor options in key=value format (e.g., lhm.url=http://localhost:8085/data.json)")
	serveCmd.Flags().BoolVar(&serveManagement, "management", true, "Serve the local Management Studio")
	serveCmd.Flags().StringVar(&serveManagementAddr, "management-address", "", "Management Studio address (localhost only; default "+defaultStudioAddress+")")

	rootCmd.AddCommand(serveCmd)
}
