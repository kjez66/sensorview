package display

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/kjez66/sensorview/pkg/sensors"
)

const (
	testCanvasWidth  = 120
	testCanvasHeight = 200
)

// writeNativeTheme writes a minimal schema V2 theme filled with background, a
// "#rrggbb" colour, and returns its directory.
func writeNativeTheme(t *testing.T, dir, background string) string {
	t.Helper()

	definition := fmt.Sprintf(`{
  "schema_version": 2,
  "layout": "freeform_v2",
  "width": %d,
  "height": %d,
  "background": %q,
  "text": "#ffffff",
  "canvas": {"width": %d, "height": %d},
  "widgets": [
    {"id": "load", "type": "bar", "rect": {"x": 10, "y": 150, "width": 100, "height": 20},
     "binding": {"provider": "cpu", "field": "load", "max": 100},
     "style": {"color": "#00ddff"}}
  ]
}`, testCanvasWidth, testCanvasHeight, background, testCanvasWidth, testCanvasHeight)

	if err := os.WriteFile(filepath.Join(dir, nativeThemeFile), []byte(definition), 0o644); err != nil {
		t.Fatalf("write native theme: %v", err)
	}
	return dir
}

// framesURL returns the WebSocket address of a running native display.
func framesURL(srv *Server) string {
	return "ws://127.0.0.1:" + itoa(srv.Port()) + "/frames"
}

// dialFrames connects to the frame stream.
func dialFrames(t *testing.T, srv *Server) *websocket.Conn {
	t.Helper()

	conn, _, err := websocket.DefaultDialer.Dial(framesURL(srv), nil)
	if err != nil {
		t.Fatalf("dial frames: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// readFrame waits for the next frame and decodes it.
func readFrame(t *testing.T, conn *websocket.Conn) image.Image {
	t.Helper()

	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	kind, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	if kind != websocket.BinaryMessage {
		t.Fatalf("frame message type = %d, want binary", kind)
	}
	frame, err := jpeg.Decode(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("frame is not a JPEG: %v", err)
	}
	return frame
}

// cornerColour returns the colour of a pixel no widget covers.
func cornerColour(frame image.Image) (r, g, b uint8) {
	red, green, blue, _ := frame.At(2, 2).RGBA()
	return uint8(red >> 8), uint8(green >> 8), uint8(blue >> 8)
}

// near reports whether two channel values match within JPEG error.
func near(got, want uint8) bool {
	diff := int(got) - int(want)
	return diff > -12 && diff < 12
}

func assertCorner(t *testing.T, frame image.Image, r, g, b uint8) {
	t.Helper()

	gotR, gotG, gotB := cornerColour(frame)
	if !near(gotR, r) || !near(gotG, g) || !near(gotB, b) {
		t.Errorf("corner colour = #%02x%02x%02x, want about #%02x%02x%02x", gotR, gotG, gotB, r, g, b)
	}
}

func TestNewLoadsANativeTheme(t *testing.T) {
	dir := writeNativeTheme(t, t.TempDir(), "#102030")

	srv, err := New(Options{NativeThemeDir: dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer srv.native.Close()

	if !srv.Native() {
		t.Error("Native = false for a native theme directory")
	}
}

// A native theme that cannot render must fail in New, so serve can fall back
// to the web version before it starts listening.
func TestNewRejectsANativeThemeThatCannotLoad(t *testing.T) {
	missing := t.TempDir()
	if _, err := New(Options{NativeThemeDir: missing}); err == nil {
		t.Error("New succeeded with no native.theme.json")
	}

	broken := t.TempDir()
	if err := os.WriteFile(filepath.Join(broken, nativeThemeFile), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Options{NativeThemeDir: broken}); err == nil {
		t.Error("New succeeded with malformed native.theme.json")
	}
}

func TestNativeDisplayServesTheViewerPage(t *testing.T) {
	srv := runServer(t, Options{NativeThemeDir: writeNativeTheme(t, t.TempDir(), "#102030"), Address: "127.0.0.1:0"})
	base := "http://127.0.0.1:" + itoa(srv.Port())

	response, err := http.Get(base + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), "/frames") {
		t.Errorf("GET / = %d, want the viewer page", response.StatusCode)
	}

	missing, err := http.Get(base + "/index.js")
	if err != nil {
		t.Fatalf("GET /index.js: %v", err)
	}
	missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Errorf("GET /index.js = %d, want 404", missing.StatusCode)
	}
}

func TestNativeDisplayStreamsTheRenderedTheme(t *testing.T) {
	srv := runServer(t, Options{NativeThemeDir: writeNativeTheme(t, t.TempDir(), "#102030"), Address: "127.0.0.1:0"})

	frame := readFrame(t, dialFrames(t, srv))

	if frame.Bounds() != image.Rect(0, 0, testCanvasWidth, testCanvasHeight) {
		t.Errorf("frame bounds = %v, want the %dx%d canvas", frame.Bounds(), testCanvasWidth, testCanvasHeight)
	}
	assertCorner(t, frame, 0x10, 0x20, 0x30)
}

// A static theme sends no new frame until something changes, so a display
// that connects later must be given the current picture at once.
func TestNativeDisplayGivesALateClientTheCurrentFrame(t *testing.T) {
	srv := runServer(t, Options{NativeThemeDir: writeNativeTheme(t, t.TempDir(), "#102030"), Address: "127.0.0.1:0"})
	readFrame(t, dialFrames(t, srv))

	late := dialFrames(t, srv)
	_ = late.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	if _, _, err := late.ReadMessage(); err != nil {
		t.Errorf("late client got no frame within 500ms: %v", err)
	}
}

// Reload is what the Studio's Apply calls: the edited theme must reach every
// connected display without a restart.
func TestReloadShowsTheEditedTheme(t *testing.T) {
	dir := writeNativeTheme(t, t.TempDir(), "#102030")
	srv := runServer(t, Options{NativeThemeDir: dir, Address: "127.0.0.1:0"})
	conn := dialFrames(t, srv)
	readFrame(t, conn)

	writeNativeTheme(t, dir, "#e04010")
	if err := srv.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	assertCorner(t, readFrame(t, conn), 0xe0, 0x40, 0x10)
}

// A theme that fails to load after an edit must be reported, so the Studio can
// restore the previous file, and the display must keep the working version.
func TestReloadKeepsTheCurrentThemeWhenTheEditIsBroken(t *testing.T) {
	dir := writeNativeTheme(t, t.TempDir(), "#102030")
	srv := runServer(t, Options{NativeThemeDir: dir, Address: "127.0.0.1:0"})
	readFrame(t, dialFrames(t, srv))

	if err := os.WriteFile(filepath.Join(dir, nativeThemeFile), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := srv.Reload(); err == nil {
		t.Fatal("Reload succeeded with a malformed theme")
	}

	assertCorner(t, readFrame(t, dialFrames(t, srv)), 0x10, 0x20, 0x30)
}

func TestReloadIsANoOpForAWebTheme(t *testing.T) {
	srv := runServer(t, Options{DistDir: themeDist(t), Address: "127.0.0.1:0"})

	if srv.Native() {
		t.Error("Native = true for a web theme")
	}
	if err := srv.Reload(); err != nil {
		t.Errorf("Reload on a web theme = %v, want nil", err)
	}
}

func TestReloadFailsWhenTheDisplayIsNotRunning(t *testing.T) {
	previous := reloadTimeout
	reloadTimeout = 50 * time.Millisecond
	t.Cleanup(func() { reloadTimeout = previous })

	srv, err := New(Options{NativeThemeDir: writeNativeTheme(t, t.TempDir(), "#102030")})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer srv.native.Close()

	if err := srv.Reload(); err == nil {
		t.Error("Reload succeeded with no render loop running")
	}
}

// newTestRuntime loads a runtime with no sensors enabled, so its picture only
// changes when the test says so.
func newTestRuntime(t *testing.T) (*nativeRuntime, *sensors.Collector) {
	t.Helper()

	runtime, err := loadNativeRuntime(writeNativeTheme(t, t.TempDir(), "#102030"), time.Second)
	if err != nil {
		t.Fatalf("loadNativeRuntime: %v", err)
	}
	t.Cleanup(runtime.Close)
	return runtime, sensors.NewCollector(&sensors.Config{EnabledSensors: map[string]bool{}})
}

// Rendering and encoding a frame is the expensive part, so a static theme whose
// readings have not changed must not produce another one.
func TestStaticThemeSkipsUnchangedFrames(t *testing.T) {
	runtime, collector := newTestRuntime(t)
	now := time.Now()

	runtime.collect(collector, now)
	first, err := runtime.frame(now)
	if err != nil || first == nil {
		t.Fatalf("first frame = %d bytes, %v; want a frame", len(first), err)
	}

	runtime.collect(collector, now)
	if again, err := runtime.frame(now); err != nil || again != nil {
		t.Errorf("unchanged frame = %d bytes, %v; want nothing", len(again), err)
	}

	runtime.viewSignature = "changed"
	if changed, err := runtime.frame(now); err != nil || changed == nil {
		t.Errorf("frame after a change = %d bytes, %v; want a frame", len(changed), err)
	}
}

// The encoder reuses its buffer, so each frame handed out must be a copy that
// stays intact after the next one is encoded.
func TestFramesAreIndependentCopies(t *testing.T) {
	runtime, collector := newTestRuntime(t)
	now := time.Now()

	runtime.collect(collector, now)
	first, _ := runtime.frame(now)
	kept := bytes.Clone(first)

	runtime.viewSignature = "changed"
	if _, err := runtime.frame(now); err != nil {
		t.Fatalf("second frame: %v", err)
	}
	if !bytes.Equal(first, kept) {
		t.Error("the first frame changed when the second was encoded")
	}
}

func TestStaticThemeFrameRateFollowsTheSensorInterval(t *testing.T) {
	cases := []struct {
		interval time.Duration
		want     float64
	}{
		{time.Second, 1},
		{250 * time.Millisecond, 4},
		{5 * time.Second, 1}, // never slower than once a second
		{0, 1},
	}
	for _, tc := range cases {
		runtime, err := loadNativeRuntime(writeNativeTheme(t, t.TempDir(), "#102030"), tc.interval)
		if err != nil {
			t.Fatalf("loadNativeRuntime: %v", err)
		}
		if runtime.fps != tc.want {
			t.Errorf("interval %s: fps = %v, want %v", tc.interval, runtime.fps, tc.want)
		}
		runtime.Close()
	}
}

// runNative must stop cleanly with a display connected.
func TestNativeDisplayStopsWithAClientConnected(t *testing.T) {
	srv, err := New(Options{NativeThemeDir: writeNativeTheme(t, t.TempDir(), "#102030"), Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()
	if err := srv.waitReady(2 * time.Second); err != nil {
		t.Fatal(err)
	}
	readFrame(t, dialFrames(t, srv))

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

// Rendering pauses while nobody watches, so the first display to connect must
// not wait a whole frame interval for its picture.
func TestFirstDisplayGetsAFrameWithoutWaitingForATick(t *testing.T) {
	srv := runServer(t, Options{
		NativeThemeDir: writeNativeTheme(t, t.TempDir(), "#102030"),
		Address:        "127.0.0.1:0",
		Interval:       time.Second,
	})

	conn := dialFrames(t, srv)
	start := time.Now()
	readFrame(t, conn)
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("first frame took %s, want well under the 1s frame interval", elapsed)
	}
}
