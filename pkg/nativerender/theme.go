// Package nativerender draws JSON-native themes without a headless browser.
package nativerender

import (
	"encoding/json"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Theme is a lightweight native theme definition.
type Theme struct {
	SchemaVersion      int                 `json:"schema_version,omitempty"`
	Name               string              `json:"name,omitempty"`
	Layout             string              `json:"layout"`
	Width              int                 `json:"width,omitempty"`
	Height             int                 `json:"height,omitempty"`
	Background         string              `json:"background,omitempty"`
	BackgroundSequence *BackgroundSequence `json:"background_sequence,omitempty"`
	Accent             string              `json:"accent,omitempty"`
	Accent2            string              `json:"accent2,omitempty"`
	Accent3            string              `json:"accent3,omitempty"`
	Text               string              `json:"text,omitempty"`
	Muted              string              `json:"muted,omitempty"`
	Panel              string              `json:"panel,omitempty"`
	PanelLine          string              `json:"panel_line,omitempty"`
	Performance        *Performance        `json:"performance,omitempty"`
	Canvas             *Canvas             `json:"canvas,omitempty"`
	Widgets            []Widget            `json:"widgets,omitempty"`
	Assets             map[string]Asset    `json:"assets,omitempty"`
}

// Canvas defines the fixed logical editing surface for a V2 native theme.
type Canvas struct {
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	Background string `json:"background,omitempty"`
}

// Asset references a file contained inside a theme directory.
type Asset struct {
	Type string `json:"type"` // image, video-sequence, or font
	Path string `json:"path"`
}

// Rect is a widget's logical pixel rectangle.
type Rect struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Binding selects and transforms one sensor field.
type Binding struct {
	Provider       string    `json:"provider"`
	Field          string    `json:"field"`
	ItemKey        string    `json:"item_key,omitempty"`
	ItemValue      string    `json:"item_value,omitempty"`
	Select         string    `json:"select,omitempty"`
	Scale          float64   `json:"scale,omitempty"`
	Offset         float64   `json:"offset,omitempty"`
	Min            float64   `json:"min,omitempty"`
	Max            float64   `json:"max,omitempty"`
	Clamp          bool      `json:"clamp,omitempty"`
	Format         string    `json:"format,omitempty"`
	Formatter      string    `json:"formatter,omitempty"`
	MaxLength      int       `json:"max_length,omitempty"`
	Uppercase      bool      `json:"uppercase,omitempty"`
	FallbackOnZero bool      `json:"fallback_on_zero,omitempty"`
	Fallbacks      []Binding `json:"fallbacks,omitempty"`
}

// WidgetStyle contains renderer-independent visual properties.
type WidgetStyle struct {
	Color       string `json:"color,omitempty"`
	Background  string `json:"background,omitempty"`
	TrackColor  string `json:"track_color,omitempty"`
	BorderColor string `json:"border_color,omitempty"`
	BorderWidth int    `json:"border_width,omitempty"`
	Radius      int    `json:"radius,omitempty"`
	FontSize    int    `json:"font_size,omitempty"`
	Font        string `json:"font,omitempty"`
	Align       string `json:"align,omitempty"`
	Fill        bool   `json:"fill,omitempty"`
}

// Widget is one layer on a V2 freeform canvas.
type Widget struct {
	ID             string             `json:"id"`
	Type           string             `json:"type"`
	Rect           Rect               `json:"rect"`
	ZIndex         int                `json:"z_index,omitempty"`
	Rotation       int                `json:"rotation,omitempty"`
	Opacity        float64            `json:"opacity,omitempty"`
	Visible        *bool              `json:"visible,omitempty"`
	Locked         bool               `json:"locked,omitempty"`
	Text           string             `json:"text,omitempty"`
	Uppercase      bool               `json:"uppercase,omitempty"`
	MaxLength      int                `json:"max_length,omitempty"`
	Asset          string             `json:"asset,omitempty"`
	Binding        *Binding           `json:"binding,omitempty"`
	Bindings       map[string]Binding `json:"bindings,omitempty"`
	MaxBinding     *Binding           `json:"max_binding,omitempty"`
	Series         []Binding          `json:"series,omitempty"`
	HistorySeconds int                `json:"history_seconds,omitempty"`
	Style          WidgetStyle        `json:"style,omitempty"`
}

// Performance controls the native renderer's resource budget. Themes may
// override these values, while the command line can still supply a temporary
// target FPS override.
type Performance struct {
	Profile            string  `json:"profile,omitempty"` // power-saver, balanced, smooth
	TargetFPS          float64 `json:"target_fps,omitempty"`
	ActiveFPS          float64 `json:"active_fps,omitempty"`
	IdleFPS            float64 `json:"idle_fps,omitempty"`
	IdleTimeoutSeconds int     `json:"idle_timeout_seconds,omitempty"`
	JPEGQuality        int     `json:"jpeg_quality,omitempty"`
	PrefetchFrames     int     `json:"prefetch_frames,omitempty"`
	JPEGEncoder        string  `json:"jpeg_encoder,omitempty"` // auto, stdlib, turbo
}

// BackgroundSequence configures a pre-rendered animated background.
type BackgroundSequence struct {
	Path    string  `json:"path"`
	Pattern string  `json:"pattern,omitempty"`
	Frames  int     `json:"frames,omitempty"`
	FPS     float64 `json:"fps,omitempty"`
	Opacity float64 `json:"opacity,omitempty"`
	Cache   string  `json:"cache,omitempty"` // lru or memory
}

// Load reads a native.theme.json file.
func Load(path string) (*Theme, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var t Theme
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("parse native theme: %w", err)
	}
	if t.SchemaVersion >= 2 && t.Canvas != nil {
		if t.Layout == "" {
			t.Layout = "freeform_v2"
		}
		if t.Width == 0 {
			t.Width = t.Canvas.Width
		}
		if t.Height == 0 {
			t.Height = t.Canvas.Height
		}
	}
	if t.Layout == "" {
		return nil, fmt.Errorf("native theme missing layout")
	}
	if t.Background == "" {
		t.Background = "#000000"
	}
	if t.Accent == "" {
		t.Accent = "#00d8ff"
	}
	if t.Accent2 == "" {
		t.Accent2 = "#ff4df3"
	}
	if t.Accent3 == "" {
		t.Accent3 = "#71ffa8"
	}
	if t.Text == "" {
		t.Text = "#f8fbff"
	}
	if t.Muted == "" {
		t.Muted = "#8ea1bb"
	}
	if t.Panel == "" {
		t.Panel = "#071427cc"
	}
	if t.PanelLine == "" {
		t.PanelLine = "#1d5cff88"
	}
	if t.BackgroundSequence != nil {
		if t.BackgroundSequence.Pattern == "" {
			t.BackgroundSequence.Pattern = "frame_%04d.jpg"
		}
		if t.BackgroundSequence.FPS <= 0 {
			t.BackgroundSequence.FPS = 24
		}
		if t.BackgroundSequence.Opacity <= 0 {
			t.BackgroundSequence.Opacity = 0.3
		}
		if t.BackgroundSequence.Cache == "" {
			t.BackgroundSequence.Cache = "lru"
		}
	}
	if t.Performance == nil {
		t.Performance = &Performance{}
	}
	if t.Performance.Profile == "" {
		t.Performance.Profile = "balanced"
	}
	switch t.Performance.Profile {
	case "power-saver":
		if t.Performance.TargetFPS <= 0 {
			t.Performance.TargetFPS = 6
		}
		if t.Performance.JPEGQuality <= 0 {
			t.Performance.JPEGQuality = 72
		}
		if t.Performance.PrefetchFrames <= 0 {
			t.Performance.PrefetchFrames = 6
		}
	case "smooth":
		if t.Performance.TargetFPS <= 0 {
			t.Performance.TargetFPS = 12
		}
		if t.Performance.JPEGQuality <= 0 {
			t.Performance.JPEGQuality = 80
		}
		if t.Performance.PrefetchFrames <= 0 {
			t.Performance.PrefetchFrames = 12
		}
	default:
		t.Performance.Profile = "balanced"
		if t.Performance.TargetFPS <= 0 {
			t.Performance.TargetFPS = 8
		}
		if t.Performance.JPEGQuality <= 0 {
			t.Performance.JPEGQuality = 78
		}
		if t.Performance.PrefetchFrames <= 0 {
			t.Performance.PrefetchFrames = 10
		}
	}
	if t.Performance.JPEGEncoder == "" {
		t.Performance.JPEGEncoder = "auto"
	}
	if t.Performance.IdleTimeoutSeconds <= 0 {
		t.Performance.IdleTimeoutSeconds = 20
	}
	return &t, nil
}

// Validate checks a loaded native theme before preview or activation.
func (t *Theme) Validate() error {
	if t.Layout == "" {
		return fmt.Errorf("layout is required")
	}
	if t.SchemaVersion < 2 {
		return nil
	}
	if t.Canvas == nil || t.Canvas.Width <= 0 || t.Canvas.Height <= 0 {
		return fmt.Errorf("V2 theme requires a positive canvas size")
	}
	ids := make(map[string]struct{}, len(t.Widgets))
	for _, widget := range t.Widgets {
		if widget.ID == "" {
			return fmt.Errorf("widget id is required")
		}
		if _, exists := ids[widget.ID]; exists {
			return fmt.Errorf("duplicate widget id %q", widget.ID)
		}
		ids[widget.ID] = struct{}{}
		if widget.Rect.Width <= 0 || widget.Rect.Height <= 0 {
			return fmt.Errorf("widget %q has invalid dimensions", widget.ID)
		}
		rotation := ((widget.Rotation % 360) + 360) % 360
		if rotation != 0 && rotation != 90 && rotation != 180 && rotation != 270 {
			return fmt.Errorf("widget %q rotation must be 0, 90, 180, or 270", widget.ID)
		}
		switch widget.Type {
		case "text", "value", "clock", "bar", "gauge", "line", "area", "sparkline", "panel", "image":
		default:
			return fmt.Errorf("widget %q has unsupported type %q", widget.ID, widget.Type)
		}
		if widget.Binding != nil {
			if err := validateBinding(*widget.Binding); err != nil {
				return fmt.Errorf("widget %q: %w", widget.ID, err)
			}
		}
		if widget.MaxBinding != nil {
			if err := validateBinding(*widget.MaxBinding); err != nil {
				return fmt.Errorf("widget %q maximum: %w", widget.ID, err)
			}
		}
		for name, binding := range widget.Bindings {
			if name == "" {
				return fmt.Errorf("widget %q has an empty template binding name", widget.ID)
			}
			if err := validateBinding(binding); err != nil {
				return fmt.Errorf("widget %q binding %q: %w", widget.ID, name, err)
			}
		}
		for _, binding := range widget.Series {
			if err := validateBinding(binding); err != nil {
				return fmt.Errorf("widget %q chart series: %w", widget.ID, err)
			}
		}
	}
	for id, asset := range t.Assets {
		if id == "" || asset.Path == "" {
			return fmt.Errorf("asset id and path are required")
		}
		if asset.Type != "image" && asset.Type != "font" {
			return fmt.Errorf("asset %q has unsupported type %q", id, asset.Type)
		}
		if filepath.IsAbs(asset.Path) || strings.Contains(filepath.Clean(asset.Path), "..") {
			return fmt.Errorf("asset %q has an unsafe path", id)
		}
	}
	if t.Canvas.Background != "" {
		asset, exists := t.Assets[t.Canvas.Background]
		if !exists || asset.Type != "image" {
			return fmt.Errorf("canvas background %q is not an image asset", t.Canvas.Background)
		}
	}
	for _, widget := range t.Widgets {
		if widget.Type == "image" {
			asset, exists := t.Assets[widget.Asset]
			if widget.Asset == "" || !exists || asset.Type != "image" {
				return fmt.Errorf("widget %q does not reference an image asset", widget.ID)
			}
		}
		if widget.Style.Font != "" {
			asset, exists := t.Assets[widget.Style.Font]
			if !exists || asset.Type != "font" {
				return fmt.Errorf("widget %q does not reference a font asset", widget.ID)
			}
		}
	}
	if t.BackgroundSequence != nil {
		cleanPath := filepath.Clean(t.BackgroundSequence.Path)
		cleanPattern := filepath.Clean(t.BackgroundSequence.Pattern)
		if filepath.IsAbs(t.BackgroundSequence.Path) || strings.Contains(cleanPath, "..") ||
			filepath.IsAbs(t.BackgroundSequence.Pattern) || strings.Contains(cleanPattern, "..") {
			return fmt.Errorf("background sequence has an unsafe path")
		}
	}
	return nil
}

func validateBinding(binding Binding) error {
	if binding.Provider == "" || binding.Field == "" {
		return fmt.Errorf("binding provider and field are required")
	}
	if binding.Select != "" && binding.Select != "best" {
		return fmt.Errorf("binding select must be empty or %q", "best")
	}
	for _, fallback := range binding.Fallbacks {
		if err := validateBinding(fallback); err != nil {
			return fmt.Errorf("invalid fallback: %w", err)
		}
	}
	return nil
}

func parseColor(value string) color.RGBA {
	value = strings.TrimPrefix(strings.TrimSpace(value), "#")
	alpha := uint8(0xff)
	if len(value) == 8 {
		if n, err := strconv.ParseUint(value[6:8], 16, 8); err == nil {
			alpha = uint8(n)
		}
		value = value[:6]
	}
	if len(value) != 6 {
		return color.RGBA{}
	}
	n, err := strconv.ParseUint(value, 16, 32)
	if err != nil {
		return color.RGBA{}
	}
	return color.RGBA{R: uint8(n >> 16), G: uint8(n >> 8), B: uint8(n), A: alpha}
}
