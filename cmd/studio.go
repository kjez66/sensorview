package cmd

import (
	"sync"

	"github.com/kjez66/sensorview/pkg/config"
	"github.com/kjez66/sensorview/pkg/management"
	"github.com/kjez66/sensorview/pkg/sensors"
	"github.com/kjez66/sensorview/pkg/theme"
)

// Studio modes, reported in the status endpoint so the UI and logs can tell
// which process owns the Studio.
const (
	studioModeServe      = "serve"
	studioModeStandalone = "standalone"
)

// studioOptions configures a Studio started without a USB panel.
type studioOptions struct {
	address   string
	collector *sensors.Collector

	// preferredTheme is the theme to open, when it is a native one.
	preferredTheme string

	// mode and renderer are reported by the status endpoint.
	mode     string
	renderer string

	// applyTheme runs after Apply has validated and saved a theme, and may
	// redraw a display showing it. An error makes the Studio restore the
	// previous file. When nil, saving is all Apply does.
	applyTheme func(name string) error
}

// startStudio starts the Management Studio without a USB panel; run wires in
// its own with the panel reload.
//
// The Studio only edits native themes and fails to load if its active theme is
// not one, so the active theme is the preferred one when that is native and
// otherwise the first native theme installed.
func startStudio(options studioOptions) (*management.Server, error) {
	installed, err := theme.List()
	if err != nil {
		installed = nil
	}
	active := newStudioActiveTheme(pickStudioTheme(options.preferredTheme, installed))

	manager, err := management.New(management.Options{
		Address:     options.address,
		Collector:   options.collector,
		ActiveTheme: active.get,
		ApplyTheme:  options.applyTheme,
		Status: func() any {
			return map[string]any{
				"state": "running", "theme": active.get(),
				"renderer": options.renderer, "mode": options.mode,
			}
		},
		// Switching theme in the Studio saves the config; follow it so a page
		// reload opens the theme that was chosen rather than the initial one.
		ApplyConfig: func(updated *config.Config) error {
			if updated != nil && isNativeTheme(updated.Theme) {
				active.set(updated.Theme)
			}
			return nil
		},
	})
	if err != nil {
		return nil, err
	}
	if err := manager.Start(); err != nil {
		return nil, err
	}
	return manager, nil
}

// pickStudioTheme returns preferred when it is an installed native theme, else
// the first installed native theme, else an empty string.
func pickStudioTheme(preferred string, installed []theme.Theme) string {
	first := ""
	for _, candidate := range installed {
		if !candidate.HasNative {
			continue
		}
		if candidate.Name == preferred {
			return preferred
		}
		if first == "" {
			first = candidate.Name
		}
	}
	return first
}

// isNativeTheme reports whether name is an installed theme with a native
// definition.
func isNativeTheme(name string) bool {
	if name == "" {
		return false
	}
	loaded, err := theme.Load(name)
	return err == nil && loaded.HasNative
}

// studioActiveTheme is the theme the Studio opens, shared between its request
// handlers.
type studioActiveTheme struct {
	mu   sync.Mutex
	name string
}

func newStudioActiveTheme(name string) *studioActiveTheme {
	return &studioActiveTheme{name: name}
}

func (a *studioActiveTheme) get() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.name
}

func (a *studioActiveTheme) set(name string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.name = name
}

// studioSettings resolves whether and where to start the Studio, letting an
// explicitly passed flag override the config file. cfg may be nil when the
// config could not be read.
func studioSettings(cfg *config.Config, enabledFlag bool, enabledChanged bool, addressFlag string, addressChanged bool) (bool, string) {
	enabled, address := enabledFlag, addressFlag
	if cfg != nil && cfg.Management != nil {
		if !enabledChanged {
			enabled = cfg.Management.Enabled
		}
		if !addressChanged {
			address = cfg.Management.Address
		}
	}
	if address == "" {
		address = defaultStudioAddress
	}
	return enabled, address
}

// defaultStudioAddress matches the config default.
const defaultStudioAddress = "127.0.0.1:19848"
