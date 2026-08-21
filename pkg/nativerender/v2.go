package nativerender

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	bitmap "github.com/oae/sensorpanel/pkg/renderer"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

type historyPoint struct {
	at    time.Time
	value float64
}

// LoadAssets loads V2 image assets from a theme directory. Video sequences
// continue to use LoadBackgroundSequence so they retain the optimized JPEG path.
func (r *Renderer) LoadAssets(baseDir string) error {
	for id, asset := range r.theme.Assets {
		if asset.Type == "font" {
			path := filepath.Join(baseDir, filepath.Clean(asset.Path))
			rel, err := filepath.Rel(baseDir, path)
			if err != nil || strings.HasPrefix(rel, "..") {
				return fmt.Errorf("asset %q escapes theme directory", id)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("open font %q: %w", id, err)
			}
			parsed, err := opentype.Parse(data)
			if err != nil {
				return fmt.Errorf("parse font %q: %w", id, err)
			}
			r.assetFonts[id] = parsed
			continue
		}
		if asset.Type != "image" {
			continue
		}
		path := filepath.Join(baseDir, filepath.Clean(asset.Path))
		rel, err := filepath.Rel(baseDir, path)
		if err != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("asset %q escapes theme directory", id)
		}
		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open asset %q: %w", id, err)
		}
		decoded, _, decodeErr := image.Decode(file)
		_ = file.Close()
		if decodeErr != nil {
			return fmt.Errorf("decode asset %q: %w", id, decodeErr)
		}
		bounds := decoded.Bounds()
		rgba := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
		draw.Draw(rgba, rgba.Bounds(), decoded, bounds.Min, draw.Src)
		r.assetImages[id] = rgba
	}
	return nil
}

// RecordSnapshot appends one sample for every V2 binding. It should be called
// at sensor cadence, not animation cadence.
func (r *Renderer) RecordSnapshot(data map[string]interface{}, now time.Time) {
	if r.theme.Layout != "freeform_v2" {
		return
	}
	for _, widget := range r.theme.Widgets {
		bindings := widget.Series
		if widget.Binding != nil {
			bindings = append(bindings, *widget.Binding)
		}
		for _, binding := range bindings {
			value, ok := resolveNumericBinding(data, binding)
			if !ok {
				continue
			}
			key := bindingKey(binding)
			points := append(r.history[key], historyPoint{at: now, value: value})
			historySeconds := widget.HistorySeconds
			if historySeconds <= 0 {
				historySeconds = 300
			}
			if historySeconds > 3600 {
				historySeconds = 3600
			}
			cutoff := now.Add(-time.Duration(historySeconds) * time.Second)
			first := 0
			for first < len(points) && points[first].at.Before(cutoff) {
				first++
			}
			if first > 0 {
				copy(points, points[first:])
				points = points[:len(points)-first]
			}
			if len(points) > 3600 {
				points = points[len(points)-3600:]
			}
			r.history[key] = points
		}
	}
}

func (r *Renderer) drawV2CanvasBackground(dst *image.RGBA) {
	if r.theme.Canvas == nil || r.theme.Canvas.Background == "" {
		return
	}
	if src := r.assetImages[r.theme.Canvas.Background]; src != nil {
		drawImageCover(dst, dst.Bounds(), src, 1)
	}
}

func (r *Renderer) drawV2(img *image.RGBA, data map[string]interface{}, now time.Time) {
	if r.sortedWidgets == nil {
		r.sortedWidgets = append([]Widget(nil), r.theme.Widgets...)
		sort.SliceStable(r.sortedWidgets, func(i, j int) bool {
			return r.sortedWidgets[i].ZIndex < r.sortedWidgets[j].ZIndex
		})
	}
	for _, widget := range r.sortedWidgets {
		if widget.Visible != nil && !*widget.Visible {
			continue
		}
		r.drawV2Widget(img, data, now, widget)
	}
}

func (r *Renderer) drawV2Widget(img *image.RGBA, data map[string]interface{}, now time.Time, widget Widget) {
	rotation := ((widget.Rotation % 360) + 360) % 360
	if rotation != 0 {
		local := widget
		local.Rotation = 0
		local.Rect = Rect{Width: widget.Rect.Width, Height: widget.Rect.Height}
		layer := image.NewRGBA(image.Rect(0, 0, widget.Rect.Width, widget.Rect.Height))
		r.drawV2Widget(layer, data, now, local)
		rotated := rotateV2Layer(layer, rotation)
		centerX := widget.Rect.X + widget.Rect.Width/2
		centerY := widget.Rect.Y + widget.Rect.Height/2
		target := image.Rect(
			centerX-rotated.Bounds().Dx()/2,
			centerY-rotated.Bounds().Dy()/2,
			centerX+(rotated.Bounds().Dx()+1)/2,
			centerY+(rotated.Bounds().Dy()+1)/2,
		)
		draw.Draw(img, target, rotated, rotated.Bounds().Min, draw.Over)
		return
	}
	rect := image.Rect(widget.Rect.X, widget.Rect.Y, widget.Rect.X+widget.Rect.Width, widget.Rect.Y+widget.Rect.Height)
	opacity := widget.Opacity
	if opacity <= 0 {
		opacity = 1
	}
	fg := r.v2Color(widget.Style.Color, r.text, opacity)
	bg := r.v2Color(widget.Style.Background, color.RGBA{}, opacity)
	track := r.v2Color(widget.Style.TrackColor, withAlpha(r.text, 38), opacity)
	if bg.A > 0 {
		r.roundedRect(img, rect, widget.Style.Radius, bg)
	}
	if widget.Style.BorderWidth > 0 {
		border := r.v2Color(widget.Style.BorderColor, r.panelLine, opacity)
		r.roundedBorder(img, rect, widget.Style.Radius, widget.Style.BorderWidth, border)
	}

	switch widget.Type {
	case "panel":
		return
	case "image":
		if src := r.assetImages[widget.Asset]; src != nil {
			drawImageCover(img, rect, src, opacity)
		}
		return
	case "text":
		r.v2Text(img, rect, r.widgetText(widget, data), widget.Style, fg)
		return
	case "clock":
		value := now.Format("15:04")
		if widget.Text != "" {
			value = now.Format(widget.Text)
		}
		value = transformWidgetText(value, widget)
		r.v2Text(img, rect, value, widget.Style, fg)
		return
	}

	binding := derefBinding(widget.Binding)
	value, ok := resolveNumericBinding(data, binding)
	if !ok {
		if widget.Type == "value" {
			r.v2Text(img, rect, "--", widget.Style, fg)
		}
		return
	}
	switch widget.Type {
	case "value":
		raw, _ := resolveBinding(data, binding)
		r.v2Text(img, rect, transformWidgetText(formatResolvedBinding(raw, binding), widget), widget.Style, fg)
	case "bar":
		minimum, maximum := bindingRange(binding)
		if widget.MaxBinding != nil {
			if dynamicMaximum, found := resolveNumericBinding(data, *widget.MaxBinding); found {
				maximum = dynamicMaximum
			}
		}
		percentValue := percentage(value, minimum, maximum)
		r.roundedRect(img, rect, widget.Style.Radius, track)
		fill := rect
		fill.Max.X = fill.Min.X + int(float64(rect.Dx())*percentValue/100)
		r.roundedRect(img, fill, min(widget.Style.Radius, fill.Dx()/2), fg)
	case "gauge":
		minimum, maximum := bindingRange(binding)
		size := min(rect.Dx(), rect.Dy())
		r.gauge(img, rect.Min.X+(rect.Dx()-size)/2, rect.Min.Y+(rect.Dy()-size)/2, size, percentage(value, minimum, maximum), fg)
	case "line", "area", "sparkline":
		r.drawV2Chart(img, rect, widget, data, fg, track)
	}
}

func (r *Renderer) roundedRect(img *image.RGBA, rect image.Rectangle, radius int, fill color.RGBA) {
	rect = rect.Intersect(img.Bounds())
	if rect.Empty() {
		return
	}
	radius = min(max(0, radius), min(rect.Dx(), rect.Dy())/2)
	if radius == 0 {
		r.rect(img, rect.Min.X, rect.Min.Y, rect.Dx(), rect.Dy(), fill)
		return
	}
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			if insideRoundedRect(x, y, rect, radius) {
				blendPixel(img, x, y, fill)
			}
		}
	}
}

func (r *Renderer) roundedBorder(img *image.RGBA, rect image.Rectangle, radius, width int, stroke color.RGBA) {
	rect = rect.Intersect(img.Bounds())
	if rect.Empty() || width <= 0 {
		return
	}
	radius = min(max(0, radius), min(rect.Dx(), rect.Dy())/2)
	width = min(width, min(rect.Dx(), rect.Dy())/2)
	inner := image.Rect(rect.Min.X+width, rect.Min.Y+width, rect.Max.X-width, rect.Max.Y-width)
	innerRadius := max(0, radius-width)
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			if insideRoundedRect(x, y, rect, radius) && !insideRoundedRect(x, y, inner, innerRadius) {
				blendPixel(img, x, y, stroke)
			}
		}
	}
}

func insideRoundedRect(x, y int, rect image.Rectangle, radius int) bool {
	if rect.Empty() || !image.Pt(x, y).In(rect) {
		return false
	}
	if radius <= 0 || (x >= rect.Min.X+radius && x < rect.Max.X-radius) ||
		(y >= rect.Min.Y+radius && y < rect.Max.Y-radius) {
		return true
	}
	centerX := rect.Min.X + radius
	if x >= rect.Max.X-radius {
		centerX = rect.Max.X - radius - 1
	}
	centerY := rect.Min.Y + radius
	if y >= rect.Max.Y-radius {
		centerY = rect.Max.Y - radius - 1
	}
	dx, dy := x-centerX, y-centerY
	return dx*dx+dy*dy <= radius*radius
}

func rotateV2Layer(source *image.RGBA, degrees int) *image.RGBA {
	width, height := source.Bounds().Dx(), source.Bounds().Dy()
	switch degrees {
	case 90:
		target := image.NewRGBA(image.Rect(0, 0, height, width))
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				target.SetRGBA(height-1-y, x, source.RGBAAt(x, y))
			}
		}
		return target
	case 180:
		target := image.NewRGBA(image.Rect(0, 0, width, height))
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				target.SetRGBA(width-1-x, height-1-y, source.RGBAAt(x, y))
			}
		}
		return target
	case 270:
		target := image.NewRGBA(image.Rect(0, 0, height, width))
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				target.SetRGBA(y, width-1-x, source.RGBAAt(x, y))
			}
		}
		return target
	default:
		return source
	}
}

func (r *Renderer) drawV2Chart(img *image.RGBA, rect image.Rectangle, widget Widget, data map[string]interface{}, fg, track color.RGBA) {
	bindings := make([]Binding, 0, len(widget.Series)+1)
	if widget.Binding != nil {
		bindings = append(bindings, *widget.Binding)
	}
	bindings = append(bindings, widget.Series...)
	palette := []color.RGBA{fg, r.v2Color(r.theme.Accent2, r.accent2, 1), r.v2Color(r.theme.Accent3, r.accent3, 1)}
	for seriesIndex, binding := range bindings {
		points := r.history[bindingKey(binding)]
		if len(points) < 2 {
			if value, ok := resolveNumericBinding(data, binding); ok {
				points = []historyPoint{{value: value}, {value: value}}
			} else {
				continue
			}
		}
		minimum, maximum := bindingRange(binding)
		if binding.Min == 0 && binding.Max == 0 {
			minimum, maximum = points[0].value, points[0].value
			for _, point := range points[1:] {
				minimum = math.Min(minimum, point.value)
				maximum = math.Max(maximum, point.value)
			}
			if maximum <= minimum {
				maximum = minimum + 1
			}
		}
		lineColor := palette[seriesIndex%len(palette)]
		lastX, lastY := rect.Min.X, chartY(points[0].value, minimum, maximum, rect)
		for index := 1; index < len(points); index++ {
			x := rect.Min.X + index*(rect.Dx()-1)/(len(points)-1)
			y := chartY(points[index].value, minimum, maximum, rect)
			if widget.Type == "area" {
				for fillX := lastX; fillX <= x; fillX++ {
					r.line(img, fillX, max(lastY, y), fillX, rect.Max.Y-1, withAlpha(lineColor, 45), 1)
				}
			}
			r.line(img, lastX, lastY, x, y, lineColor, max(1, widget.Style.BorderWidth))
			lastX, lastY = x, y
		}
	}
	_ = track
}

func chartY(value, minimum, maximum float64, rect image.Rectangle) int {
	pct := percentage(value, minimum, maximum)
	return rect.Max.Y - 1 - int(float64(rect.Dy()-1)*pct/100)
}

func (r *Renderer) v2Text(img *image.RGBA, rect image.Rectangle, value string, style WidgetStyle, c color.RGBA) {
	if style.Font != "" {
		if parsed := r.assetFonts[style.Font]; parsed != nil {
			size := style.FontSize
			if size <= 0 {
				size = 24
			}
			key := fmt.Sprintf("%s:%d", style.Font, size)
			face := r.fontFaces[key]
			if face == nil {
				created, err := opentype.NewFace(parsed, &opentype.FaceOptions{
					Size: float64(size), DPI: 72, Hinting: font.HintingFull,
				})
				if err == nil {
					face = created
					r.fontFaces[key] = face
				}
			}
			if face != nil {
				width := font.MeasureString(face, value).Ceil()
				x := rect.Min.X
				switch style.Align {
				case "center":
					x += (rect.Dx() - width) / 2
				case "right":
					x += rect.Dx() - width
				}
				drawer := font.Drawer{
					Dst: img, Src: image.NewUniform(c), Face: face,
					Dot: fixed.P(x, rect.Min.Y+face.Metrics().Ascent.Ceil()),
				}
				drawer.DrawString(value)
				return
			}
		}
	}
	scale := max(1, style.FontSize/8)
	if style.FontSize == 0 {
		scale = 2
	}
	width, _ := bitmapTextSize(value, scale)
	x := rect.Min.X
	switch style.Align {
	case "center":
		x += (rect.Dx() - width) / 2
	case "right":
		x += rect.Dx() - width
	}
	r.textAt(img, x, rect.Min.Y, scale, value, c)
}

func bitmapTextSize(value string, scale int) (int, int) {
	return bitmap.MeasureBitmapText(ascii(value), scale)
}

func (r *Renderer) v2Color(value string, fallback color.RGBA, opacity float64) color.RGBA {
	result := fallback
	if value != "" {
		result = parseColor(value)
	}
	if r.v2Monochrome {
		if semantic, ok := r.monochromePaletteColor(result); ok {
			result = semantic
		} else {
			luma := uint8((uint16(result.R)*54 + uint16(result.G)*183 + uint16(result.B)*19) >> 8)
			result.R, result.G, result.B = luma, luma, luma
		}
	}
	result.A = uint8(float64(result.A) * clamp(opacity, 0, 1))
	return result
}

func (r *Renderer) monochromePaletteColor(input color.RGBA) (color.RGBA, bool) {
	roles := []struct {
		configured string
		current    color.RGBA
	}{
		{r.theme.Accent, r.accent},
		{r.theme.Accent2, r.accent2},
		{r.theme.Accent3, r.accent3},
		{r.theme.Text, r.text},
		{r.theme.Muted, r.muted},
		{r.theme.Panel, r.panel},
		{r.theme.PanelLine, r.panelLine},
	}
	for _, role := range roles {
		if role.configured == "" {
			continue
		}
		configured := parseColor(role.configured)
		if input.R != configured.R || input.G != configured.G || input.B != configured.B {
			continue
		}
		result := role.current
		if input.A != configured.A {
			result.A = input.A
		}
		return result, true
	}
	return color.RGBA{}, false
}

func derefBinding(binding *Binding) Binding {
	if binding == nil {
		return Binding{}
	}
	return *binding
}

func bindingRange(binding Binding) (float64, float64) {
	minimum, maximum := binding.Min, binding.Max
	if maximum <= minimum {
		minimum, maximum = 0, 100
	}
	return minimum, maximum
}

func resolveNumericBinding(data map[string]interface{}, binding Binding) (float64, bool) {
	raw, ok := resolveBinding(data, binding)
	if !ok {
		return 0, false
	}
	var value float64
	switch number := raw.(type) {
	case float64:
		value = number
	case float32:
		value = float64(number)
	case int:
		value = float64(number)
	case int64:
		value = float64(number)
	case uint64:
		value = float64(number)
	case json.Number:
		value, _ = number.Float64()
	default:
		parsed, err := strconv.ParseFloat(fmt.Sprint(raw), 64)
		if err != nil {
			return 0, false
		}
		value = parsed
	}
	scale := binding.Scale
	if scale == 0 {
		scale = 1
	}
	value = value*scale + binding.Offset
	if binding.Clamp {
		minimum, maximum := bindingRange(binding)
		value = math.Max(minimum, math.Min(maximum, value))
	}
	return value, true
}

func resolveBinding(data map[string]interface{}, binding Binding) (interface{}, bool) {
	raw, ok := resolveBindingDirect(data, binding)
	if ok && (!binding.FallbackOnZero || !isZeroValue(raw)) {
		return raw, true
	}
	for _, fallback := range binding.Fallbacks {
		if raw, found := resolveBinding(data, fallback); found &&
			(!binding.FallbackOnZero || !isZeroValue(raw)) {
			return raw, true
		}
	}
	return raw, ok
}

func resolveBindingDirect(data map[string]interface{}, binding Binding) (interface{}, bool) {
	if binding.Provider == "system" {
		switch binding.Field {
		case "hostname":
			hostname, err := os.Hostname()
			return hostname, err == nil && hostname != ""
		}
	}
	provider, ok := data[binding.Provider].(map[string]interface{})
	if !ok {
		return nil, false
	}
	if binding.Select == "best" {
		provider = firstItem(provider, "disks", "interfaces", "_items")
	}
	if binding.ItemKey == "" {
		if value, exists := provider[binding.Field]; exists {
			return value, true
		}
		return findField(provider, binding.Field)
	}
	var find func(interface{}) (interface{}, bool)
	find = func(value interface{}) (interface{}, bool) {
		switch collection := value.(type) {
		case map[string]interface{}:
			if fmt.Sprint(collection[binding.ItemKey]) == binding.ItemValue {
				result, exists := collection[binding.Field]
				return result, exists
			}
			for _, nested := range collection {
				if result, found := find(nested); found {
					return result, true
				}
			}
		case []interface{}:
			for _, nested := range collection {
				if result, found := find(nested); found {
					return result, true
				}
			}
		case []map[string]interface{}:
			for _, nested := range collection {
				if result, found := find(nested); found {
					return result, true
				}
			}
		}
		return nil, false
	}
	return find(provider)
}

func isZeroValue(value interface{}) bool {
	switch typed := value.(type) {
	case float64:
		return typed == 0
	case float32:
		return typed == 0
	case int:
		return typed == 0
	case int64:
		return typed == 0
	case uint64:
		return typed == 0
	case json.Number:
		number, err := typed.Float64()
		return err == nil && number == 0
	}
	return false
}

func findField(value interface{}, field string) (interface{}, bool) {
	switch collection := value.(type) {
	case map[string]interface{}:
		if result, exists := collection[field]; exists {
			return result, true
		}
		for _, nested := range collection {
			if result, found := findField(nested, field); found {
				return result, true
			}
		}
	case []interface{}:
		for _, nested := range collection {
			if result, found := findField(nested, field); found {
				return result, true
			}
		}
	case []map[string]interface{}:
		for _, nested := range collection {
			if result, found := findField(nested, field); found {
				return result, true
			}
		}
	}
	return nil, false
}

// MigrateV1ToV2 expands the fixed Trofeo V1 layout into independently editable
// V2 layers. The source definition is never mutated.
func MigrateV1ToV2(source *Theme) *Theme {
	if source.SchemaVersion >= 2 {
		result := *source
		if result.Assets == nil {
			result.Assets = map[string]Asset{}
		}
		if result.Widgets == nil {
			result.Widgets = []Widget{}
		}
		return &result
	}
	width, height := source.Width, source.Height
	if width <= 0 {
		width = 462
	}
	if height <= 0 {
		height = 1920
	}
	result := *source
	result.SchemaVersion = 2
	result.Layout = "freeform_v2"
	result.Width, result.Height = width, height
	result.Canvas = &Canvas{Width: width, Height: height}
	result.Assets = make(map[string]Asset, len(source.Assets))
	for id, asset := range source.Assets {
		result.Assets[id] = asset
	}
	if source.Layout != "trofeo_vertical_v1" {
		result.Widgets = []Widget{}
		return &result
	}

	accent := colorDefault(source.Accent, "#00d8ff")
	accent2 := colorDefault(source.Accent2, "#ff4df3")
	accent3 := colorDefault(source.Accent3, "#71ffa8")
	text := colorDefault(source.Text, "#f8fbff")
	muted := colorDefault(source.Muted, "#8ea1bb")
	panel := colorDefault(source.Panel, "#071427cc")
	panelLine := colorDefault(source.PanelLine, "#1d5cff88")
	track := colorWithAlpha(text, 34)
	result.Accent, result.Accent2, result.Accent3 = accent, accent2, accent3
	result.Text, result.Muted = text, muted
	result.Panel, result.PanelLine = panel, panelLine

	style := func(color string, scale int) WidgetStyle {
		return WidgetStyle{Color: color, FontSize: scale * 8}
	}
	textLayer := func(id, value string, x, y, w, h, scale int, color, align string) Widget {
		return Widget{
			ID: id, Type: "text", Rect: Rect{X: x, Y: y, Width: w, Height: h}, Text: value,
			Style: WidgetStyle{Color: color, FontSize: scale * 8, Align: align},
		}
	}
	valueLayer := func(id string, binding Binding, x, y, w, h, scale int, color, align string) Widget {
		return Widget{
			ID: id, Type: "value", Rect: Rect{X: x, Y: y, Width: w, Height: h},
			Binding: &binding, Style: WidgetStyle{Color: color, FontSize: scale * 8, Align: align},
		}
	}
	barLayer := func(id string, binding Binding, maximum *Binding, x, y, w, h int, color string) Widget {
		return Widget{
			ID: id, Type: "bar", Rect: Rect{X: x, Y: y, Width: w, Height: h},
			Binding: &binding, MaxBinding: maximum,
			Style: WidgetStyle{Color: color, TrackColor: track},
		}
	}

	cpu := func(field string, fallbacks ...string) Binding {
		binding := Binding{Provider: "cpu", Field: field}
		for _, fallback := range fallbacks {
			binding.Fallbacks = append(binding.Fallbacks, Binding{Provider: "cpu", Field: fallback})
		}
		return binding
	}
	gpu := func(field string, fallbacks ...string) Binding {
		binding := Binding{Provider: "nvidia_gpu", Field: field}
		for _, fallback := range fallbacks {
			binding.Fallbacks = append(binding.Fallbacks,
				Binding{Provider: "nvidia_gpu", Field: fallback})
		}
		binding.Fallbacks = append(binding.Fallbacks, Binding{Provider: "amd_gpu", Field: field})
		for _, fallback := range fallbacks {
			binding.Fallbacks = append(binding.Fallbacks,
				Binding{Provider: "amd_gpu", Field: fallback})
		}
		return binding
	}
	memory := func(field string, fallbacks ...string) Binding {
		binding := Binding{Provider: "memory", Field: field}
		for _, fallback := range fallbacks {
			binding.Fallbacks = append(binding.Fallbacks, Binding{Provider: "memory", Field: fallback})
		}
		return binding
	}
	best := func(provider, field string, fallbacks ...string) Binding {
		binding := Binding{Provider: provider, Field: field, Select: "best"}
		for _, fallback := range fallbacks {
			binding.Fallbacks = append(binding.Fallbacks,
				Binding{Provider: provider, Field: fallback, Select: "best"})
		}
		return binding
	}

	var widgets []Widget
	add := func(widget Widget) {
		widget.ZIndex = len(widgets)
		widgets = append(widgets, widget)
	}
	addPanel := func(id string, x, y, w, h int) {
		add(Widget{ID: id, Type: "panel", Rect: Rect{X: x, Y: y, Width: w, Height: h},
			Style: WidgetStyle{Background: panel}})
		add(Widget{ID: id + "-top", Type: "panel", Rect: Rect{X: x, Y: y, Width: w, Height: 2},
			Style: WidgetStyle{Background: panelLine}})
		side := colorWithAlpha(panelLine, 80)
		add(Widget{ID: id + "-bottom", Type: "panel", Rect: Rect{X: x, Y: y + h - 2, Width: w, Height: 2},
			Style: WidgetStyle{Background: side}})
		add(Widget{ID: id + "-left", Type: "panel", Rect: Rect{X: x, Y: y, Width: 2, Height: h},
			Style: WidgetStyle{Background: side}})
		add(Widget{ID: id + "-right", Type: "panel", Rect: Rect{X: x + w - 2, Y: y, Width: 2, Height: h},
			Style: WidgetStyle{Background: side}})
	}

	host := Binding{Provider: "system", Field: "hostname", Uppercase: true, MaxLength: 13}
	add(Widget{ID: "host", Type: "text", Rect: Rect{X: 18, Y: 18, Width: 230, Height: 32},
		Binding: &host, Style: style(text, 3)})
	add(Widget{ID: "clock", Type: "clock", Rect: Rect{X: 260, Y: 18, Width: 184, Height: 40},
		Text: "15:04", Style: WidgetStyle{Color: text, FontSize: 32, Align: "right"}})
	add(Widget{ID: "date", Type: "clock", Rect: Rect{X: 18, Y: 76, Width: 300, Height: 24},
		Text: "Mon, Jan 2", Uppercase: true, Style: style(muted, 2)})

	addPanel("summary-panel", 18, 134, width-36, 420)
	cpuLoad := cpu("load", "load_percent")
	cpuLoad.Min, cpuLoad.Max = 0, 100
	gpuLoad := gpu("load", "load_percent")
	gpuLoad.Min, gpuLoad.Max = 0, 100
	add(Widget{ID: "cpu-gauge", Type: "gauge", Rect: Rect{X: 56, Y: 196, Width: 150, Height: 150},
		Binding: &cpuLoad, Style: WidgetStyle{Color: accent}})
	add(Widget{ID: "gpu-gauge", Type: "gauge", Rect: Rect{X: 256, Y: 196, Width: 150, Height: 150},
		Binding: &gpuLoad, Style: WidgetStyle{Color: accent2}})
	cpuTemp := cpu("temperature", "temperature_c")
	cpuTemp.Format = "%.0fC"
	gpuTemp := gpu("temperature", "temperature_c")
	gpuTemp.Format = "%.0fC"
	add(valueLayer("cpu-temp", cpuTemp, 56, 266, 150, 40, 4, text, "center"))
	add(valueLayer("gpu-temp", gpuTemp, 256, 266, 150, 40, 4, text, "center"))
	cpuSummaryLoad := cpuLoad
	cpuSummaryLoad.Format = "CPU %.0f%%"
	gpuSummaryLoad := gpuLoad
	gpuSummaryLoad.Format = "GPU %.0f%%"
	add(valueLayer("cpu-summary-load", cpuSummaryLoad, 56, 339, 150, 24, 2, muted, "center"))
	add(valueLayer("gpu-summary-load", gpuSummaryLoad, 256, 339, 150, 24, 2, muted, "center"))
	cpuModel := cpu("name", "model")
	cpuModel.MaxLength = 30
	gpuModel := gpu("name", "model")
	gpuModel.MaxLength = 30
	add(Widget{ID: "cpu-model", Type: "text", Rect: Rect{X: 42, Y: 426, Width: 378, Height: 24},
		Binding: &cpuModel, Style: style(muted, 2)})
	add(Widget{ID: "gpu-model", Type: "text", Rect: Rect{X: 42, Y: 468, Width: 378, Height: 24},
		Binding: &gpuModel, Style: style(muted, 2)})

	addPanel("compute-panel", 18, 578, width-36, 620)
	add(textLayer("cpu-title", "CPU", 42, 602, 180, 32, 3, accent, ""))
	addMetricRow := func(id, label string, binding Binding, maximum *Binding, y int, color string) {
		add(textLayer(id+"-label", label, 42, y, 180, 24, 2, muted, ""))
		add(valueLayer(id+"-value", binding, 222, y-4, 198, 32, 3, text, "right"))
		add(barLayer(id+"-bar", binding, maximum, 42, y+42, 378, 18, color))
	}
	cpuLoad.Format = "%.0f%%"
	addMetricRow("cpu-load", "LOAD", cpuLoad, nil, 654, accent)
	cpuClock := cpu("frequency", "frequency_mhz")
	cpuClock.Min, cpuClock.Max, cpuClock.Formatter = 0, 6000, "clock"
	addMetricRow("cpu-clock", "CLOCK", cpuClock, nil, 734, accent)
	cpuFan := cpu("fan_speed", "fan_rpm")
	cpuFan.FallbackOnZero = true
	cpuFan.Fallbacks = append(cpuFan.Fallbacks,
		Binding{Provider: "motherboard", Field: "cpu_fan"},
		Binding{Provider: "motherboard", Field: "system_fan1"})
	cpuFan.Min, cpuFan.Max, cpuFan.Formatter = 0, 6000, "rpm"
	addMetricRow("cpu-fan", "FAN", cpuFan, nil, 814, accent)

	add(textLayer("gpu-title", "GPU", 42, 904, 180, 32, 3, accent2, ""))
	gpuLoad.Format = "%.0f%%"
	addMetricRow("gpu-load", "LOAD", gpuLoad, nil, 956, accent2)
	gpuVRAM := gpu("memory_used", "memory_used_mb")
	gpuVRAM.Formatter = "gb_mb"
	gpuVRAMTotal := gpu("memory_total", "memory_total_mb")
	addMetricRow("gpu-vram", "VRAM", gpuVRAM, &gpuVRAMTotal, 1036, accent2)
	gpuPower := gpu("power", "power_watts")
	gpuPower.Min, gpuPower.Max, gpuPower.Format = 0, 650, "%.0fW"
	addMetricRow("gpu-power", "POWER", gpuPower, nil, 1116, accent2)

	addPanel("memory-panel", 18, 1222, width-36, 380)
	add(textLayer("memory-title", "MEMORY", 42, 1246, 200, 32, 3, accent, ""))
	memPercent := memory("percent")
	memPercent.Min, memPercent.Max, memPercent.Format = 0, 100, "%.0f%%"
	add(valueLayer("memory-percent", memPercent, 42, 1308, 250, 72, 8, text, ""))
	memUsed := memory("used", "used_mb")
	memUsed.Scale, memUsed.Format = 1.0/1024, "%.1f"
	memTotal := memory("total", "total_mb")
	memTotal.Scale, memTotal.Format = 1.0/1024, "%.0f"
	add(Widget{ID: "memory-usage", Type: "text", Rect: Rect{X: 42, Y: 1398, Width: 378, Height: 32},
		Text: "{used} / {total} GB", Bindings: map[string]Binding{"used": memUsed, "total": memTotal},
		Style: style(muted, 3)})
	add(barLayer("memory-bar", memPercent, nil, 42, 1460, 378, 22, accent))
	socketLabels := []string{"A1", "A2", "B1", "B2"}
	socketFields := []string{"dimm1_temp", "dimm2_temp", "dimm3_temp", "dimm4_temp"}
	for index, label := range socketLabels {
		x := 42 + (index%2)*197
		y := 1502 + (index/2)*38
		add(Widget{ID: "memory-" + strings.ToLower(label) + "-panel", Type: "panel",
			Rect:  Rect{X: x, Y: y, Width: 181, Height: 32},
			Style: WidgetStyle{Background: colorWithAlpha(text, 28)}})
		add(textLayer("memory-"+strings.ToLower(label)+"-label", label, x+10, y+8, 50, 18, 2, accent, ""))
		socket := Binding{Provider: "motherboard", Field: socketFields[index], Formatter: "temperature_or_dash"}
		add(valueLayer("memory-"+strings.ToLower(label)+"-value", socket, x+60, y+8, 111, 18, 2, text, "right"))
	}

	storageY := 1626
	addPanel("storage-panel", 18, storageY, width-36, height-storageY-18)
	add(textLayer("storage-title", "STORAGE / NETWORK", 42, 1650, 370, 32, 3, accent2, ""))
	diskName := best("disk", "mount_point", "mount", "name", "label")
	diskName.MaxLength = 14
	add(Widget{ID: "disk-name", Type: "text", Rect: Rect{X: 42, Y: 1700, Width: 190, Height: 24},
		Binding: &diskName, Style: style(text, 2)})
	diskPercent := best("disk", "percent", "used_percent")
	diskPercent.Min, diskPercent.Max, diskPercent.Format = 0, 100, "%.0f%%"
	add(valueLayer("disk-percent", diskPercent, 232, 1692, 188, 32, 3, text, "right"))
	add(barLayer("disk-bar", diskPercent, nil, 42, 1748, 378, 22, accent3))
	add(textLayer("network-down-label", "DOWN", 42, 1806, 100, 24, 2, muted, ""))
	netDown := best("network", "rx_bytes_per_sec", "rx_rate")
	netDown.Formatter = "bps"
	add(valueLayer("network-down", netDown, 142, 1800, 278, 32, 3, text, "right"))
	add(textLayer("network-up-label", "UP", 42, 1862, 100, 24, 2, muted, ""))
	netUp := best("network", "tx_bytes_per_sec", "tx_rate")
	netUp.Formatter = "bps"
	add(valueLayer("network-up", netUp, 142, 1856, 278, 32, 3, text, "right"))

	result.Widgets = widgets
	return &result
}

// EditableV2 returns a non-destructive Studio representation.
func EditableV2(source *Theme) *Theme {
	return MigrateV1ToV2(source)
}

func colorDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func colorWithAlpha(value string, alpha uint8) string {
	parsed := parseColor(value)
	return fmt.Sprintf("#%02x%02x%02x%02x", parsed.R, parsed.G, parsed.B, alpha)
}

func (r *Renderer) widgetText(widget Widget, data map[string]interface{}) string {
	value := widget.Text
	if widget.Binding != nil {
		if raw, ok := resolveBinding(data, *widget.Binding); ok {
			value = formatResolvedBinding(raw, *widget.Binding)
		} else {
			value = "--"
		}
	}
	for name, binding := range widget.Bindings {
		replacement := "--"
		if raw, ok := resolveBinding(data, binding); ok {
			replacement = formatResolvedBinding(raw, binding)
		}
		value = strings.ReplaceAll(value, "{"+name+"}", replacement)
	}
	return transformWidgetText(value, widget)
}

func transformWidgetText(value string, widget Widget) string {
	if widget.Uppercase {
		value = strings.ToUpper(value)
	}
	if widget.MaxLength > 0 {
		value = short(value, widget.MaxLength)
	}
	return value
}

func formatResolvedBinding(raw interface{}, binding Binding) string {
	if text, ok := raw.(string); ok {
		if binding.Uppercase {
			text = strings.ToUpper(text)
		}
		if binding.MaxLength > 0 {
			text = short(text, binding.MaxLength)
		}
		return text
	}
	value, ok := numericValue(raw)
	if !ok {
		return fmt.Sprint(raw)
	}
	scale := binding.Scale
	if scale == 0 {
		scale = 1
	}
	value = value*scale + binding.Offset
	switch binding.Formatter {
	case "clock":
		return formatClock(value)
	case "rpm":
		return formatRPM(value)
	case "gb_mb":
		return formatGB(value * 1024 * 1024)
	case "bps":
		return formatBPS(value)
	case "temperature_or_dash":
		if value <= 0 {
			return "--"
		}
		return fmt.Sprintf("%.0fC", value)
	}
	if binding.Format == "" {
		return strconv.FormatFloat(value, 'f', 0, 64)
	}
	if strings.Contains(binding.Format, "%") {
		return fmt.Sprintf(binding.Format, value)
	}
	return strings.ReplaceAll(binding.Format, "{value}", strconv.FormatFloat(value, 'f', -1, 64))
}

func numericValue(raw interface{}) (float64, bool) {
	switch number := raw.(type) {
	case float64:
		return number, true
	case float32:
		return float64(number), true
	case int:
		return float64(number), true
	case int64:
		return float64(number), true
	case uint64:
		return float64(number), true
	case json.Number:
		value, err := number.Float64()
		return value, err == nil
	default:
		value, err := strconv.ParseFloat(fmt.Sprint(raw), 64)
		return value, err == nil
	}
}

func bindingKey(binding Binding) string {
	encoded, _ := json.Marshal(binding)
	return string(encoded)
}

func percentage(value, minimum, maximum float64) float64 {
	if maximum <= minimum {
		return 0
	}
	return clamp((value-minimum)*100/(maximum-minimum), 0, 100)
}

func drawImageCover(dst *image.RGBA, rect image.Rectangle, src *image.RGBA, opacity float64) {
	rect = rect.Intersect(dst.Bounds())
	if rect.Empty() || src.Bounds().Empty() {
		return
	}
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	scale := math.Max(float64(rect.Dx())/float64(sw), float64(rect.Dy())/float64(sh))
	scaledW, scaledH := float64(sw)*scale, float64(sh)*scale
	offsetX, offsetY := (scaledW-float64(rect.Dx()))/2, (scaledH-float64(rect.Dy()))/2
	alpha := clamp(opacity, 0, 1)
	for y := 0; y < rect.Dy(); y++ {
		sy := min(sh-1, max(0, int((float64(y)+offsetY)/scale)))
		for x := 0; x < rect.Dx(); x++ {
			sx := min(sw-1, max(0, int((float64(x)+offsetX)/scale)))
			source := src.RGBAAt(sx, sy)
			source.A = uint8(float64(source.A) * alpha)
			blendPixel(dst, rect.Min.X+x, rect.Min.Y+y, source)
		}
	}
}

func (r *Renderer) viewSignatureV2(data map[string]interface{}, now time.Time) string {
	values := make([]string, 0, len(r.theme.Widgets)+1)
	values = append(values, now.Format("2006-01-02 15:04"))
	for _, widget := range r.theme.Widgets {
		if widget.Binding != nil {
			if value, ok := resolveBinding(data, *widget.Binding); ok {
				values = append(values, widget.ID+"="+fmt.Sprint(value))
			}
		}
		for _, binding := range widget.Series {
			if value, ok := resolveBinding(data, binding); ok {
				values = append(values, widget.ID+"="+fmt.Sprint(value))
			}
		}
		if widget.MaxBinding != nil {
			if value, ok := resolveBinding(data, *widget.MaxBinding); ok {
				values = append(values, widget.ID+":max="+fmt.Sprint(value))
			}
		}
		names := make([]string, 0, len(widget.Bindings))
		for name := range widget.Bindings {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if value, ok := resolveBinding(data, widget.Bindings[name]); ok {
				values = append(values, widget.ID+":"+name+"="+fmt.Sprint(value))
			}
		}
	}
	return strings.Join(values, "\x00")
}
