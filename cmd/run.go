//go:build usb

// cmd/run.go - Main dashboard run command
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/kjez66/sensorview/pkg/activity"
	"github.com/kjez66/sensorview/pkg/animation"
	"github.com/kjez66/sensorview/pkg/browser"
	"github.com/kjez66/sensorview/pkg/config"
	"github.com/kjez66/sensorview/pkg/device"
	"github.com/kjez66/sensorview/pkg/management"
	"github.com/kjez66/sensorview/pkg/music"
	"github.com/kjez66/sensorview/pkg/nativerender"
	"github.com/kjez66/sensorview/pkg/panel"
	"github.com/kjez66/sensorview/pkg/renderer"
	"github.com/kjez66/sensorview/pkg/sensors"
	"github.com/kjez66/sensorview/pkg/server"
	"github.com/kjez66/sensorview/pkg/theme"
)

var (
	runInterval       float64
	runBrightness     int
	runSensors        []string
	runExcludeSensors []string
	runOpts           []string
	runGIF            string
	runImage          string
	runMusic          bool
	runOrientation    int
	runRenderer       string
	runTargetFPS      float64
	runJPEGQuality    int
	runJPEGEncoder    string
	runManagement     bool
	runManagementAddr string
)

const nativeIdleKeepaliveWindow = 750 * time.Millisecond

var themeSensorInterval = time.Second

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the sensor panel dashboard",
	Long: `Start the sensor panel dashboard, displaying system metrics
on the USB display in a continuous loop.

By default, all available sensors are enabled. Use --sensors to enable only
specific sensors, or --exclude to disable specific sensors.

Use --gif to play an animated GIF instead of displaying sensor data. GIF
playback skips sensor collection and the selected theme.

Use --image to display a PNG, JPEG, or GIF file or URL as a static image.

Use --music for a now-playing dashboard with cover art, track and artist
information, a song-specific progress waveform, and auto-scrolling timed lyrics.

Available sensors (varies by platform):
  cpu        - CPU temperature, load, frequency
  memory     - RAM usage
  nvidia_gpu - NVIDIA GPU stats
  amd_gpu    - AMD GPU stats
  disk       - Disk usage per mount point
  network    - Network throughput per interface

Sensor options (use --opt key=value):
  disk.mounts=/,/home      - Disk mount points to monitor
  network.interface=eth*   - Network interface filter pattern
  nvidia_gpu.smi_path=...  - Custom path to nvidia-smi

Examples:
  sensorview run                                    # All sensors
  sensorview run -s cpu,memory,disk                 # Only CPU, memory, and disk
  sensorview run -x network                         # All except network
  sensorview run --opt disk.mounts=/,/home          # Monitor specific mounts
  sensorview run --gif animation.gif                # Play a local GIF continuously
  sensorview run --gif https://example.com/a.gif    # Play a remote GIF
  sensorview run --image wallpaper.png              # Show a static image
  sensorview run --music                            # Show now-playing dashboard
  sensorview run --renderer native                  # Render selected theme without Chrome

Press Ctrl+C to stop. The backlight will be turned off on exit.`,
	RunE: runDashboard,
}

func init() {
	rootCmd.AddCommand(runCmd)

	runCmd.Flags().Float64VarP(&runInterval, "interval", "i", 1.0, "Update interval in seconds (min 0.5; music min 0.25)")
	runCmd.Flags().IntVarP(&runBrightness, "brightness", "b", 7, "Backlight brightness (0-7)")
	runCmd.Flags().StringSliceVarP(&runSensors, "sensors", "s", nil, "Sensors to enable (e.g., cpu,memory,disk). Default: all available")
	runCmd.Flags().StringSliceVarP(&runExcludeSensors, "exclude", "x", nil, "Sensors to exclude (e.g., network,nvidia_gpu)")
	runCmd.Flags().StringSliceVarP(&runOpts, "opt", "o", nil, "Sensor options in key=value format (e.g., disk.mounts=/,/home)")
	runCmd.Flags().StringVar(&runGIF, "gif", "", "Play an animated GIF file or URL instead of sensor data")
	runCmd.Flags().StringVar(&runImage, "image", "", "Display a PNG, JPEG, or GIF file or URL instead of sensor data")
	runCmd.Flags().BoolVar(&runMusic, "music", false, "Show now-playing music dashboard instead of sensor data")
	runCmd.Flags().IntVar(&runOrientation, "orientation", 0, "Display orientation in degrees (0, 90, 180, 270)")
	runCmd.Flags().StringVar(&runRenderer, "renderer", config.RendererAuto, "Theme renderer: auto, native, or chrome")
	runCmd.Flags().Float64Var(&runTargetFPS, "target-fps", 0, "Native theme animation target FPS (0 uses theme profile)")
	runCmd.Flags().IntVar(&runJPEGQuality, "jpeg-quality", 0, "LY JPEG quality 1-100 (0 uses theme profile)")
	runCmd.Flags().StringVar(&runJPEGEncoder, "jpeg-encoder", "auto", "LY JPEG encoder: auto, stdlib, or turbo")
	runCmd.Flags().BoolVar(&runManagement, "management", true, "Serve the local management Studio")
	runCmd.Flags().StringVar(&runManagementAddr, "management-address", "", "Management Studio address (localhost only)")
}

func runDashboard(cmd *cobra.Command, args []string) error {
	mediaModes := 0
	if runGIF != "" {
		mediaModes++
	}
	if runImage != "" {
		mediaModes++
	}
	if runMusic {
		mediaModes++
	}
	if mediaModes > 1 {
		return fmt.Errorf("--gif, --image, and --music cannot be used together")
	}

	// Load config for defaults
	cfg, err := config.Load()
	if err != nil {
		cfg = config.DefaultConfig()
	}

	// Use config defaults if flags not explicitly set
	if !cmd.Flags().Changed("interval") && cfg.UpdateInterval > 0 {
		runInterval = cfg.UpdateInterval
	}
	if !cmd.Flags().Changed("brightness") {
		runBrightness = cfg.Brightness
	}
	if !cmd.Flags().Changed("renderer") && cfg.Renderer != "" {
		runRenderer = cfg.Renderer
	}
	if !cmd.Flags().Changed("orientation") {
		runOrientation = cfg.Orientation
	}
	if cfg.Management != nil {
		if !cmd.Flags().Changed("management") {
			runManagement = cfg.Management.Enabled
		}
		if !cmd.Flags().Changed("management-address") {
			runManagementAddr = cfg.Management.Address
		}
	}
	if runRenderer, err = config.NormalizeRenderer(runRenderer); err != nil {
		return err
	}

	if runMusic && !cmd.Flags().Changed("interval") {
		runInterval = 0.5
	}

	// Clamp brightness
	if runBrightness < 0 {
		runBrightness = 0
	}
	if runBrightness > 7 {
		runBrightness = 7
	}

	// Create and open device using config
	dev, err := openConfiguredDevice()
	fmt.Println("Opening USB display...")
	if err != nil {
		return fmt.Errorf("failed to open device: %w", err)
	}
	defer dev.Close()
	if err := dev.SetOrientation(runOrientation); err != nil {
		return err
	}

	// Validate interval after device detection. Standard sensor panels stay at a
	// conservative cadence; LY JPEG panels can sustain much faster full-frame
	// updates for lightweight animated themes.
	minimumInterval := 0.5
	if runMusic {
		minimumInterval = 0.25
	}
	if dev.Profile.ProtocolType() == device.ProtocolLYBulk {
		minimumInterval = 1.0 / 24.0
	}
	if runInterval < minimumInterval {
		runInterval = minimumInterval
	}
	// Sensor polling is deliberately independent from animation FPS. Hardware
	// providers are never polled faster than twice per second.
	themeSensorInterval = time.Duration(max(0.5, runInterval) * float64(time.Second))

	fmt.Printf("Connected: %s\n", dev.Info.String())

	// Set brightness
	if err := dev.SetBacklight(runBrightness); err != nil {
		fmt.Printf("Warning: failed to set brightness: %v\n", err)
	}

	// Setup signal handling for graceful shutdown.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	if runGIF != "" {
		return runGIFPlayback(dev, runGIF, sigChan)
	}
	if runImage != "" {
		return runStaticImage(dev, runImage, sigChan)
	}
	if runMusic {
		return runMusicDashboard(dev, sigChan)
	}

	// Configure sensors
	var enabledSensors map[string]bool
	if len(runSensors) > 0 {
		// If --sensors flag is provided, only enable those sensors
		enabledSensors = make(map[string]bool)
		for _, s := range runSensors {
			enabledSensors[s] = true
		}
	} else if cfg.EnabledSensors != nil {
		enabledSensors = make(map[string]bool, len(cfg.EnabledSensors))
		for _, sensor := range cfg.EnabledSensors {
			enabledSensors[sensor] = true
		}
	}
	// If no --sensors flag, enabledSensors stays nil (all enabled)

	// Start with sensor options from config, then override with CLI flags
	var options map[string]interface{}
	if cfg.SensorOptions != nil {
		options = make(map[string]interface{})
		for k, v := range cfg.SensorOptions {
			options[k] = v
		}
	}

	// Parse sensor options from --opt flags (override config)
	if len(runOpts) > 0 {
		if options == nil {
			options = make(map[string]interface{})
		}
		for _, opt := range runOpts {
			key, value, ok := strings.Cut(opt, "=")
			if !ok {
				return fmt.Errorf("invalid option format %q (expected key=value)", opt)
			}
			// If value contains commas, treat it as a string slice
			if strings.Contains(value, ",") {
				options[key] = strings.Split(value, ",")
			} else {
				options[key] = value
			}
		}
	}

	sensorConfig := &sensors.Config{
		EnabledSensors:  enabledSensors,
		DisabledSensors: append(append([]string(nil), cfg.DisabledSensors...), runExcludeSensors...),
		Options:         options,
	}
	collector := sensors.NewCollector(sensorConfig)

	// Check if a theme is selected
	themeName := cfg.Theme
	if themeName != "" {
		return runWithTheme(dev, collector, cfg, themeName, sigChan)
	}

	// No theme selected - use built-in renderer
	return runWithBuiltinRenderer(dev, collector, sigChan)
}

func runMusicDashboard(dev *panel.Device, sigChan chan os.Signal) error {
	if _, err := exec.LookPath("playerctl"); err != nil {
		return fmt.Errorf("music dashboard requires playerctl: %w", err)
	}
	if err := ensureBrowserAvailable(); err != nil {
		return err
	}

	dashboardDir, err := os.MkdirTemp("", "sensorview-music-*")
	if err != nil {
		return fmt.Errorf("create music dashboard: %w", err)
	}
	defer os.RemoveAll(dashboardDir)
	if err := os.WriteFile(filepath.Join(dashboardDir, "index.html"), music.DashboardHTML, 0o600); err != nil {
		return fmt.Errorf("write music dashboard: %w", err)
	}

	width, height := dev.RenderWidth(), dev.RenderHeight()
	browserRenderer, err := browser.NewRenderer(width, height)
	if err != nil {
		return fmt.Errorf("initialize music dashboard browser: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := browserRenderer.Start(ctx, dashboardDir); err != nil {
		return fmt.Errorf("start music dashboard browser: %w", err)
	}
	defer browserRenderer.Stop()

	monitor := music.NewMonitor()
	monitor.Start(ctx)
	frameUpdater := panel.NewFrameUpdater(dev)
	fmt.Printf("Music dashboard running (%.2fs interval). Press Ctrl+C to stop.\n", runInterval)

	ticker := time.NewTicker(time.Duration(runInterval * float64(time.Second)))
	defer ticker.Stop()
	firstFrame := true
	for {
		if err := renderMusicFrame(dev, frameUpdater, monitor, browserRenderer, firstFrame); err != nil {
			fmt.Printf("Music frame error: %v\n", err)
		}
		firstFrame = false
		select {
		case <-sigChan:
			fmt.Println("\nStopping music dashboard...")
			return nil
		case <-ticker.C:
		}
	}
}

func renderMusicFrame(dev *panel.Device, frameUpdater *panel.FrameUpdater, monitor *music.Monitor, browserRenderer *browser.Renderer, firstFrame bool) error {
	data := map[string]interface{}{"music": monitor.Snapshot()}
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("encode music state: %w", err)
	}
	if err := browserRenderer.SendSensorData(string(jsonData)); err != nil {
		return fmt.Errorf("update music dashboard: %w", err)
	}
	if firstFrame {
		time.Sleep(150 * time.Millisecond)
	}
	img, err := browserRenderer.Capture()
	if err != nil {
		return fmt.Errorf("capture music dashboard: %w", err)
	}
	if _, err := frameUpdater.Display(dev.Profile.ConvertImage(img)); err != nil {
		return fmt.Errorf("display music dashboard: %w", err)
	}
	return nil
}

// runStaticImage displays one image and keeps it on screen until interrupted.
func runStaticImage(dev *panel.Device, source string, sigChan chan os.Signal) error {
	img, err := animation.LoadImage(source, dev.RenderWidth(), dev.RenderHeight())
	if err != nil {
		return err
	}

	if err := dev.DisplayBuffer(dev.Profile.ConvertImage(img)); err != nil {
		return fmt.Errorf("display image: %w", err)
	}

	fmt.Printf("Displaying image: %s. Press Ctrl+C to stop.\n", source)
	<-sigChan
	fmt.Println("\nStopping image display...")
	return nil
}

// runGIFPlayback displays an animated GIF continuously at its encoded frame rate.
func runGIFPlayback(dev *panel.Device, path string, sigChan chan os.Signal) error {
	animation, err := animation.LoadGIF(path, dev.RenderWidth(), dev.RenderHeight())
	if err != nil {
		return err
	}

	fmt.Printf("Playing GIF: %s (%d frames). Press Ctrl+C to stop.\n", path, len(animation.Frames))
	buffers := make([][]byte, len(animation.Frames))
	for i, image := range animation.Frames {
		buffers[i] = dev.Profile.ConvertImage(image)
	}

	// The panel applies each regional command independently and has no frame
	// commit/vsync operation. Keep GIF frames to one write so parts of
	// different animation frames are not visible at the same time.
	frameUpdater := panel.NewCoherentFrameUpdater(dev)
	frame := 0
	nextDeadline := time.Now()
	started := time.Now()
	displayedFrames := 0
	skippedFrames := 0
	bytesSent := 0
	regionalWrites := 0
	fullFrames := 0
	for {
		stats, err := frameUpdater.Display(buffers[frame])
		if err != nil {
			return fmt.Errorf("display GIF frame: %w", err)
		}
		displayedFrames++
		bytesSent += stats.Bytes
		regionalWrites += stats.Regions
		if stats.FullFrame {
			fullFrames++
		}

		nextDeadline = nextDeadline.Add(animation.Delays[frame])
		nextFrame := (frame + 1) % len(animation.Frames)
		for !nextDeadline.After(time.Now()) {
			skippedFrames++
			nextDeadline = nextDeadline.Add(animation.Delays[nextFrame])
			nextFrame = (nextFrame + 1) % len(animation.Frames)
		}

		timer := time.NewTimer(time.Until(nextDeadline))
		select {
		case <-sigChan:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			elapsed := time.Since(started)
			fmt.Printf("\nGIF performance: %.2f FPS, %.1f KB/s, %d displayed, %d skipped, %d regional writes, %d full frames\n",
				float64(displayedFrames)/elapsed.Seconds(),
				float64(bytesSent)/elapsed.Seconds()/1024,
				displayedFrames, skippedFrames, regionalWrites, fullFrames)
			fmt.Println("\nStopping GIF playback...")
			return nil
		case <-timer.C:
			frame = nextFrame
		}
	}
}

// runWithTheme runs the dashboard using a theme with headless browser rendering.
func runWithTheme(dev *panel.Device, collector *sensors.Collector, cfg *config.Config, themeName string, sigChan chan os.Signal) error {
	// Load the theme
	t, err := theme.Load(themeName)
	if err != nil {
		return fmt.Errorf("failed to load theme '%s': %w", themeName, err)
	}

	mode := runRenderer
	if mode == "" {
		mode = cfg.Renderer
	}
	mode, err = config.NormalizeRenderer(mode)
	if err != nil {
		return err
	}
	if mode == config.RendererAuto {
		if t.HasNative {
			mode = config.RendererNative
		} else {
			mode = config.RendererChrome
		}
	}

	switch mode {
	case config.RendererNative:
		if !t.HasNative {
			return fmt.Errorf("theme '%s' has no native.theme.json; use --renderer chrome or add a native theme definition", themeName)
		}
		return runWithNativeTheme(dev, collector, cfg, t, sigChan)
	case config.RendererChrome:
		return runWithChromeTheme(dev, collector, t, sigChan)
	default:
		return fmt.Errorf("unsupported renderer %q", mode)
	}
}

// runWithChromeTheme runs the dashboard using a theme with headless browser rendering.
func runWithChromeTheme(dev *panel.Device, collector *sensors.Collector, t *theme.Theme, sigChan chan os.Signal) error {
	if !t.HasDist {
		return fmt.Errorf("theme '%s' is not built (missing dist/index.html)\nRun 'cd %s && npm install && npm run build' to build it", t.Name, t.Path)
	}

	// Check if theme is outdated
	if theme.IsOutdated(t.Path) {
		fmt.Printf("Warning: Theme '%s' may be outdated (src/ is newer than dist/)\n", t.Name)
		fmt.Printf("Run 'sensorview theme build %s' to rebuild\n\n", t.Name)
	}

	fmt.Printf("Using theme: %s (renderer: chrome)\n", t.Name)

	// Check if browser is available, auto-download if not
	if err := ensureBrowserAvailable(); err != nil {
		return err
	}

	// Start the theme server with WebSocket support
	srv := server.New(t.DistDir())
	if err := srv.Start(); err != nil {
		return fmt.Errorf("failed to start theme server: %w", err)
	}
	defer srv.Stop()

	fmt.Printf("Theme server running at %s\n", srv.URL())

	// Use device profile dimensions, or fall back to theme metadata
	width := dev.RenderWidth()
	height := dev.RenderHeight()
	if dev.Profile.ProtocolType() != device.ProtocolLYBulk && t.Metadata.Width > 0 && t.Metadata.Height > 0 {
		// Theme specifies its own dimensions - use those for browser rendering
		width = t.Metadata.Width
		height = t.Metadata.Height
	}

	// Start headless browser
	browserRenderer, err := browser.NewRenderer(width, height)
	if err != nil {
		return fmt.Errorf("failed to initialize browser: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := browserRenderer.Start(ctx, t.DistDir()); err != nil {
		return fmt.Errorf("failed to start browser: %w", err)
	}
	defer browserRenderer.Stop()

	fmt.Printf("Dashboard running with theme (%.1fs interval). Press Ctrl+C to stop.\n", runInterval)

	// Initial collection to prime CPU load calculation
	collector.CollectAll()
	time.Sleep(100 * time.Millisecond)

	// Main loop
	ticker := time.NewTicker(time.Duration(runInterval * float64(time.Second)))
	defer ticker.Stop()

	frameCount := 0
	startTime := time.Now()
	frameUpdater := panel.NewFrameUpdater(dev)
	frameState := &themeFrameState{}

	// Render first frame immediately
	if err := renderThemeFrame(dev, frameUpdater, collector, srv, browserRenderer, frameState, &frameCount); err != nil {
		fmt.Printf("Frame error: %v\n", err)
	}

	for {
		select {
		case <-sigChan:
			fmt.Println("\nShutting down...")
			elapsed := time.Since(startTime).Seconds()
			if elapsed > 0 {
				fps := float64(frameCount) / elapsed
				fmt.Printf("Rendered %d frames in %.1fs (%.2f FPS)\n", frameCount, elapsed, fps)
			}
			return nil

		case <-ticker.C:
			if err := renderThemeFrame(dev, frameUpdater, collector, srv, browserRenderer, frameState, &frameCount); err != nil {
				fmt.Printf("Frame error: %v\n", err)
			}
		}
	}
}

type nativeRuntime struct {
	definition    *nativerender.Theme
	renderer      *nativerender.Renderer
	activeFPS     float64
	idleFPS       float64
	idleTimeout   time.Duration
	jpegQuality   int
	effectiveJPEG string
}

func loadNativeRuntime(dev *panel.Device, t *theme.Theme) (*nativeRuntime, error) {
	nativeTheme, err := nativerender.Load(t.NativePath())
	if err != nil {
		return nil, fmt.Errorf("failed to load native theme '%s': %w", t.Name, err)
	}
	if err := nativeTheme.Validate(); err != nil {
		return nil, fmt.Errorf("validate native theme '%s': %w", t.Name, err)
	}
	logicalWidth, logicalHeight := dev.RenderWidth(), dev.RenderHeight()
	themeWidth, themeHeight := nativeTheme.Width, nativeTheme.Height
	if nativeTheme.Canvas != nil {
		themeWidth, themeHeight = nativeTheme.Canvas.Width, nativeTheme.Canvas.Height
	}
	if themeWidth > 0 && themeHeight > 0 &&
		(themeWidth != logicalWidth || themeHeight != logicalHeight) {
		return nil, fmt.Errorf(
			"native theme %q canvas is %dx%d, but display orientation %d requires %dx%d; choose an orientation matching the theme",
			t.Name, themeWidth, themeHeight, dev.Orientation, logicalWidth, logicalHeight)
	}
	render := nativerender.New(nativeTheme, logicalWidth, logicalHeight)
	if dev.Profile.ProtocolType() == device.ProtocolLYBulk {
		if err := render.ConfigureWire(dev.Profile.Width(), dev.Profile.Height(), 180-dev.Orientation); err != nil {
			_ = render.Close()
			return nil, err
		}
	}
	if err := render.LoadBackgroundSequence(t.Path); err != nil {
		_ = render.Close()
		return nil, fmt.Errorf("load native background: %w", err)
	}
	if err := render.LoadAssets(t.Path); err != nil {
		_ = render.Close()
		return nil, fmt.Errorf("load native assets: %w", err)
	}

	targetFPS := render.PreferredFPS()
	if nativeTheme.Performance != nil && nativeTheme.Performance.TargetFPS > 0 {
		targetFPS = nativeTheme.Performance.TargetFPS
	}
	activeFPS, idleFPS := targetFPS, targetFPS
	idleTimeout := 20 * time.Second
	if nativeTheme.Performance != nil {
		if nativeTheme.Performance.ActiveFPS > 0 {
			activeFPS = nativeTheme.Performance.ActiveFPS
		}
		if nativeTheme.Performance.IdleFPS > 0 {
			idleFPS = nativeTheme.Performance.IdleFPS
		}
		idleTimeout = time.Duration(nativeTheme.Performance.IdleTimeoutSeconds) * time.Second
	}
	if runTargetFPS > 0 {
		activeFPS, idleFPS = runTargetFPS, runTargetFPS
	}
	if sourceFPS := render.PreferredFPS(); sourceFPS > 0 {
		if activeFPS > sourceFPS {
			activeFPS = sourceFPS
		}
		if idleFPS > sourceFPS {
			idleFPS = sourceFPS
		}
	}
	// A static V2 scene changes only when a sensor sample changes. Rendering it
	// at animation cadence wastes CPU and USB bandwidth without producing a
	// different frame.
	if render.BackgroundFrameCount() <= 1 && runTargetFPS <= 0 {
		staticFPS := 1 / themeSensorInterval.Seconds()
		if staticFPS < 1 {
			staticFPS = 1
		}
		if activeFPS > staticFPS {
			activeFPS = staticFPS
		}
		if idleFPS > staticFPS {
			idleFPS = staticFPS
		}
	}
	if activeFPS <= 0 {
		activeFPS = 1
	}
	if idleFPS <= 0 {
		idleFPS = activeFPS
	}
	jpegQuality := 80
	jpegEncoder := runJPEGEncoder
	if nativeTheme.Performance != nil {
		jpegQuality = nativeTheme.Performance.JPEGQuality
		if jpegEncoder == "auto" {
			jpegEncoder = nativeTheme.Performance.JPEGEncoder
		}
	}
	if runJPEGQuality > 0 {
		jpegQuality = runJPEGQuality
	}
	if err := dev.SetJPEGOptions(jpegQuality, jpegEncoder); err != nil {
		_ = render.Close()
		return nil, err
	}
	effectiveJPEG, err := dev.PrepareJPEGEncoder()
	if err != nil {
		_ = render.Close()
		return nil, err
	}
	return &nativeRuntime{
		definition:    nativeTheme,
		renderer:      render,
		activeFPS:     activeFPS,
		idleFPS:       idleFPS,
		idleTimeout:   idleTimeout,
		jpegQuality:   jpegQuality,
		effectiveJPEG: effectiveJPEG,
	}, nil
}

func (r *nativeRuntime) Close() {
	if r != nil && r.renderer != nil {
		_ = r.renderer.Close()
	}
}

type nativeReloadRequest struct {
	result chan error
}

type nativeConfigRequest struct {
	config *config.Config
	result chan error
}

// runWithNativeTheme runs the dashboard using a native Go renderer and hosts
// the optional management Studio beside the USB render loop.
func runWithNativeTheme(dev *panel.Device, collector *sensors.Collector, cfg *config.Config, t *theme.Theme, sigChan chan os.Signal) error {
	runtimeState, err := loadNativeRuntime(dev, t)
	if err != nil {
		return err
	}
	defer func() { runtimeState.Close() }()
	render := runtimeState.renderer
	activeFPS, idleFPS, idleTimeout := runtimeState.activeFPS, runtimeState.idleFPS, runtimeState.idleTimeout

	fmt.Printf("Using theme: %s (renderer: native)\n", t.Name)
	if frames := render.BackgroundFrameCount(); frames > 0 {
		fmt.Printf("Native background sequence: %d frames at %.1f FPS (active %.1f, idle %.1f after %s, decode %s, JPEG %s/%d)\n",
			frames, render.PreferredFPS(), activeFPS, idleFPS, idleTimeout,
			render.BackgroundDecoder(), runtimeState.effectiveJPEG, runtimeState.jpegQuality)
	}
	fmt.Printf("Dashboard running (adaptive %.1f/%.1f FPS, %.2fs sensor interval). Press Ctrl+C to stop.\n", activeFPS, idleFPS, themeSensorInterval.Seconds())

	reloadRequests := make(chan nativeReloadRequest)
	configRequests := make(chan nativeConfigRequest)
	var activeThemeName atomic.Value
	activeThemeName.Store(t.Name)
	var effectiveConfig atomic.Value
	initialConfig := *cfg
	initialConfig.Theme = t.Name
	initialConfig.Brightness = runBrightness
	initialConfig.Orientation = dev.Orientation
	initialConfig.UpdateInterval = themeSensorInterval.Seconds()
	effectiveConfig.Store(&initialConfig)
	if runManagement {
		manager, managerErr := management.New(management.Options{
			Address:     runManagementAddr,
			Collector:   collector,
			ActiveTheme: func() string { return activeThemeName.Load().(string) },
			CurrentConfig: func() *config.Config {
				current := effectiveConfig.Load().(*config.Config)
				snapshot := *current
				return &snapshot
			},
			Status: func() any {
				width, height := dev.RenderDimensions()
				return map[string]any{
					"state": "running", "theme": activeThemeName.Load().(string), "renderer": "native",
					"width": width, "height": height,
					"device": dev.Info.String(),
				}
			},
			ApplyTheme: func(name string) error {
				if name != activeThemeName.Load().(string) {
					return fmt.Errorf("select theme %q before applying it", name)
				}
				response := make(chan error, 1)
				select {
				case reloadRequests <- nativeReloadRequest{result: response}:
				case <-time.After(10 * time.Second):
					return fmt.Errorf("renderer reload timed out")
				}
				return <-response
			},
			ApplyConfig: func(updated *config.Config) error {
				response := make(chan error, 1)
				select {
				case configRequests <- nativeConfigRequest{config: updated, result: response}:
				case <-time.After(10 * time.Second):
					return fmt.Errorf("panel settings reload timed out")
				}
				return <-response
			},
		})
		if managerErr != nil {
			fmt.Printf("Warning: management Studio unavailable: %v\n", managerErr)
		} else if managerErr = manager.Start(); managerErr != nil {
			fmt.Printf("Warning: management Studio unavailable: %v\n", managerErr)
		} else {
			defer manager.Close()
			fmt.Printf("Management Studio: %s\n", manager.URL())
		}
	}

	collector.CollectAll()
	time.Sleep(100 * time.Millisecond)

	frameCount := 0
	startTime := time.Now()
	state := &themeFrameState{forceDisplay: true}
	activityMonitor := activity.New(time.Second)
	defer activityMonitor.Close()
	var idleSince time.Time
	mode := ""
	nextFrame := time.Now()
	timer := time.NewTimer(0)
	defer timer.Stop()
	resetTimer := func(deadline time.Time) {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		wait := time.Until(deadline)
		if wait < 0 {
			wait = 0
		}
		timer.Reset(wait)
	}

	for {
		select {
		case <-sigChan:
			fmt.Println("\nShutting down...")
			elapsed := time.Since(startTime).Seconds()
			if elapsed > 0 {
				fps := float64(frameCount) / elapsed
				fmt.Printf("Presented %d frames in %.1fs (%.2f FPS)\n", frameCount, elapsed, fps)
			}
			return nil
		case <-activityMonitor.Events():
			idleSince = time.Time{}
			state.forceDisplay = true
			nextFrame = time.Now()
			resetTimer(nextFrame)
		case request := <-reloadRequests:
			reloaded, reloadErr := loadNativeRuntime(dev, t)
			if reloadErr == nil {
				old := runtimeState
				runtimeState = reloaded
				render = reloaded.renderer
				activeFPS, idleFPS, idleTimeout = reloaded.activeFPS, reloaded.idleFPS, reloaded.idleTimeout
				state = &themeFrameState{forceDisplay: true}
				nextFrame = time.Now()
				old.Close()
				fmt.Printf("Native theme reloaded from Studio (active %.1f, idle %.1f FPS)\n", activeFPS, idleFPS)
			}
			request.result <- reloadErr
			resetTimer(nextFrame)
			continue
		case request := <-configRequests:
			oldBrightness := cfg.Brightness
			applyErr := dev.SetBacklight(request.config.Brightness)
			oldOrientation := dev.Orientation
			oldSensorInterval := themeSensorInterval
			targetTheme := t
			needsReload := request.config.Orientation != dev.Orientation
			if request.config.UpdateInterval > 0 {
				nextInterval := time.Duration(max(0.5, request.config.UpdateInterval) * float64(time.Second))
				if nextInterval != themeSensorInterval {
					themeSensorInterval = nextInterval
					needsReload = true
				}
			}
			if applyErr == nil && request.config.Theme != "" && request.config.Theme != t.Name {
				targetTheme, applyErr = theme.Load(request.config.Theme)
				if applyErr == nil && !targetTheme.HasNative {
					applyErr = fmt.Errorf("theme %q has no native definition", request.config.Theme)
				}
				needsReload = true
			}
			if applyErr == nil && request.config.Orientation != dev.Orientation {
				applyErr = dev.SetOrientation(request.config.Orientation)
			}
			if applyErr == nil && needsReload {
				reloaded, reloadErr := loadNativeRuntime(dev, targetTheme)
				applyErr = reloadErr
				if reloadErr == nil {
					old := runtimeState
					runtimeState = reloaded
					render = reloaded.renderer
					activeFPS, idleFPS, idleTimeout = reloaded.activeFPS, reloaded.idleFPS, reloaded.idleTimeout
					state = &themeFrameState{forceDisplay: true}
					t = targetTheme
					activeThemeName.Store(t.Name)
					old.Close()
				} else {
					_ = dev.SetOrientation(oldOrientation)
					themeSensorInterval = oldSensorInterval
				}
			}
			if applyErr == nil {
				var enabled map[string]bool
				if request.config.EnabledSensors != nil {
					enabled = make(map[string]bool, len(request.config.EnabledSensors))
					for _, sensor := range request.config.EnabledSensors {
						enabled[sensor] = true
					}
				}
				collector.Reconfigure(&sensors.Config{
					EnabledSensors: enabled, DisabledSensors: request.config.DisabledSensors,
					Options: request.config.SensorOptions,
				})
				cfg = request.config
				snapshot := *request.config
				snapshot.Theme = t.Name
				snapshot.Orientation = dev.Orientation
				snapshot.UpdateInterval = themeSensorInterval.Seconds()
				effectiveConfig.Store(&snapshot)
			} else {
				themeSensorInterval = oldSensorInterval
				if dev.Orientation != oldOrientation {
					_ = dev.SetOrientation(oldOrientation)
				}
				_ = dev.SetBacklight(oldBrightness)
			}
			request.result <- applyErr
			nextFrame = time.Now()
			resetTimer(nextFrame)
			continue
		case <-timer.C:
		}

		now := time.Now()
		idle := activityMonitor.Idle()
		if idle {
			if idleSince.IsZero() {
				idleSince = now
			}
		} else {
			idleSince = time.Time{}
		}
		fps := activeFPS
		currentMode := "active"
		if !idleSince.IsZero() && now.Sub(idleSince) >= idleTimeout {
			fps = idleFPS
			currentMode = "idle"
		}
		if currentMode != mode {
			mode = currentMode
			state.forceDisplay = true
			fmt.Printf("Native performance mode: %s (%.1f FPS)\n", mode, fps)
		}
		if _, err := renderNativeThemeFrame(dev, collector, render, state, &frameCount, currentMode == "idle"); err != nil {
			fmt.Printf("Frame error: %v\n", err)
		}
		frameDelay := time.Duration(float64(time.Second) / fps)
		nextFrame = nextFrame.Add(frameDelay)
		finished := time.Now()
		if nextFrame.Before(finished) {
			missed := finished.Sub(nextFrame)/frameDelay + 1
			nextFrame = nextFrame.Add(missed * frameDelay)
		}
		resetTimer(nextFrame)
	}
}

// runWithBuiltinRenderer runs the dashboard using the built-in bitmap renderer.
func runWithBuiltinRenderer(dev *panel.Device, collector *sensors.Collector, sigChan chan os.Signal) error {
	// Configure renderer with device profile dimensions
	renderConfig := &renderer.Config{
		Width:  dev.RenderWidth(),
		Height: dev.RenderHeight(),
	}
	render := renderer.New(renderConfig)

	fmt.Printf("Dashboard running (%.1fs interval). Press Ctrl+C to stop.\n", runInterval)

	// Initial collection to prime CPU load calculation
	collector.CollectAll()
	time.Sleep(100 * time.Millisecond)

	// Main loop
	ticker := time.NewTicker(time.Duration(runInterval * float64(time.Second)))
	defer ticker.Stop()

	frameCount := 0
	startTime := time.Now()
	frameUpdater := panel.NewFrameUpdater(dev)

	// Render first frame immediately
	renderFrame(dev, frameUpdater, collector, render, &frameCount)

	for {
		select {
		case <-sigChan:
			fmt.Println("\nShutting down...")
			elapsed := time.Since(startTime).Seconds()
			if elapsed > 0 {
				fps := float64(frameCount) / elapsed
				fmt.Printf("Rendered %d frames in %.1fs (%.2f FPS)\n", frameCount, elapsed, fps)
			}
			return nil

		case <-ticker.C:
			renderFrame(dev, frameUpdater, collector, render, &frameCount)
		}
	}
}

func renderFrame(dev *panel.Device, frameUpdater *panel.FrameUpdater, collector *sensors.Collector, render *renderer.Renderer, frameCount *int) {
	// Collect sensor data
	data := collector.CollectAll()

	// Render to image
	img := render.Render(data)

	// Convert to RGB565 and send to display
	buffer := dev.Profile.ConvertImage(img)
	if _, err := frameUpdater.Display(buffer); err != nil {
		fmt.Printf("Display error: %v\n", err)
		return
	}

	*frameCount++
}

type themeFrameState struct {
	data              map[string]interface{}
	lastSensorUpdate  time.Time
	overlay           *image.RGBA
	canvas            *image.RGBA
	lowPower          *image.RGBA
	overlaySignature  string
	lowPowerSignature string
	viewSignature     string
	signatureMinute   int
	lastIdleSent      time.Time
	idleMode          bool
	forceDisplay      bool
	lastTiming        nativeFrameTiming
}

type nativeFrameTiming struct {
	Sensor     time.Duration
	Overlay    time.Duration
	Background time.Duration
	Composite  time.Duration
	Display    panel.DisplayMetrics
	Total      time.Duration
	Skipped    bool
}

// renderThemeFrame updates sensor data at a slower cadence, then captures and sends a rendered frame.
func renderThemeFrame(dev *panel.Device, frameUpdater *panel.FrameUpdater, collector *sensors.Collector, srv *server.Server, browserRenderer *browser.Renderer, state *themeFrameState, frameCount *int) error {
	now := time.Now()
	shouldUpdateSensors := state.data == nil || now.Sub(state.lastSensorUpdate) >= themeSensorInterval
	if shouldUpdateSensors {
		state.data = collector.CollectAll()
		state.lastSensorUpdate = now

		// Broadcast sensor data to theme via WebSocket
		if err := srv.BroadcastSensorData(state.data); err != nil {
			// Non-fatal: theme might not have connected yet
			_ = err
		}

		// Also inject via postMessage for themes that use that method
		jsonBytes, err := json.Marshal(state.data)
		if err == nil {
			_ = browserRenderer.SendSensorData(string(jsonBytes))
		}
	}

	// Give the theme a moment to render (if first frame)
	// This is only needed for initial load
	if *frameCount == 0 {
		time.Sleep(100 * time.Millisecond)
	}

	// Capture screenshot from browser
	img, err := browserRenderer.Capture()
	if err != nil {
		return fmt.Errorf("capture failed: %w", err)
	}

	// Convert to RGB565 and send to display
	buffer := dev.Profile.ConvertImage(img)
	if _, err := frameUpdater.Display(buffer); err != nil {
		return fmt.Errorf("display failed: %w", err)
	}

	*frameCount++
	return nil
}

func renderNativeThemeFrame(dev *panel.Device, collector *sensors.Collector, render *nativerender.Renderer, state *themeFrameState, frameCount *int, lowPower ...bool) (bool, error) {
	totalStarted := time.Now()
	timing := nativeFrameTiming{}
	now := time.Now()
	idleMode := len(lowPower) > 0 && lowPower[0]
	if idleMode != state.idleMode {
		state.idleMode = idleMode
		state.forceDisplay = true
		if idleMode {
			// The active canvas is reused for the idle frame. Force a redraw
			// because active playback may have overwritten its previous pixels.
			state.lowPowerSignature = ""
		}
	}
	started := time.Now()
	var collected bool
	state.data, collected = collector.CollectScheduled(now, idleMode)
	timing.Sensor = time.Since(started)
	if collected {
		render.RecordSnapshot(state.data, now)
	}
	minuteKey := now.YearDay()*24*60 + now.Hour()*60 + now.Minute()
	if collected || state.viewSignature == "" || state.signatureMinute != minuteKey {
		state.viewSignature = render.ViewSignature(state.data, now)
		state.signatureMinute = minuteKey
	}
	signature := state.viewSignature
	if idleMode {
		changed := false
		if state.lowPower == nil {
			if state.canvas == nil {
				state.canvas = image.NewRGBA(image.Rect(0, 0, render.OutputWidth(), render.OutputHeight()))
			}
			state.lowPower = state.canvas
		}
		if state.lowPowerSignature != signature {
			started = time.Now()
			render.RenderLowPowerInto(state.lowPower, state.data, now)
			timing.Overlay = time.Since(started)
			state.lowPowerSignature = signature
			changed = true
		}
		// The panel firmware restores its Thermalright splash screen when the
		// host remains silent. Keep presenting the cached monochrome frame at
		// the idle scheduler cadence (1 FPS by default) without redrawing it.
		if !state.forceDisplay && !changed && now.Sub(state.lastIdleSent) < nativeIdleKeepaliveWindow {
			timing.Skipped = true
			timing.Total = time.Since(totalStarted)
			state.lastTiming = timing
			return false, nil
		}
		display, err := displayNativeImage(dev, render, state.lowPower)
		timing.Display = display
		timing.Total = time.Since(totalStarted)
		state.lastTiming = timing
		if err != nil {
			return false, fmt.Errorf("display failed: %w", err)
		}
		state.lastIdleSent = now
		state.forceDisplay = false
		*frameCount++
		return true, nil
	}

	if state.overlay == nil {
		state.overlay = image.NewRGBA(image.Rect(0, 0, render.OutputWidth(), render.OutputHeight()))
	}
	if state.overlaySignature != signature {
		started = time.Now()
		render.RenderOutputOverlayInto(state.overlay, state.data, now)
		timing.Overlay = time.Since(started)
		state.overlaySignature = signature
	}

	if state.canvas == nil {
		state.canvas = image.NewRGBA(image.Rect(0, 0, render.OutputWidth(), render.OutputHeight()))
	}
	started = time.Now()
	render.RenderBackgroundInto(state.canvas, now)
	timing.Background = time.Since(started)
	if state.overlay != nil {
		started = time.Now()
		render.Composite(state.canvas, state.overlay)
		timing.Composite = time.Since(started)
	}
	display, err := displayNativeImage(dev, render, state.canvas)
	timing.Display = display
	timing.Total = time.Since(totalStarted)
	state.lastTiming = timing
	if err != nil {
		return false, fmt.Errorf("display failed: %w", err)
	}

	state.forceDisplay = false
	*frameCount++
	return true, nil
}

func displayNativeImage(dev *panel.Device, render *nativerender.Renderer, img *image.RGBA) (panel.DisplayMetrics, error) {
	if render.WireOutput() {
		return dev.DisplayWireImageTimed(img)
	}
	started := time.Now()
	err := dev.DisplayImage(img)
	return panel.DisplayMetrics{USBWrite: time.Since(started)}, err
}

// ensureBrowserAvailable checks if a browser is available and downloads one if needed.
func ensureBrowserAvailable() error {
	// Check if browser is already available
	if _, err := browser.GetChromePath(); err == nil {
		return nil
	}

	// Browser not found - auto-download
	fmt.Println("Browser not found. Downloading Chrome for Testing...")
	fmt.Printf("Version: %s\n", browser.Version())

	var lastPercent int
	err := browser.Download(context.Background(), func(downloaded, total int64) {
		if total > 0 {
			percent := int(downloaded * 100 / total)
			// Only print every 10%
			if percent/10 > lastPercent/10 {
				lastPercent = percent
				fmt.Printf("  %d%% (%d / %d MB)\n", percent, downloaded/(1024*1024), total/(1024*1024))
			}
		}
	})

	if err != nil {
		return fmt.Errorf("failed to download browser: %w", err)
	}

	chromePath, err := browser.GetChromePath()
	if err != nil {
		return fmt.Errorf("browser download succeeded but not found: %w", err)
	}

	fmt.Printf("Browser installed: %s\n", chromePath)
	return nil
}
