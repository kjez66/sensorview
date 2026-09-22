package cmd

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kjez66/sensorview/pkg/config"
	"github.com/kjez66/sensorview/pkg/theme"
)

// installedThemes mirrors the bundled themes: trofeo is web and native,
// neonpulse web only, caelestia native only.
var installedThemes = []theme.Theme{
	{Name: "neonpulse"},
	{Name: "trofeo", HasNative: true},
	{Name: "caelestia", HasNative: true},
}

func TestPickStudioThemeKeepsANativePreference(t *testing.T) {
	if got := pickStudioTheme("caelestia", installedThemes); got != "caelestia" {
		t.Errorf("pickStudioTheme = %q, want caelestia", got)
	}
}

// The Studio fails to load when its active theme is not native, so serving a
// web-only theme must still open it on a native one.
func TestPickStudioThemeSkipsAWebOnlyPreference(t *testing.T) {
	if got := pickStudioTheme("neonpulse", installedThemes); got != "trofeo" {
		t.Errorf("pickStudioTheme = %q, want the first native theme, trofeo", got)
	}
}

func TestPickStudioThemeFallsBackWhenThePreferenceIsMissing(t *testing.T) {
	for _, preferred := range []string{"", "deleted"} {
		if got := pickStudioTheme(preferred, installedThemes); got != "trofeo" {
			t.Errorf("pickStudioTheme(%q) = %q, want trofeo", preferred, got)
		}
	}
}

func TestPickStudioThemeEmptyWithoutNativeThemes(t *testing.T) {
	if got := pickStudioTheme("neonpulse", []theme.Theme{{Name: "neonpulse"}}); got != "" {
		t.Errorf("pickStudioTheme = %q, want empty", got)
	}
}

func TestStudioSettingsUsesTheConfigUnlessAFlagIsPassed(t *testing.T) {
	cfg := &config.Config{Management: &config.ManagementConfig{Enabled: false, Address: "127.0.0.1:20000"}}

	enabled, address := studioSettings(cfg, true, false, "", false)
	if enabled || address != "127.0.0.1:20000" {
		t.Errorf("from config: enabled=%v address=%q, want false and 127.0.0.1:20000", enabled, address)
	}

	enabled, address = studioSettings(cfg, true, true, "127.0.0.1:20001", true)
	if !enabled || address != "127.0.0.1:20001" {
		t.Errorf("from flags: enabled=%v address=%q, want true and 127.0.0.1:20001", enabled, address)
	}
}

func TestStudioSettingsDefaultsWithoutAConfig(t *testing.T) {
	enabled, address := studioSettings(nil, true, false, "", false)
	if !enabled || address != defaultStudioAddress {
		t.Errorf("enabled=%v address=%q, want true and %s", enabled, address, defaultStudioAddress)
	}
}

func TestStudioActiveThemeFollowsSet(t *testing.T) {
	active := newStudioActiveTheme("trofeo")
	active.set("caelestia")
	if got := active.get(); got != "caelestia" {
		t.Errorf("active = %q, want caelestia", got)
	}
}

func TestStudioRunningRecognisesTheStudio(t *testing.T) {
	studio := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/status" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"state":"running","renderer":"none"}`))
	}))
	defer studio.Close()

	if !studioRunning(studio.URL) {
		t.Error("studioRunning = false for a Studio status endpoint")
	}
}

// Something else holding the port must not be mistaken for the Studio, or ui
// would open an unrelated page instead of starting one.
func TestStudioRunningIgnoresOtherServers(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>something else</html>"))
	}))
	defer other.Close()

	if studioRunning(other.URL) {
		t.Error("studioRunning = true for a server that is not the Studio")
	}
}

func TestStudioRunningFalseWhenNothingListens(t *testing.T) {
	closed := httptest.NewServer(http.NotFoundHandler())
	url := closed.URL
	closed.Close()

	if studioRunning(url) {
		t.Error("studioRunning = true with nothing listening")
	}
}
