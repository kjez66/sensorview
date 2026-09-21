# Creating Themes

Themes can be React + TypeScript applications rendered by headless Chrome, or
native JSON themes rendered directly by SensorView without Chrome.

## Quick Start

```bash
# Create a new theme
./sensorview theme create my-theme

# Start development (opens browser with hot reload)
./sensorview theme dev my-theme

# Build for production
./sensorview theme build my-theme

# Use with your panel
./sensorview theme select my-theme
./sensorview run
```

## Theme Structure

```
my-theme/
├── package.json
├── native.theme.json     # Optional native renderer theme
├── tsconfig.json
├── vite.config.ts
├── eslint.config.js
├── index.html
├── src/
│   ├── main.tsx      # Entry point
│   ├── App.tsx       # Main component
│   ├── App.css       # Styles
│   └── vite-env.d.ts
├── lib/
│   └── sensorview/  # SDK (auto-generated)
│       ├── index.ts
│       ├── client.ts
│       ├── hooks.ts
│       └── types.ts
└── dist/             # Built output
```

## Renderer Modes

SensorView supports three renderer modes for normal sensor dashboards:

```bash
sensorview run --renderer auto    # Native if native.theme.json exists, else Chrome
sensorview run --renderer native  # Native Go renderer, no browser process
sensorview run --renderer chrome  # Existing web theme renderer
```

`--renderer` does not affect GIF, image, or music modes. The music dashboard
still uses the browser path because it depends on artwork, lyrics layout, and
dynamic media UI code.

Native themes are intended for low CPU/RAM dashboards on USB panels. They trade
CSS/React flexibility for predictable rendering cost and fewer moving parts.

### Native theme file

The recommended way to build a native theme is `sensorview ui`. The Studio
writes schema V2 and uses the same Go renderer for its **Native preview** and
the physical panel. The JSON remains portable and hand-editable.

Add `native.theme.json` to the theme directory:

```json
{
  "schema_version": 2,
  "name": "my-native-theme",
  "layout": "freeform_v2",
  "background": "#000000",
  "accent": "#2de2ff",
  "canvas": {
    "width": 462,
    "height": 1920,
    "background": "wallpaper"
  },
  "assets": {
    "wallpaper": {"type": "image", "path": "assets/wallpaper.jpg"},
    "display-font": {"type": "font", "path": "assets/display.ttf"}
  },
  "widgets": [
    {
      "id": "cpu-value",
      "type": "value",
      "rect": {"x": 24, "y": 100, "width": 180, "height": 64},
      "binding": {
        "provider": "cpu",
        "field": "usage_percent",
        "min": 0,
        "max": 100,
        "clamp": true,
        "format": "%.0f%%"
      },
      "style": {
        "color": "#f8fbff",
        "font_size": 44,
        "font": "display-font"
      }
    }
  ],
  "performance": {
    "profile": "balanced",
    "target_fps": 8,
    "active_fps": 24,
    "idle_fps": 1,
    "idle_timeout_seconds": 20,
    "jpeg_quality": 78,
    "prefetch_frames": 10,
    "jpeg_encoder": "auto"
  }
}
```

Schema V2 widget types are `text`, `value`, `clock`, `bar`, `gauge`, `line`,
`area`, `sparkline`, `panel`, and `image`. Every widget has an exact pixel
rectangle and may specify `z_index`, `rotation` (0/90/180/270), `opacity`,
`visible`, and `locked`. Numeric bindings support provider/field selection,
fallback bindings, best-item or explicit array item selection, dynamic maximum
bindings, scale, offset, min/max clamping, printf-style formatting, and native
formatters for clock, RPM, VRAM, and transfer-rate values. Text widgets can
combine named `bindings` in templates such as `{used} / {total} GB`. Charts
keep bounded history in RAM and can use `series` for multiple bindings.

`performance.profile` accepts `power-saver` (6 FPS), `balanced` (8 FPS), or
`smooth` (12 FPS). `target_fps`, `jpeg_quality`, and `prefetch_frames` override
the profile defaults. The Trofeo pipeline keeps compressed source frames in a
bounded cache and reuses a small decoded-frame ring; it never retains the whole
animation as raw RGBA. On LY panels, background JPEGs are rotated once into
`$XDG_CACHE_HOME/sensorview/native-background/` and reused on later starts.
`jpeg_encoder: "auto"` uses libjpeg-turbo only when SensorView was built with
`go build -tags turbojpeg`; otherwise it safely falls back to Go's standard
JPEG implementation.

On Linux, `active_fps` and `idle_fps` enable adaptive rendering from local
keyboard and mouse events: SensorView uses `active_fps` while the desktop
receives input, then switches to `idle_fps` after `idle_timeout_seconds`. This
requires read access to `/dev/input` (normally membership in the `input`
group); without it, SensorView safely remains active.

`idle_fps` is both the idle scheduler cadence and the panel keepalive cadence.
In idle mode the native renderer omits video and redraws the monochrome frame
only when formatted on-screen values change. It resends the cached pixels at
the configured cadence (1 FPS in the Trofeo theme) so the panel firmware does
not restore its splash screen.

The original `trofeo_vertical_v1` layout remains supported. Studio exposes an
exact in-memory V2 representation for inspection, but treats the installed V1
definition as read-only. Use **Clone active theme** to create a separate V2
copy with all assets and 75 editable layers. Applying directly over a V1 theme
is rejected, so the known-good source cannot be accidentally replaced.
Existing React themes continue to work through `--renderer chrome`.

## Using the SDK

The SDK provides React hooks for accessing sensor data:

```tsx
import { useSensorData, useConnectionStatus } from "../lib/sensorview";

function App() {
  const data = useSensorData();
  const status = useConnectionStatus();

  if (status !== "connected" || !data) {
    return <div>Connecting...</div>;
  }

  return (
    <div>
      <p>CPU: {data.cpu.load}%</p>
      <p>GPU: {data.gpu.temperature}°C</p>
    </div>
  );
}
```

### Available Hooks

| Hook | Returns | Description |
|------|---------|-------------|
| `useSensorData()` | `SensorData \| null` | All sensor data |
| `useConnectionStatus()` | `ConnectionStatus` | "connecting", "connected", "disconnected", "error" |
| `useCpuData()` | `CpuData \| null` | CPU data only |
| `useGpuData()` | `GpuData \| null` | GPU data only |
| `useMemoryData()` | `MemoryData \| null` | Memory data only |

### Utility Functions

```tsx
import { formatBytes, formatRate } from "../lib/sensorview";

formatBytes(1073741824);  // "1.0 GB"
formatRate(1048576);      // "1.0 MB/s"
```

## Data Types

```typescript
interface SensorData {
  cpu: {
    temperature?: number;  // Celsius
    load: number;          // 0-100
    frequency?: number;    // MHz
    cores?: number;
  };
  gpu: {
    available: boolean;
    name?: string;
    temperature?: number;
    load?: number;
    memoryUsed?: number;   // MB
    memoryTotal?: number;  // MB
    power?: number;        // Watts
  };
  memory: {
    used: number;          // MB
    total: number;         // MB
    available: number;     // MB
    percent: number;       // 0-100
  };
  disk: Record<string, {
    mountpoint: string;
    used: number;          // GB
    total: number;         // GB
    free: number;          // GB
    percent: number;       // 0-100
  }>;
  network: Record<string, {
    interface: string;
    rxRate: number;        // bytes/sec
    txRate: number;        // bytes/sec
  }>;
}

type ConnectionStatus = "connecting" | "connected" | "disconnected" | "error";
```

## Display Dimensions

The default display is 480x320 pixels. Design your theme for this resolution:

```css
html, body, #root {
  width: 480px;
  height: 320px;
  overflow: hidden;
}
```

## Development Workflow

1. **Create theme**: `sensorview theme create my-theme`

2. **Start dev server**: `sensorview theme dev my-theme`
   - This starts the WebSocket sensor server
   - Launches Vite dev server with HMR
   - Opens your browser automatically

3. **Edit files**: Changes to `src/` hot-reload instantly

4. **Build**: `sensorview theme build my-theme`
   - Runs TypeScript compiler
   - Bundles with Vite
   - Outputs to `dist/`

5. **Use**: `sensorview theme select my-theme && sensorview run`

## Package Manager Support

SensorView auto-detects your preferred package manager:

| Lockfile | Package Manager |
|----------|-----------------|
| `bun.lockb` | bun |
| `pnpm-lock.yaml` | pnpm |
| `yarn.lock` | yarn |
| `package-lock.json` | npm |

You can use any of these - just install dependencies normally:

```bash
npm install     # or yarn, pnpm, bun
```

## Tips

1. **Keep it simple**: The display is small, focus on key metrics
2. **High contrast**: Use light text on dark background for readability
3. **Large fonts**: Aim for 24px+ for values you need to read at a glance
4. **Test on device**: Colors may look different on the LCD
5. **Keep unchanged areas stable**: Regional updates are fastest when only small rectangles change
6. **Avoid full-screen animations**: They defeat regional updates and may not render smoothly on USB panels
7. **Render nothing until data arrives**: Avoid flashing temporary connection text on the physical display
8. **Handle null values**: Sensors may not always return data

### Thermalright Trofeo Vision 9.16 LCD themes

The Trofeo Vision 9.16 LCD uses a 1920×462 render canvas in SensorView. Design
for an ultrawide strip rather than a small 480×320 grid:

- Use large primary values; tinted case glass reduces perceived contrast.
- Prefer static backgrounds and simple bars/gauges. The device receives
  JPEG-compressed full frames, not regional RGB565 updates.
- Avoid tiny labels at the far edges; the panel is usually viewed through a
  side window at an angle.
- The balanced native video profile targets 8 FPS. Full-screen video always
  requires complete JPEG frames, so use `power-saver` when fan noise and CPU
  use matter more than motion smoothness.
- The included `trofeo` theme has a native JSON definition for portrait use:
  `sensorview run --orientation 90 --renderer native`.

## Example Themes

### Minimal

```tsx
function App() {
  const data = useSensorData();
  if (!data) return null;
  
  return (
    <div style={{ fontSize: 48, textAlign: 'center', paddingTop: 120 }}>
      CPU: {data.cpu.load.toFixed(0)}%
    </div>
  );
}
```

### Grid Layout

```tsx
function App() {
  const data = useSensorData();
  if (!data) return null;
  
  return (
    <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8, padding: 8, height: '100%' }}>
      <Metric label="CPU" value={`${data.cpu.load.toFixed(0)}%`} />
      <Metric label="GPU" value={`${data.gpu.load?.toFixed(0) ?? '--'}%`} />
      <Metric label="RAM" value={`${data.memory.percent.toFixed(0)}%`} />
      <Metric label="TEMP" value={`${data.cpu.temperature?.toFixed(0) ?? '--'}°C`} />
    </div>
  );
}
```

## Troubleshooting

### "Disconnected. Retrying..."

Make sure `sensorview theme dev` is running. The theme connects to the WebSocket server on port 19847.

### Theme not updating on device

Run `sensorview theme build` to rebuild, then restart `sensorview run`.

### TypeScript errors

Run `npm run lint` to check for issues. The template uses strict TypeScript settings.
