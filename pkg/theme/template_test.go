package theme

import (
	"strings"
	"testing"
)

func TestViteConfigBindsEveryInterface(t *testing.T) {
	t.Parallel()

	// A theme run with a plain "npm run dev", outside sensorpanel theme dev,
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
