package theme

import (
	"strings"
	"testing"
)

func TestViteConfigBindsEveryInterface(t *testing.T) {
	t.Parallel()

	// A theme run with a plain "npm run dev", outside sensorview theme dev,
	// still has to be reachable from a phone on the LAN.
	config := viteConfigTS()

	if !strings.Contains(config, "host: true") {
		t.Errorf("generated vite config has no host setting:\n%s", config)
	}
}

func TestViteConfigKeepsItsPort(t *testing.T) {
	t.Parallel()

	// The theme SDK probes for the sensor server relative to the page, and the
	// documented phone URL names this port.
	config := viteConfigTS()

	if !strings.Contains(config, "port: 15173") {
		t.Errorf("generated vite config lost its port:\n%s", config)
	}
}

func TestGeneratedThemeFilesIncludeViteConfig(t *testing.T) {
	t.Parallel()

	files := getTemplateFiles("example")

	config, ok := files["vite.config.ts"]
	if !ok {
		t.Fatalf("generated files %v have no vite.config.ts", keysOf(files))
	}
	if !strings.Contains(config, "host: true") {
		t.Error("the generated vite.config.ts does not bind every interface")
	}
}

func keysOf(files map[string]string) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	return names
}

// A theme opened on a phone used to show a clipped corner it could not scroll,
// because the shell pinned the viewport to the 480x320 USB panel and hid the
// overflow. Both generated shells must now use the device viewport and scale
// the panel canvas to fit it.
func TestGeneratedShellsScaleToTheViewport(t *testing.T) {
	t.Parallel()

	shells := map[string]string{
		"index.html":      indexHTML("demo"),
		"dist/index.html": distIndexHTML("demo"),
	}

	for name, html := range shells {
		t.Run(name, func(t *testing.T) {
			if strings.Contains(html, "width=480") {
				t.Errorf("%s still pins the viewport to the panel width:\n%s", name, html)
			}
			if !strings.Contains(html, "width=device-width") {
				t.Errorf("%s does not use the device viewport", name)
			}
			if !strings.Contains(html, "--panel-scale") {
				t.Errorf("%s never scales the panel canvas", name)
			}
			if !strings.Contains(html, "window.innerWidth") {
				t.Errorf("%s has no script to compute the scale", name)
			}
			// Grid and flex clamp an oversized item to the start edge rather than
			// centring it, so the panel must be centred with fixed positioning.
			if !strings.Contains(html, "translate(-50%, -50%)") {
				t.Errorf("%s does not centre the panel by translation", name)
			}
			if !strings.Contains(html, "position: fixed") {
				t.Errorf("%s does not position the panel independently of layout", name)
			}
		})
	}
}

// The panel canvas itself keeps its authored size; only the shell around it
// changes. A theme's layout is written against these pixels.
func TestGeneratedShellsKeepTheAuthoredCanvas(t *testing.T) {
	t.Parallel()

	for name, html := range map[string]string{
		"index.html":      indexHTML("demo"),
		"dist/index.html": distIndexHTML("demo"),
	} {
		if !strings.Contains(html, "width: 480px") || !strings.Contains(html, "height: 320px") {
			t.Errorf("%s lost the authored 480x320 canvas:\n%s", name, html)
		}
	}
}

// The title is the theme's own name. Copying a shell between themes once left
// trofeo advertising itself as NeonPulse.
func TestGeneratedShellsUseTheThemeName(t *testing.T) {
	t.Parallel()

	if got := indexHTML("trofeo"); !strings.Contains(got, "<title>trofeo - SensorView</title>") {
		t.Errorf("index.html title is not the theme name:\n%s", got)
	}
	if got := distIndexHTML("trofeo"); !strings.Contains(got, "<title>trofeo - SensorView</title>") {
		t.Errorf("dist/index.html title is not the theme name:\n%s", got)
	}
}
