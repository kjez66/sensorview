package management

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/kjez66/sensorview/pkg/config"
	"github.com/kjez66/sensorview/pkg/nativerender"
	"github.com/kjez66/sensorview/pkg/paths"
	"github.com/kjez66/sensorview/pkg/theme"
)

func TestManagementRequiresTokenForMutation(t *testing.T) {
	server, err := New(Options{Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	server.routes(mux)
	handler := server.securityHeaders(mux)
	request := httptest.NewRequest(http.MethodPut, "http://127.0.0.1/api/v1/config", bytes.NewReader([]byte("{}")))
	request.Host = "127.0.0.1"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestConfigGetReturnsEffectiveRuntimeOverrides(t *testing.T) {
	server, err := New(Options{
		Address: "127.0.0.1:0",
		CurrentConfig: func() *config.Config {
			return &config.Config{Theme: "portrait", Orientation: 90, Brightness: 7, UpdateInterval: 1}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	server.routes(mux)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	request.Host = "127.0.0.1"
	response := httptest.NewRecorder()
	server.securityHeaders(mux).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var current config.Config
	if err := json.Unmarshal(response.Body.Bytes(), &current); err != nil {
		t.Fatal(err)
	}
	if current.Orientation != 90 || current.Theme != "portrait" {
		t.Fatalf("effective config = %+v", current)
	}
}

func TestThemePreviewUsesV2Renderer(t *testing.T) {
	dataRoot := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataRoot)
	themeDir := filepath.Join(dataRoot, "sensorview", "themes", "preview")
	if err := os.MkdirAll(themeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(themeDir, "package.json"), []byte(`{"name":"preview","width":100,"height":200}`), 0o644); err != nil {
		t.Fatal(err)
	}
	definition := nativerender.Theme{
		SchemaVersion: 2, Name: "Preview", Layout: "freeform_v2",
		Width: 100, Height: 200, Background: "#000000",
		Text: "#ffffff", Accent: "#00ddff",
		Canvas: &nativerender.Canvas{Width: 100, Height: 200},
		Widgets: []nativerender.Widget{{
			ID: "label", Type: "text", Rect: nativerender.Rect{X: 4, Y: 4, Width: 80, Height: 20},
			Text: "HELLO", Style: nativerender.WidgetStyle{Color: "#ffffff", FontSize: 16},
		}},
	}
	body, _ := json.Marshal(definition)
	if err := os.WriteFile(filepath.Join(themeDir, "native.theme.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	server, err := New(Options{Address: "127.0.0.1:0", ActiveTheme: func() string { return "preview" }})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	server.routes(mux)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/theme/preview?name=preview", bytes.NewReader(body))
	request.Host = "127.0.0.1"
	request.Header.Set("X-SensorView-Token", server.token)
	response := httptest.NewRecorder()
	server.securityHeaders(mux).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "image/png" {
		t.Fatalf("content type = %q", contentType)
	}
}

func TestCloningV1ThemeMigratesCopyWithoutChangingSource(t *testing.T) {
	dataRoot := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataRoot)
	source, err := paths.ThemeDir("legacy")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "package.json"),
		[]byte(`{"name":"legacy","width":462,"height":1920}`), 0o644); err != nil {
		t.Fatal(err)
	}
	legacy := []byte(`{
  "name": "Legacy",
  "layout": "trofeo_vertical_v1",
  "width": 462,
  "height": 1920,
  "accent": "#2de2ff",
  "accent2": "#ff4df3"
}
`)
	nativePath := filepath.Join(source, "native.theme.json")
	if err := os.WriteFile(nativePath, legacy, 0o644); err != nil {
		t.Fatal(err)
	}
	server, err := New(Options{Address: "127.0.0.1:0", ActiveTheme: func() string { return "legacy" }})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	server.routes(mux)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/themes",
		bytes.NewReader([]byte(`{"name":"legacy-v2","clone_from":"legacy"}`)))
	request.Host = "127.0.0.1"
	request.Header.Set("X-SensorView-Token", server.token)
	response := httptest.NewRecorder()
	server.securityHeaders(mux).ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	unchanged, err := os.ReadFile(nativePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(unchanged, legacy) {
		t.Fatal("source V1 definition was modified")
	}
	migrated, err := nativerender.Load(filepath.Join(dataRoot, "sensorview", "themes", "legacy-v2", "native.theme.json"))
	if err != nil {
		t.Fatal(err)
	}
	if migrated.SchemaVersion != 2 || migrated.Layout != "freeform_v2" {
		t.Fatalf("migrated schema/layout = %d/%q", migrated.SchemaVersion, migrated.Layout)
	}
	if len(migrated.Widgets) < 40 {
		t.Fatalf("migrated widget count = %d, want complete editable layout", len(migrated.Widgets))
	}

	draft, err := json.Marshal(migrated)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "native.theme.draft.json"), draft, 0o644); err != nil {
		t.Fatal(err)
	}
	applyRequest := httptest.NewRequest(http.MethodPost, "/api/v1/theme/apply?name=legacy", nil)
	applyRequest.Host = "127.0.0.1"
	applyRequest.Header.Set("X-SensorView-Token", server.token)
	applyResponse := httptest.NewRecorder()
	server.securityHeaders(mux).ServeHTTP(applyResponse, applyRequest)
	if applyResponse.Code != http.StatusConflict {
		t.Fatalf("legacy apply status = %d, want %d", applyResponse.Code, http.StatusConflict)
	}
	unchanged, err = os.ReadFile(nativePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(unchanged, legacy) {
		t.Fatal("rejected V1 apply changed the source definition")
	}
}

func TestThemeArchiveRoundTripRenamesTheme(t *testing.T) {
	dataRoot := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataRoot)
	source, err := paths.ThemeDir("source")
	if err != nil {
		t.Fatal(err)
	}
	if err := createStudioTheme(source, "source", 100, 200); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(source, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "assets", "marker.txt"), []byte("asset"), 0o644); err != nil {
		t.Fatal(err)
	}
	server, err := New(Options{Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	server.routes(mux)
	handler := server.securityHeaders(mux)

	exportRequest := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/themes/export?name=source", nil)
	exportRequest.Host = "127.0.0.1"
	exportResponse := httptest.NewRecorder()
	handler.ServeHTTP(exportResponse, exportRequest)
	if exportResponse.Code != http.StatusOK {
		t.Fatalf("export status = %d, body = %s", exportResponse.Code, exportResponse.Body.String())
	}

	var upload bytes.Buffer
	writer := multipart.NewWriter(&upload)
	part, err := writer.CreateFormFile("file", "source.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(exportResponse.Body.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	importRequest := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/v1/themes/import?name=copy", &upload)
	importRequest.Host = "127.0.0.1"
	importRequest.Header.Set("Content-Type", writer.FormDataContentType())
	importRequest.Header.Set("X-SensorView-Token", server.token)
	importResponse := httptest.NewRecorder()
	handler.ServeHTTP(importResponse, importRequest)
	if importResponse.Code != http.StatusCreated {
		t.Fatalf("import status = %d, body = %s", importResponse.Code, importResponse.Body.String())
	}
	imported, err := theme.Load("copy")
	if err != nil {
		t.Fatal(err)
	}
	if imported.Metadata.Name != "copy" {
		t.Fatalf("metadata name = %q", imported.Metadata.Name)
	}
	data, err := os.ReadFile(imported.NativePath())
	if err != nil {
		t.Fatal(err)
	}
	var definition nativerender.Theme
	if err := json.Unmarshal(data, &definition); err != nil {
		t.Fatal(err)
	}
	if definition.Name != "copy" {
		t.Fatalf("native name = %q", definition.Name)
	}
	if _, err := os.Stat(filepath.Join(imported.Path, "assets", "marker.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestUploadRejectsInvalidImageContent(t *testing.T) {
	dataRoot := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataRoot)
	root, err := paths.ThemeDir("upload")
	if err != nil {
		t.Fatal(err)
	}
	if err := createStudioTheme(root, "upload", 100, 200); err != nil {
		t.Fatal(err)
	}
	server, err := New(Options{Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	server.routes(mux)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "not-an-image.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("plain text"))
	_ = writer.Close()
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/v1/assets/upload?name=upload&type=image", &body)
	request.Host = "127.0.0.1"
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("X-SensorView-Token", server.token)
	response := httptest.NewRecorder()
	server.securityHeaders(mux).ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	entries, err := os.ReadDir(filepath.Join(root, "assets", "source"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("invalid upload left %d files", len(entries))
	}
}
