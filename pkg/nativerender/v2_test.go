package nativerender

import (
	"bytes"
	"image"
	"testing"
	"time"
)

func TestV2BindingFindsArrayFieldAndTransforms(t *testing.T) {
	data := map[string]interface{}{
		"disk": map[string]interface{}{
			"disks": []interface{}{
				map[string]interface{}{"mount_point": "/", "percent": 73.0},
			},
		},
	}
	value, ok := resolveNumericBinding(data, Binding{
		Provider: "disk", Field: "percent", Scale: 2, Offset: 1,
	})
	if !ok || value != 147 {
		t.Fatalf("value = %v, %v; want 147, true", value, ok)
	}
}

func TestV2ValidationRejectsUnsafeAssets(t *testing.T) {
	definition := &Theme{
		SchemaVersion: 2,
		Layout:        "freeform_v2",
		Canvas:        &Canvas{Width: 100, Height: 200},
		Assets:        map[string]Asset{"bad": {Type: "image", Path: "../secret.png"}},
	}
	if err := definition.Validate(); err == nil {
		t.Fatal("expected unsafe asset validation error")
	}
}

func TestV2ValidationRejectsArbitraryRotation(t *testing.T) {
	definition := &Theme{
		SchemaVersion: 2,
		Layout:        "freeform_v2",
		Canvas:        &Canvas{Width: 100, Height: 200},
		Widgets: []Widget{{
			ID: "bad-rotation", Type: "panel", Rotation: 45,
			Rect: Rect{Width: 20, Height: 10},
		}},
	}
	if err := definition.Validate(); err == nil {
		t.Fatal("expected rotation validation error")
	}
}

func TestV2WidgetRotationChangesLayerBounds(t *testing.T) {
	definition := &Theme{
		SchemaVersion: 2,
		Layout:        "freeform_v2",
		Width:         100,
		Height:        100,
		Background:    "#000000",
		Canvas:        &Canvas{Width: 100, Height: 100},
		Widgets: []Widget{{
			ID: "rotated", Type: "panel", Rotation: 90,
			Rect:  Rect{X: 40, Y: 40, Width: 20, Height: 10},
			Style: WidgetStyle{Background: "#ff0000"},
		}},
	}
	renderer := New(definition, 100, 100)
	frame := renderer.Render(nil, time.Now())
	if pixel := frame.RGBAAt(50, 36); pixel.R < 200 {
		t.Fatalf("rotated vertical pixel = %#v, want red", pixel)
	}
	if pixel := frame.RGBAAt(41, 45); pixel.R != 0 {
		t.Fatalf("pixel outside rotated bounds = %#v, want black", pixel)
	}
}

func TestV2RenderAndLowPowerAreMonochrome(t *testing.T) {
	definition := &Theme{
		SchemaVersion: 2,
		Layout:        "freeform_v2",
		Width:         120,
		Height:        200,
		Background:    "#000000",
		Text:          "#ffffff",
		Accent:        "#00ddff",
		Accent2:       "#ff00cc",
		Canvas:        &Canvas{Width: 120, Height: 200},
		Widgets: []Widget{
			{
				ID: "load", Type: "bar", Rect: Rect{X: 10, Y: 20, Width: 100, Height: 20},
				Binding: &Binding{Provider: "cpu", Field: "load", Max: 100},
				Style:   WidgetStyle{Color: "#00ddff"},
			},
			{
				ID: "chart", Type: "line", Rect: Rect{X: 10, Y: 60, Width: 100, Height: 80},
				Binding: &Binding{Provider: "cpu", Field: "load", Max: 100},
				Style:   WidgetStyle{Color: "#ff00cc", BorderWidth: 2},
			},
		},
	}
	renderer := New(definition, 120, 200)
	data := map[string]interface{}{"cpu": map[string]interface{}{"load": 52.0}}
	renderer.RecordSnapshot(data, time.Now().Add(-time.Second))
	renderer.RecordSnapshot(map[string]interface{}{"cpu": map[string]interface{}{"load": 61.0}}, time.Now())
	active := renderer.RenderOverlay(data, time.Now())
	if active.Bounds() != image.Rect(0, 0, 120, 200) {
		t.Fatalf("active bounds = %v", active.Bounds())
	}
	idle := renderer.RenderLowPower(data, time.Now())
	for offset := 0; offset < len(idle.Pix); offset += 4 {
		if idle.Pix[offset] != idle.Pix[offset+1] || idle.Pix[offset+1] != idle.Pix[offset+2] {
			t.Fatalf("colored idle pixel at offset %d", offset)
		}
	}
}

func TestTrofeoV1MigrationPreservesRenderedOverlay(t *testing.T) {
	source := &Theme{
		Layout:     "trofeo_vertical_v1",
		Width:      462,
		Height:     1920,
		Background: "#000000",
		Accent:     "#2de2ff",
		Accent2:    "#ff4df3",
		Accent3:    "#71ffa8",
		Text:       "#f8fbff",
		Muted:      "#8ea1bb",
		Panel:      "#06132688",
		PanelLine:  "#245cff99",
	}
	data := map[string]interface{}{
		"cpu": map[string]interface{}{
			"name": "AMD Ryzen 9 7950X3D 16-Core Processor", "temperature": 64.0,
			"load": 37.0, "frequency": 4876.0, "fan_speed": 5230.0,
		},
		"nvidia_gpu": map[string]interface{}{
			"name": "NVIDIA GeForce RTX 4090", "temperature": 48.0, "load": 18.0,
			"memory_used": 2558.0, "memory_total": 24564.0, "power": 49.0,
		},
		"memory": map[string]interface{}{"percent": 11.0, "used": 10342.4, "total": 95232.0},
		"motherboard": map[string]interface{}{
			"dimm1_temp": 50.0, "dimm2_temp": 51.0, "dimm3_temp": 0.0, "dimm4_temp": 0.0,
		},
		"disk": map[string]interface{}{"disks": []map[string]interface{}{
			{"mount_point": "/", "percent": 87.0},
		}},
		"network": map[string]interface{}{"interfaces": []map[string]interface{}{
			{"name": "eth0", "rx_bytes_per_sec": 1536.0, "tx_bytes_per_sec": 2_621_440.0},
		}},
	}
	now := time.Date(2026, 7, 29, 18, 11, 0, 0, time.Local)
	v1 := New(source, 462, 1920)
	v2 := New(MigrateV1ToV2(source), 462, 1920)
	v1Frame := v1.RenderOverlay(data, now)
	v2Frame := v2.RenderOverlay(data, now)
	assertSamePixels(t, "active overlay", v1Frame.Pix, v2Frame.Pix, 462, 1920)
	v1Idle := v1.RenderLowPower(data, now)
	v2Idle := v2.RenderLowPower(data, now)
	assertSamePixels(t, "idle frame", v1Idle.Pix, v2Idle.Pix, 462, 1920)
}

func assertSamePixels(t *testing.T, label string, first, second []byte, width, height int) {
	t.Helper()
	if !bytes.Equal(first, second) {
		different := 0
		minX, minY, maxX, maxY := width, height, 0, 0
		rows := make([]int, height)
		for index := range first {
			if first[index] != second[index] {
				different++
				pixel := index / 4
				x, y := pixel%width, pixel/width
				rows[y]++
				minX, minY = min(minX, x), min(minY, y)
				maxX, maxY = max(maxX, x), max(maxY, y)
			}
		}
		var ranges [][2]int
		for y := 0; y < len(rows); {
			if rows[y] == 0 {
				y++
				continue
			}
			start := y
			for y+1 < len(rows) && rows[y+1] > 0 {
				y++
			}
			ranges = append(ranges, [2]int{start, y})
			y++
		}
		t.Fatalf("migrated %s differs in %d of %d RGBA bytes; bounds=(%d,%d)-(%d,%d); rows=%v",
			label, different, len(first), minX, minY, maxX, maxY, ranges)
	}
}
