package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kjez66/sensorview/pkg/display"
	"github.com/kjez66/sensorview/pkg/theme"
)

func TestServeRendererOrder(t *testing.T) {
	cases := []struct {
		requested string
		hasNative bool
		want      []string
	}{
		// auto prefers the version the Studio edits, as run does.
		{"auto", true, []string{"native", "web"}},
		{"", true, []string{"native", "web"}},
		{"auto", false, []string{"web"}},
		{"native", true, []string{"native"}},
		{"web", true, []string{"web"}},
		{"web", false, []string{"web"}},
	}
	for _, tc := range cases {
		got, err := serveRendererOrder(tc.requested, tc.hasNative)
		if err != nil {
			t.Errorf("serveRendererOrder(%q, %v): %v", tc.requested, tc.hasNative, err)
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("serveRendererOrder(%q, %v) = %v, want %v", tc.requested, tc.hasNative, got, tc.want)
		}
	}
}

func TestServeRendererOrderRejects(t *testing.T) {
	if _, err := serveRendererOrder("native", false); err == nil {
		t.Error("native accepted for a theme without native.theme.json")
	}
	if _, err := serveRendererOrder("chrome", true); err == nil {
		t.Error("an unknown renderer was accepted")
	}
}

// testTheme lays out a theme directory with the requested versions. A broken
// native version stands in for trofeo, whose background frames are not in git.
func testTheme(t *testing.T, native string, web bool) *theme.Theme {
	t.Helper()

	dir := t.TempDir()
	if native != "" {
		if err := os.WriteFile(filepath.Join(dir, "native.theme.json"), []byte(native), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if web {
		dist := filepath.Join(dir, "dist")
		if err := os.MkdirAll(dist, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte("<h1>web</h1>"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return &theme.Theme{Name: "test", Path: dir, HasNative: native != ""}
}

const workingNativeTheme = `{"schema_version": 2, "layout": "freeform_v2", "width": 40, "height": 60,
  "background": "#000000", "canvas": {"width": 40, "height": 60}}`

func TestNewServeDisplayUsesTheNativeVersion(t *testing.T) {
	srv, err := newServeDisplay(display.Options{}, testTheme(t, workingNativeTheme, true), []string{"native", "web"})
	if err != nil {
		t.Fatalf("newServeDisplay: %v", err)
	}
	if !srv.Native() {
		t.Error("served the web version although the native one loads")
	}
}

func TestNewServeDisplayFallsBackToWeb(t *testing.T) {
	srv, err := newServeDisplay(display.Options{}, testTheme(t, "{", true), []string{"native", "web"})
	if err != nil {
		t.Fatalf("newServeDisplay: %v", err)
	}
	if srv.Native() {
		t.Error("served the broken native version instead of falling back to web")
	}
}

func TestNewServeDisplayReportsEveryFailure(t *testing.T) {
	_, err := newServeDisplay(display.Options{}, testTheme(t, "{", false), []string{"native", "web"})
	if err == nil {
		t.Fatal("newServeDisplay succeeded with nothing that can be shown")
	}
	for _, want := range []string{"native:", "web:"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}
