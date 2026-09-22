package display

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"image"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"github.com/gorilla/websocket"

	"github.com/kjez66/sensorview/pkg/jpegcodec"
	"github.com/kjez66/sensorview/pkg/nativerender"
	"github.com/kjez66/sensorview/pkg/sensors"
	"github.com/kjez66/sensorview/pkg/server"
)

const (
	// nativeThemeFile is the native definition inside a theme directory.
	nativeThemeFile = "native.theme.json"

	// defaultNativeJPEGQuality matches the USB panel default.
	defaultNativeJPEGQuality = 80
)

// nativeRuntime renders one native theme into JPEG frames. It mirrors the USB
// render path in cmd/run.go without the device: no wire rotation, no dirty
// regions, no panel keepalive. It is owned by a single goroutine.
type nativeRuntime struct {
	renderer *nativerender.Renderer
	encoder  jpegcodec.Encoder
	fps      float64
	animated bool
	width    int
	height   int

	overlay *image.RGBA
	canvas  *image.RGBA

	data             map[string]interface{}
	viewSignature    string
	signatureMinute  int
	overlaySignature string
	overlayDrawn     bool
	sentSignature    string
	sent             bool
}

// loadNativeRuntime loads the native theme in themeDir with its assets and
// background sequence. sensorInterval sets the frame rate of a static theme,
// which only changes when a reading does.
func loadNativeRuntime(themeDir string, sensorInterval time.Duration) (*nativeRuntime, error) {
	definition, err := nativerender.Load(filepath.Join(themeDir, nativeThemeFile))
	if err != nil {
		return nil, fmt.Errorf("load native theme: %w", err)
	}
	if err := definition.Validate(); err != nil {
		return nil, fmt.Errorf("validate native theme: %w", err)
	}

	width, height := definition.Width, definition.Height
	if definition.Canvas != nil {
		width, height = definition.Canvas.Width, definition.Canvas.Height
	}
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("native theme has no canvas size")
	}

	renderer := nativerender.New(definition, width, height)
	if err := renderer.LoadBackgroundSequence(themeDir); err != nil {
		_ = renderer.Close()
		return nil, fmt.Errorf("load native background: %w", err)
	}
	if err := renderer.LoadAssets(themeDir); err != nil {
		_ = renderer.Close()
		return nil, fmt.Errorf("load native assets: %w", err)
	}

	quality, backend := defaultNativeJPEGQuality, "auto"
	if performance := definition.Performance; performance != nil {
		if performance.JPEGQuality > 0 {
			quality = performance.JPEGQuality
		}
		if performance.JPEGEncoder != "" {
			backend = performance.JPEGEncoder
		}
	}
	encoder, err := jpegcodec.NewEncoder(jpegcodec.Config{
		Width: renderer.OutputWidth(), Height: renderer.OutputHeight(),
		Quality: quality, Backend: backend,
	})
	if err != nil {
		_ = renderer.Close()
		return nil, fmt.Errorf("create JPEG encoder: %w", err)
	}

	return &nativeRuntime{
		renderer: renderer,
		encoder:  encoder,
		fps:      nativeFrameRate(definition.Performance, renderer, sensorInterval),
		animated: renderer.BackgroundFrameCount() > 1,
		width:    renderer.OutputWidth(),
		height:   renderer.OutputHeight(),
	}, nil
}

// nativeFrameRate picks the render cadence the same way the USB path does: the
// theme's own rate, capped at its background video, and for a static theme the
// sensor rate, because nothing else changes the picture.
func nativeFrameRate(performance *nativerender.Performance, renderer *nativerender.Renderer, sensorInterval time.Duration) float64 {
	fps := renderer.PreferredFPS()
	if performance != nil {
		if performance.TargetFPS > 0 {
			fps = performance.TargetFPS
		}
		if performance.ActiveFPS > 0 {
			fps = performance.ActiveFPS
		}
	}
	if source := renderer.PreferredFPS(); source > 0 && fps > source {
		fps = source
	}
	if renderer.BackgroundFrameCount() <= 1 {
		static := 1.0
		if sensorInterval > 0 {
			static = max(1, 1/sensorInterval.Seconds())
		}
		if fps <= 0 || fps > static {
			fps = static
		}
	}
	if fps <= 0 {
		fps = 1
	}
	return fps
}

// frameInterval is the time between frames.
func (r *nativeRuntime) frameInterval() time.Duration {
	return time.Duration(float64(time.Second) / r.fps)
}

// collect samples the sensors that are due and records them for history
// charts. It runs on every tick, whether or not anyone is watching, so charts
// have no gap when a display connects.
func (r *nativeRuntime) collect(collector *sensors.Collector, now time.Time) {
	data, collected := collector.CollectScheduled(now, false)
	r.data = data
	if collected {
		r.renderer.RecordSnapshot(data, now)
	}

	minute := now.YearDay()*24*60 + now.Hour()*60 + now.Minute()
	if collected || r.viewSignature == "" || r.signatureMinute != minute {
		r.viewSignature = r.renderer.ViewSignature(data, now)
		r.signatureMinute = minute
	}
}

// frame renders and encodes the current picture. It returns nil when the
// picture is unchanged since the last frame, which for a static theme is most
// ticks. The returned bytes are a copy the caller may keep.
func (r *nativeRuntime) frame(now time.Time) ([]byte, error) {
	if r.sent && !r.animated && r.viewSignature == r.sentSignature {
		return nil, nil
	}

	if r.overlay == nil {
		r.overlay = image.NewRGBA(image.Rect(0, 0, r.width, r.height))
	}
	if !r.overlayDrawn || r.overlaySignature != r.viewSignature {
		r.renderer.RenderOutputOverlayInto(r.overlay, r.data, now)
		r.overlaySignature = r.viewSignature
		r.overlayDrawn = true
	}
	if r.canvas == nil {
		r.canvas = image.NewRGBA(image.Rect(0, 0, r.width, r.height))
	}
	r.renderer.RenderBackgroundInto(r.canvas, now)
	r.renderer.Composite(r.canvas, r.overlay)

	encoded, err := r.encoder.Encode(r.canvas)
	if err != nil {
		return nil, fmt.Errorf("encode frame: %w", err)
	}
	r.sentSignature = r.viewSignature
	r.sent = true
	// The encoder lends its buffer only until the next Encode.
	return bytes.Clone(encoded), nil
}

//go:embed viewer.html
var viewerPage []byte

// nativeHandler serves the viewer page and streams frames from hub.
func nativeHandler(hub *server.Hub) http.Handler {
	upgrader := websocket.Upgrader{}
	mux := http.NewServeMux()
	mux.HandleFunc("/{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(viewerPage)
	})
	mux.HandleFunc("/frames", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		hub.Serve(conn)
	})
	return mux
}

// runNative serves the viewer and renders frames until ctx is cancelled.
func (s *Server) runNative(ctx context.Context) error {
	// Each display keeps only the newest frame waiting, so a slow phone skips
	// frames instead of lagging, and a new one gets the current picture at once.
	// Rendering pauses while nobody watches, so a display joining wakes the loop
	// rather than waiting up to a frame interval for its first picture.
	joined := make(chan struct{}, 1)
	frames := server.NewHub(
		server.WithBinaryMessages(),
		server.WithSendQueue(1),
		server.WithReplayLatest(),
		server.WithOnConnect(func() {
			select {
			case joined <- struct{}{}:
			default:
			}
		}),
	)
	httpServer := server.NewWithHandler(s.options.Address, nativeHandler(frames))
	if err := httpServer.Start(); err != nil {
		return fmt.Errorf("listen on %s: %w", s.options.Address, err)
	}

	s.mu.Lock()
	s.http = httpServer
	s.mu.Unlock()

	runtime := s.native
	defer func() {
		s.mu.Lock()
		s.http = nil
		s.mu.Unlock()
		frames.Close()
		_ = httpServer.Stop()
		runtime.Close()
	}()

	if s.options.OnReady != nil {
		s.options.OnReady(s)
	}

	// Prime the collector so the first frame carries rates rather than the
	// zeroes a first sample produces.
	s.collector.CollectAll()

	tick := func() {
		now := time.Now()
		runtime.collect(s.collector, now)
		// Rendering is the expensive part; skip it while nobody is watching.
		if frames.Len() == 0 {
			return
		}
		frame, err := runtime.frame(now)
		if err != nil {
			log.Printf("display: %v", err)
			return
		}
		if frame != nil {
			frames.Broadcast(frame)
		}
	}

	ticker := time.NewTicker(runtime.frameInterval())
	defer ticker.Stop()
	tick()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			tick()
		case <-joined:
			tick()
		case result := <-s.reloads:
			reloaded, err := loadNativeRuntime(s.options.NativeThemeDir, s.options.Interval)
			if err == nil {
				runtime.Close()
				runtime = reloaded
				ticker.Reset(runtime.frameInterval())
				tick()
			}
			result <- err
		}
	}
}

// Close releases the renderer and encoder.
func (r *nativeRuntime) Close() {
	if r == nil {
		return
	}
	_ = r.encoder.Close()
	_ = r.renderer.Close()
}
