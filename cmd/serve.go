package cmd

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/oae/sensorpanel/pkg/config"
	"github.com/oae/sensorpanel/pkg/display"
	"github.com/oae/sensorpanel/pkg/theme"
)

var (
	serveAddr     string
	serveInterval float64
	serveOpts     []string
)

var serveCmd = &cobra.Command{
	Use:   "serve [name]",
	Short: "Serve a built theme over HTTP with live sensor data",
	Long: `Serve a built theme so any browser can act as the panel.

This needs no USB device, no Node toolchain and no headless browser: it serves
the theme's dist/ directory and streams sensor readings over a WebSocket on the
same port. Build the theme first with 'sensorpanel theme build <name>'.

By default it listens on loopback only. To use a phone or tablet as the panel,
bind every interface:

  sensorpanel serve trofeo --addr 0.0.0.0:19847

The addresses to open are printed on startup. Note that the sensor feed has no
authentication, so anyone who can reach that port can read your system metrics;
bind wider only on a network you trust.`,
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

		srv, err := display.New(display.Options{
			DistDir:       t.DistDir(),
			Address:       serveAddr,
			Interval:      time.Duration(serveInterval * float64(time.Second)),
			SensorOptions: sensorOptions,
			OnReady: func(ready *display.Server) {
				printServeBanner(themeName, ready)
			},
		})
		if err != nil {
			return err
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
		return "", fmt.Errorf("no theme specified and no theme selected in config\nUse: sensorpanel serve <name>")
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
func printServeBanner(themeName string, srv *display.Server) {
	port := srv.Port()

	fmt.Printf("Serving theme: %s\n", themeName)
	fmt.Printf("[serve] Local:     http://localhost:%d/?ws=%d\n", port, port)

	urls := srv.URLs()
	if len(urls) == 0 {
		fmt.Println("[serve] Listening on loopback only; pass --addr 0.0.0.0:19847 to use another device as the panel")
	}
	for _, entry := range urls {
		fmt.Printf("[serve] Phone/LAN: %s  (%s)\n", entry.URL, entry.Interface)
	}

	if isWildcardAddress(srv.Address()) {
		fmt.Println("[serve] Warning: sensor readings are served without authentication to anyone on this network")
	}
	fmt.Println("[serve] Press Ctrl+C to stop")
}

// isWildcardAddress reports whether the listener accepts connections from off
// this machine.
func isWildcardAddress(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}

	ip := net.ParseIP(host)
	return ip != nil && ip.IsUnspecified()
}

func init() {
	serveCmd.Flags().StringVar(&serveAddr, "addr", display.DefaultAddress, "Address to listen on (use 0.0.0.0:19847 to allow other devices)")
	serveCmd.Flags().Float64VarP(&serveInterval, "interval", "i", 1.0, "Sensor update interval in seconds")
	serveCmd.Flags().StringSliceVarP(&serveOpts, "opt", "o", nil, "Sensor options in key=value format (e.g., lhm.url=http://localhost:8085/data.json)")

	rootCmd.AddCommand(serveCmd)
}
