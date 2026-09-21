// Package management serves the local SensorView management studio.
package management

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"io/fs"
	"log"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/kjez66/sensorview/pkg/config"
	"github.com/kjez66/sensorview/pkg/device"
	"github.com/kjez66/sensorview/pkg/nativerender"
	"github.com/kjez66/sensorview/pkg/paths"
	"github.com/kjez66/sensorview/pkg/sensors"
	"github.com/kjez66/sensorview/pkg/theme"
	"golang.org/x/image/font/opentype"
)

//go:embed web/dist/*
var webFiles embed.FS

const maxUploadBytes = int64(4 << 30)

// Options supplies runtime dependencies without coupling the package to cmd.
type Options struct {
	Address       string
	Collector     *sensors.Collector
	ActiveTheme   func() string
	CurrentConfig func() *config.Config
	Status        func() any
	ApplyTheme    func(name string) error
	ApplyConfig   func(*config.Config) error
}

// Server is the embedded localhost management API and Studio.
type Server struct {
	options  Options
	listener net.Listener
	http     *http.Server
	token    string
	jobs     *jobManager
}

// New constructs a management server.
func New(options Options) (*Server, error) {
	if options.Address == "" {
		options.Address = "127.0.0.1:19848"
	}
	host, _, err := net.SplitHostPort(options.Address)
	if err != nil {
		return nil, fmt.Errorf("invalid management address: %w", err)
	}
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return nil, fmt.Errorf("management server must listen on localhost")
	}
	tokenBytes := make([]byte, 24)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, err
	}
	return &Server{
		options: options,
		token:   hex.EncodeToString(tokenBytes),
		jobs:    newJobManager(),
	}, nil
}

// Start begins serving in the background.
func (s *Server) Start() error {
	listener, err := net.Listen("tcp", s.options.Address)
	if err != nil {
		return err
	}
	s.listener = listener
	mux := http.NewServeMux()
	s.routes(mux)
	s.http = &http.Server{
		Handler:           s.securityHeaders(mux),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		if err := s.http.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("management server: %v", err)
		}
	}()
	return nil
}

// Close stops the server and running media jobs.
func (s *Server) Close() error {
	s.jobs.close()
	if s.http == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return s.http.Shutdown(ctx)
}

// URL returns the stable browser URL.
func (s *Server) URL() string {
	if s.listener == nil {
		return ""
	}
	return "http://" + s.listener.Addr().String()
}

func (s *Server) routes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/session", s.handleSession)
	mux.HandleFunc("/api/v1/status", s.handleStatus)
	mux.HandleFunc("/api/v1/config", s.handleConfig)
	mux.HandleFunc("/api/v1/devices", s.handleDevices)
	mux.HandleFunc("/api/v1/sensors/schema", s.handleSensorSchema)
	mux.HandleFunc("/api/v1/sensors/snapshot", s.handleSensorSnapshot)
	mux.HandleFunc("/api/v1/themes", s.handleThemes)
	mux.HandleFunc("/api/v1/themes/export", s.handleThemeExport)
	mux.HandleFunc("/api/v1/themes/import", s.handleThemeImport)
	mux.HandleFunc("/api/v1/theme", s.handleTheme)
	mux.HandleFunc("/api/v1/theme/preview", s.handlePreview)
	mux.HandleFunc("/api/v1/theme/apply", s.handleApply)
	mux.HandleFunc("/api/v1/assets/upload", s.handleUpload)
	mux.HandleFunc("/api/v1/assets/file", s.handleAssetFile)
	mux.HandleFunc("/api/v1/media/probe", s.handleProbe)
	mux.HandleFunc("/api/v1/media/process", s.handleProcess)
	mux.HandleFunc("/api/v1/media/jobs/", s.handleJob)
	mux.HandleFunc("/api/v1/events", s.handleEvents)
	mux.HandleFunc("/api/v1/logs", s.handleLogs)

	content, _ := fs.Sub(webFiles, "web/dist")
	mux.Handle("/", http.FileServer(http.FS(content)))
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if split, _, err := net.SplitHostPort(host); err == nil {
			host = split
		}
		if host != "127.0.0.1" && host != "localhost" && host != "::1" {
			http.Error(w, "localhost only", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			if r.Header.Get("X-SensorView-Token") != s.token {
				http.Error(w, "invalid session token", http.StatusForbidden)
				return
			}
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' blob: data:; media-src 'self' blob:; style-src 'self' 'unsafe-inline'; connect-src 'self' ws:")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	// The token is intentionally obtained by same-origin JavaScript and is
	// required for every mutation by securityHeaders.
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]string{"token": s.token})
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	status := any(map[string]any{"state": "running"})
	if s.options.Status != nil {
		status = s.options.Status()
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if s.options.CurrentConfig != nil {
			if cfg := s.options.CurrentConfig(); cfg != nil {
				writeJSON(w, http.StatusOK, cfg)
				return
			}
		}
		cfg, err := config.Load()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, cfg)
	case http.MethodPut:
		previous, previousErr := config.Load()
		var cfg config.Config
		if err := decodeJSON(r, &cfg); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if _, err := config.NormalizeRenderer(cfg.Renderer); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if cfg.Orientation != 0 && cfg.Orientation != 90 && cfg.Orientation != 180 && cfg.Orientation != 270 {
			writeError(w, http.StatusBadRequest, fmt.Errorf("invalid orientation"))
			return
		}
		if cfg.Brightness < 0 || cfg.Brightness > 7 {
			writeError(w, http.StatusBadRequest, fmt.Errorf("brightness must be 0-7"))
			return
		}
		if err := config.Save(&cfg); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if s.options.ApplyConfig != nil {
			if err := s.options.ApplyConfig(&cfg); err != nil {
				if previousErr == nil {
					_ = config.Save(previous)
				}
				writeError(w, http.StatusConflict, err)
				return
			}
		}
		writeJSON(w, http.StatusOK, &cfg)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	cfg, _ := config.Load()
	result := map[string]any{"configured": cfg.Device}
	if profile := device.FindByVIDPID(cfg.Device.VendorID, cfg.Device.ProductID); profile != nil {
		result["profile"] = map[string]any{
			"id": profile.ID(), "name": profile.Name(),
			"width": profile.Width(), "height": profile.Height(),
			"max_brightness": profile.MaxBrightness(),
		}
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleSensorSchema(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	type providerSchema struct {
		Meta      sensors.SensorMeta  `json:"meta"`
		Available bool                `json:"available"`
		Options   []sensors.OptionDef `json:"options,omitempty"`
	}
	providers := sensors.GlobalRegistry().All()
	result := make([]providerSchema, 0, len(providers))
	for _, provider := range providers {
		item := providerSchema{Meta: provider.Meta(), Available: provider.Available()}
		if optionProvider, ok := provider.(sensors.OptionProvider); ok {
			item.Options = optionProvider.Options()
		}
		result = append(result, item)
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleSensorSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, s.snapshot())
}

func (s *Server) handleThemes(w http.ResponseWriter, r *http.Request) {
	active := ""
	if s.options.ActiveTheme != nil {
		active = s.options.ActiveTheme()
	}
	switch r.Method {
	case http.MethodGet:
		themes, err := theme.List()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"active": active, "themes": themes})
	case http.MethodPost:
		var request struct {
			Name      string `json:"name"`
			CloneFrom string `json:"clone_from,omitempty"`
			Width     int    `json:"width,omitempty"`
			Height    int    `json:"height,omitempty"`
		}
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if !validThemeName(request.Name) {
			writeError(w, http.StatusBadRequest, fmt.Errorf("theme name must contain only lowercase letters, numbers, dashes, or underscores"))
			return
		}
		destination, _ := paths.ThemeDir(request.Name)
		if _, err := os.Stat(destination); err == nil {
			writeError(w, http.StatusConflict, fmt.Errorf("theme already exists"))
			return
		}
		if request.CloneFrom != "" {
			source, err := theme.Load(request.CloneFrom)
			if err != nil {
				writeError(w, http.StatusNotFound, err)
				return
			}
			if !source.HasNative {
				writeError(w, http.StatusBadRequest, fmt.Errorf("source theme has no native definition"))
				return
			}
			if err := copyTree(source.Path, destination); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			if err := renameClonedTheme(destination, request.Name); err != nil {
				_ = os.RemoveAll(destination)
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			if err := migrateClonedNativeTheme(destination); err != nil {
				_ = os.RemoveAll(destination)
				writeError(w, http.StatusInternalServerError, err)
				return
			}
		} else {
			if request.Width <= 0 {
				request.Width = 462
			}
			if request.Height <= 0 {
				request.Height = 1920
			}
			if request.Width > 8192 || request.Height > 8192 ||
				int64(request.Width)*int64(request.Height) > 100_000_000 {
				writeError(w, http.StatusBadRequest, fmt.Errorf("theme dimensions exceed the safety limit"))
				return
			}
			if err := createStudioTheme(destination, request.Name, request.Width, request.Height); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
		}
		created, _ := theme.Load(request.Name)
		writeJSON(w, http.StatusCreated, created)
	case http.MethodDelete:
		name := r.URL.Query().Get("name")
		if name == active {
			writeError(w, http.StatusConflict, fmt.Errorf("cannot delete the active theme"))
			return
		}
		if !validThemeName(name) {
			writeError(w, http.StatusBadRequest, fmt.Errorf("invalid theme name"))
			return
		}
		if err := theme.Delete(name); err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func migrateClonedNativeTheme(destination string) error {
	path := filepath.Join(destination, "native.theme.json")
	definition, err := nativerender.Load(path)
	if err != nil {
		return fmt.Errorf("load cloned native theme: %w", err)
	}
	if definition.SchemaVersion >= 2 {
		return nil
	}
	if definition.Layout != "trofeo_vertical_v1" {
		return nil
	}
	migrated := nativerender.MigrateV1ToV2(definition)
	if err := migrated.Validate(); err != nil {
		return fmt.Errorf("validate migrated native theme: %w", err)
	}
	body, err := json.MarshalIndent(migrated, "", "  ")
	if err != nil {
		return fmt.Errorf("encode migrated native theme: %w", err)
	}
	body = append(body, '\n')
	if err := atomicWrite(path, body, 0o644); err != nil {
		return fmt.Errorf("save migrated native theme: %w", err)
	}
	return nil
}

func (s *Server) handleTheme(w http.ResponseWriter, r *http.Request) {
	name, loaded, err := s.themeForRequest(r)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		data, err := os.ReadFile(loaded.NativePath())
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		definition, err := validateThemeBytes(data)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err)
			return
		}
		writeJSON(w, http.StatusOK, nativerender.EditableV2(definition))
	case http.MethodPut:
		body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if _, err := validateThemeBytes(body); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		draft := filepath.Join(loaded.Path, "native.theme.draft.json")
		if err := atomicWrite(draft, body, 0o644); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"theme": name, "draft": draft})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	_, loaded, err := s.themeForRequest(r)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	definition, err := validateThemeBytes(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	width, height := definition.Width, definition.Height
	if definition.Canvas != nil {
		width, height = definition.Canvas.Width, definition.Canvas.Height
	}
	renderer := nativerender.New(definition, width, height)
	defer renderer.Close()
	if err := renderer.LoadAssets(loaded.Path); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := renderer.LoadBackgroundSequence(loaded.Path); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	data := s.snapshot()
	renderer.RecordSnapshot(data, time.Now())
	image := renderer.Render(data, time.Now())
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	if err := png.Encode(w, image); err != nil {
		writeError(w, http.StatusInternalServerError, err)
	}
}

func (s *Server) handleApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	name, loaded, err := s.themeForRequest(r)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	draft := filepath.Join(loaded.Path, "native.theme.draft.json")
	body, err := os.ReadFile(draft)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("save a draft before applying"))
		return
	}
	if _, err := validateThemeBytes(body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	activePath := loaded.NativePath()
	current, err := nativerender.Load(activePath)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if current.SchemaVersion < 2 {
		writeError(w, http.StatusConflict,
			fmt.Errorf("legacy V1 themes are read-only in Studio; clone the theme first to create a safe V2 copy"))
		return
	}
	previous, previousErr := os.ReadFile(activePath)
	if err := atomicWrite(activePath, body, 0o644); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if s.options.ApplyTheme != nil {
		if err := s.options.ApplyTheme(name); err != nil {
			if previousErr == nil {
				_ = atomicWrite(activePath, previous, 0o644)
			}
			writeError(w, http.StatusConflict, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"theme": name, "applied": true})
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	_, loaded, err := s.themeForRequest(r)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	defer file.Close()
	kind := r.URL.Query().Get("type")
	if kind != "image" && kind != "video" && kind != "font" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("type must be image, video, or font"))
		return
	}
	id := randomID()
	extension := strings.ToLower(filepath.Ext(header.Filename))
	if extension == "" || len(extension) > 10 {
		extension = ".bin"
	}
	dir := filepath.Join(loaded.Path, "assets", "source")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	path := filepath.Join(dir, id+extension)
	if err := writeUpload(path, file); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := validateUploadedAsset(path, kind); err != nil {
		_ = os.Remove(path)
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	relative, _ := filepath.Rel(loaded.Path, path)
	writeJSON(w, http.StatusCreated, map[string]string{
		"id": id, "type": kind, "path": filepath.ToSlash(relative), "name": filepath.Base(header.Filename),
	})
}

func (s *Server) handleAssetFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	loaded, err := theme.Load(r.URL.Query().Get("theme"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	path, err := safeThemePath(loaded.Path, r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	file, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		writeError(w, http.StatusNotFound, fmt.Errorf("asset is not a regular file"))
		return
	}
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

func (s *Server) handleProbe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var request struct {
		Theme string `json:"theme"`
		Path  string `json:"path"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	loaded, err := theme.Load(request.Theme)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	path, err := safeThemePath(loaded.Path, request.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	output, err := exec.Command("ffprobe", "-v", "error", "-show_streams", "-show_format", "-of", "json", path).Output()
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, fmt.Errorf("ffprobe: %w", err))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(output)
}

func (s *Server) handleProcess(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var request ProcessRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	loaded, err := theme.Load(request.Theme)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if _, err := safeThemePath(loaded.Path, request.Source); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	job, err := s.jobs.start(loaded.Path, request)
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusAccepted, job.snapshot())
}

func (s *Server) handleJob(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/media/jobs/")
	job := s.jobs.get(id)
	if job == nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("job not found"))
		return
	}
	if r.Method == http.MethodDelete {
		job.cancel()
	}
	writeJSON(w, http.StatusOK, job.snapshot())
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{CheckOrigin: func(request *http.Request) bool {
		origin := request.Header.Get("Origin")
		if origin == "" {
			return true
		}
		prefixHTTP := "http://" + request.Host
		prefixHTTPS := "https://" + request.Host
		return origin == prefixHTTP || origin == prefixHTTPS
	}}
	connection, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer connection.Close()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for range ticker.C {
		payload := map[string]any{
			"type": "snapshot", "sensors": s.snapshot(), "jobs": s.jobs.snapshots(),
		}
		if err := connection.WriteJSON(payload); err != nil {
			return
		}
	}
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if runtime.GOOS != "linux" {
		writeJSON(w, http.StatusOK, map[string]string{"logs": "service logs are available on Linux"})
		return
	}
	lines := 100
	if parsed, err := strconv.Atoi(r.URL.Query().Get("lines")); err == nil {
		lines = min(500, max(1, parsed))
	}
	output, err := exec.Command("journalctl", "--user", "-u", "sensorview.service", "-n", strconv.Itoa(lines), "--no-pager").CombinedOutput()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"logs": string(output)})
}

func (s *Server) snapshot() map[string]interface{} {
	if s.options.Collector == nil {
		return map[string]interface{}{}
	}
	return s.options.Collector.Snapshot()
}

func (s *Server) themeForRequest(r *http.Request) (string, *theme.Theme, error) {
	name := r.URL.Query().Get("name")
	if name == "" && s.options.ActiveTheme != nil {
		name = s.options.ActiveTheme()
	}
	if !validThemeName(name) {
		return "", nil, fmt.Errorf("invalid theme name")
	}
	loaded, err := theme.Load(name)
	return name, loaded, err
}

func validateThemeBytes(data []byte) (*nativerender.Theme, error) {
	var definition nativerender.Theme
	if err := json.Unmarshal(data, &definition); err != nil {
		return nil, err
	}
	if definition.SchemaVersion >= 2 && definition.Layout == "" {
		definition.Layout = "freeform_v2"
	}
	if err := definition.Validate(); err != nil {
		return nil, err
	}
	if definition.Background == "" {
		definition.Background = "#000000"
	}
	return &definition, nil
}

func safeThemePath(root, relative string) (string, error) {
	if filepath.IsAbs(relative) {
		return "", fmt.Errorf("absolute paths are not allowed")
	}
	path := filepath.Join(root, filepath.Clean(relative))
	rel, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path escapes theme directory")
	}
	return path, nil
}

func writeUpload(path string, source multipart.File) error {
	temporary := path + ".part"
	output, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, source)
	closeErr := output.Close()
	if copyErr != nil {
		_ = os.Remove(temporary)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(temporary)
		return closeErr
	}
	return os.Rename(temporary, path)
}

func validateUploadedAsset(path, kind string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	switch kind {
	case "image":
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		config, format, decodeErr := image.DecodeConfig(file)
		_ = file.Close()
		if decodeErr != nil {
			return fmt.Errorf("unsupported or invalid image: %w", decodeErr)
		}
		if config.Width <= 0 || config.Height <= 0 || config.Width > 16384 || config.Height > 16384 ||
			int64(config.Width)*int64(config.Height) > 100_000_000 {
			return fmt.Errorf("image dimensions %dx%d exceed the safety limit", config.Width, config.Height)
		}
		switch format {
		case "png", "jpeg", "gif", "webp":
		default:
			return fmt.Errorf("unsupported image format %q", format)
		}
	case "font":
		if info.Size() > 64<<20 {
			return fmt.Errorf("font exceeds 64 MiB")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if _, err := opentype.Parse(data); err != nil {
			return fmt.Errorf("invalid TTF/OTF font: %w", err)
		}
	case "video":
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		output, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "v:0",
			"-show_entries", "stream=codec_type", "-of", "default=nw=1:nk=1", path).CombinedOutput()
		if err != nil {
			return fmt.Errorf("invalid video: %s", strings.TrimSpace(string(output)))
		}
		if strings.TrimSpace(string(output)) != "video" {
			return fmt.Errorf("upload contains no video stream")
		}
	default:
		return fmt.Errorf("unsupported asset type %q", kind)
	}
	return nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, mode); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func decodeJSON(r *http.Request, destination any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 8<<20))
	decoder.DisallowUnknownFields()
	return decoder.Decode(destination)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func randomID() string {
	value := make([]byte, 8)
	_, _ = rand.Read(value)
	return hex.EncodeToString(value)
}

func validThemeName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for _, char := range name {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' && char != '_' {
			return false
		}
	}
	return true
}

func createStudioTheme(destination, name string, width, height int) error {
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return err
	}
	metadata, _ := json.MarshalIndent(map[string]any{
		"name": name, "version": "1.0.0", "description": "SensorView Studio theme",
		"width": width, "height": height,
	}, "", "  ")
	if err := os.WriteFile(filepath.Join(destination, "package.json"), metadata, 0o644); err != nil {
		_ = os.RemoveAll(destination)
		return err
	}
	definition := nativerender.Theme{
		SchemaVersion: 2, Name: name, Layout: "freeform_v2",
		Width: width, Height: height, Background: "#000000",
		Accent: "#2de2ff", Accent2: "#ff4df3", Accent3: "#71ffa8",
		Text: "#f8fbff", Muted: "#8ea1bb", Panel: "#061326cc", PanelLine: "#245cff99",
		Canvas: &nativerender.Canvas{Width: width, Height: height},
		Assets: map[string]nativerender.Asset{}, Widgets: []nativerender.Widget{},
		Performance: &nativerender.Performance{
			Profile: "balanced", TargetFPS: 8, ActiveFPS: 24, IdleFPS: 1,
			IdleTimeoutSeconds: 20, JPEGQuality: 68, PrefetchFrames: 1, JPEGEncoder: "auto",
		},
	}
	encoded, _ := json.MarshalIndent(definition, "", "  ")
	if err := os.WriteFile(filepath.Join(destination, "native.theme.json"), encoded, 0o644); err != nil {
		_ = os.RemoveAll(destination)
		return err
	}
	return nil
}

func copyTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "node_modules" || strings.HasPrefix(relative, "node_modules"+string(filepath.Separator)) ||
			relative == "native.theme.draft.json" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			_ = input.Close()
			return err
		}
		_, copyErr := io.Copy(output, input)
		inputCloseErr := input.Close()
		closeErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		if inputCloseErr != nil {
			return inputCloseErr
		}
		return closeErr
	})
}

func renameClonedTheme(destination, name string) error {
	packagePath := filepath.Join(destination, "package.json")
	if data, err := os.ReadFile(packagePath); err == nil {
		var metadata map[string]any
		if err := json.Unmarshal(data, &metadata); err != nil {
			return fmt.Errorf("parse cloned package metadata: %w", err)
		}
		metadata["name"] = name
		encoded, err := json.MarshalIndent(metadata, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(packagePath, encoded, 0o644); err != nil {
			return err
		}
	}
	nativePath := filepath.Join(destination, "native.theme.json")
	if data, err := os.ReadFile(nativePath); err == nil {
		var definition nativerender.Theme
		if err := json.Unmarshal(data, &definition); err != nil {
			return fmt.Errorf("parse cloned native theme: %w", err)
		}
		definition.Name = name
		encoded, err := json.MarshalIndent(definition, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(nativePath, encoded, 0o644); err != nil {
			return err
		}
	}
	return nil
}
