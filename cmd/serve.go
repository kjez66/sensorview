package cmd

import (
	"context"
	"errors"
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
	serveRenderer       string
)

var serveCmd = &cobra.Command{
	Use:   "serve [name]",
	Short: "Serve a theme over HTTP so any browser can be the panel",
	Long: `Serve a theme so any browser can act as the panel.

This needs no USB device, no Node toolchain and no headless browser. A theme can
be shown two ways, chosen with --renderer:

  native  The native Go renderer draws native.theme.json here and streams the
          frames to a viewer page. This is the version the Management Studio
          edits, and applying it there updates every open screen.
  web     The theme's built dist/ directory is served, with sensor readings on
          a WebSocket on the same port. Build it first with
          'sensorview theme build <name>'.
  auto    native when the theme has one and it loads, otherwise web (default).

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

		renderers, err := serveRendererOrder(serveRenderer, t.HasNative)
		if err != nil {
			return fmt.Errorf("theme '%s': %w", themeName, err)
		}

		// Set before Run, which is what calls OnReady.
		studioURL := ""
		options := display.Options{
			Address:       serveAddr,
			Interval:      time.Duration(serveInterval * float64(time.Second)),
			SensorOptions: sensorOptions,
			OnReady: func(ready *display.Server) {
				printServeBanner(themeName, ready, studioURL)
			},
		}
		srv, err := newServeDisplay(options, t, renderers)
		if err != nil {
			return err
		}

		cfg, _ := config.Load()
		enabled, address := studioSettings(cfg,
			serveManagement, cmd.Flags().Changed("management"),
			serveManagementAddr, cmd.Flags().Changed("management-address"))
		if enabled {
			renderer := serveRendererWeb
			if srv.Native() {
				renderer = serveRendererNative
			}
			// A Studio that cannot start, typically because run or another serve
			// already holds the port, is not a reason to stop serving the panel.
			manager, err := startStudio(studioOptions{
				address:        address,
				collector:      srv.Collector(),
				preferredTheme: themeName,
				mode:           studioModeServe,
				renderer:       renderer,
				// Applying the theme on display redraws every connected screen;
				// any other theme is only saved. A failed reload makes the Studio
				// restore the previous file.
				applyTheme: func(name string) error {
					if name != themeName {
						return nil
					}
					return srv.Reload()
				},
			})
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

// Renderers serve can use.
const (
	serveRendererAuto   = "auto"
	serveRendererNative = "native"
	serveRendererWeb    = "web"
)

// serveRendererOrder returns the renderers to try, in order. auto prefers the
// native version of a theme, as run does, because that is the one the
// Management Studio edits; the web version is the fallback.
func serveRendererOrder(requested string, hasNative bool) ([]string, error) {
	switch requested {
	case serveRendererAuto, "":
		if hasNative {
			return []string{serveRendererNative, serveRendererWeb}, nil
		}
		return []string{serveRendererWeb}, nil
	case serveRendererNative:
		if !hasNative {
			return nil, fmt.Errorf("no native.theme.json; use --renderer web")
		}
		return []string{serveRendererNative}, nil
	case serveRendererWeb:
		return []string{serveRendererWeb}, nil
	default:
		return nil, fmt.Errorf("invalid renderer %q (expected auto, native, or web)", requested)
	}
}

// newServeDisplay prepares a display with the first renderer that works. A
// native theme that cannot load, for example because its background video was
// never extracted, falls back to the web version when there is one.
func newServeDisplay(options display.Options, t *theme.Theme, renderers []string) (*display.Server, error) {
	var failures []error
	for i, renderer := range renderers {
		attempt := options
		if renderer == serveRendererNative {
			attempt.NativeThemeDir = t.Path
		} else {
			attempt.DistDir = t.DistDir()
		}

		srv, err := display.New(attempt)
		if err == nil {
			return srv, nil
		}
		failures = append(failures, fmt.Errorf("%s: %w", renderer, err))
		if i+1 < len(renderers) {
			fmt.Printf("[serve] The %s version of '%s' cannot be shown (%v); trying the %s version\n",
				renderer, t.Name, err, renderers[i+1])
		}
	}
	return nil, fmt.Errorf("theme '%s': %w", t.Name, errors.Join(failures...))
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
	renderer := serveRendererWeb
	if srv.Native() {
		renderer = serveRendererNative
	}
	fmt.Printf("Serving theme: %s (%s)\n", themeName, renderer)
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
	serveCmd.Flags().StringVar(&serveRenderer, "renderer", serveRendererAuto, "How to draw the theme: auto, native, or web")
	serveCmd.Flags().BoolVar(&serveManagement, "management", true, "Serve the local Management Studio")
	serveCmd.Flags().StringVar(&serveManagementAddr, "management-address", "", "Management Studio address (localhost only; default "+defaultStudioAddress+")")

	rootCmd.AddCommand(serveCmd)
}
