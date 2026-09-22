package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kjez66/sensorview/pkg/config"
	"github.com/kjez66/sensorview/pkg/sensors"
	"github.com/kjez66/sensorview/pkg/theme"
	"github.com/spf13/cobra"
)

// studioProbeTimeout bounds the check for an already running Studio. It is on
// this machine, so it answers at once or not at all.
const studioProbeTimeout = 500 * time.Millisecond

var uiNoBrowser bool

var uiCmd = &cobra.Command{
	Use:   "ui",
	Short: "Open the local SensorView Management Studio",
	Long: `Open the Management Studio, the visual editor for native themes.

If 'serve' or 'run' is already running, this opens the Studio they started.
Otherwise it starts one itself and keeps it running until Ctrl+C, which is how
to edit a native-only theme such as caelestia that 'serve' cannot show.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		address := defaultStudioAddress
		if cfg.Management != nil && cfg.Management.Address != "" {
			address = cfg.Management.Address
		}
		url := "http://" + address

		if studioRunning(url) {
			openStudio(url)
			return nil
		}
		return runStandaloneStudio(cfg, address)
	},
}

// studioRunning reports whether a SensorView Studio answers at url, rather
// than merely something listening on the port.
func studioRunning(url string) bool {
	client := &http.Client{Timeout: studioProbeTimeout}
	response, err := client.Get(url + "/api/v1/status")
	if err != nil {
		return false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return false
	}

	var status struct {
		State string `json:"state"`
	}
	return json.NewDecoder(response.Body).Decode(&status) == nil && status.State == "running"
}

// openStudio opens url in a browser, or prints it where none can be opened.
func openStudio(url string) {
	if uiNoBrowser || !theme.CanOpenBrowser() {
		fmt.Println(url)
		return
	}
	fmt.Printf("Opening SensorView Studio: %s\n", url)
	if err := theme.OpenBrowser(url); err != nil {
		fmt.Printf("Failed to open browser: %v\n", err)
	}
}

// runStandaloneStudio serves the Studio with its own sensor collection until
// interrupted.
func runStandaloneStudio(cfg *config.Config, address string) error {
	collector := sensors.NewCollector(&sensors.Config{Options: cfg.SensorOptions})
	collector.CollectAll()

	manager, err := startStudio(studioOptions{
		address:        address,
		collector:      collector,
		preferredTheme: cfg.Theme,
		mode:           studioModeStandalone,
		renderer:       "none",
	})
	if err != nil {
		return fmt.Errorf("start Management Studio on %s: %w", address, err)
	}
	defer manager.Close()

	fmt.Println("No Studio was running, so this process is serving one.")
	openStudio(manager.URL())
	fmt.Println("Press Ctrl+C to stop")

	interval := time.Duration(cfg.UpdateInterval * float64(time.Second))
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	for {
		select {
		case <-sigChan:
			fmt.Println("\nShutting down...")
			return nil
		case <-ticker.C:
			collector.CollectAll()
		}
	}
}

func init() {
	uiCmd.Flags().BoolVar(&uiNoBrowser, "no-browser", false, "Print the Studio address instead of opening a browser")
	rootCmd.AddCommand(uiCmd)
}
