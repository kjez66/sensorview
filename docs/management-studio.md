# Management Studio

SensorView Studio is an embedded visual editor for native themes. It previews
through the native Go renderer. With a USB panel it runs in the same process as
the render loop and applies a validated theme at a frame boundary without
restarting the service.

## Start it

The Studio is part of every build, with or without USB support. Any of these
starts it:

```bash
sensorview ui                     # opens the running Studio, or starts one until Ctrl+C
sensorview serve trofeo           # starts it alongside the phone/tablet display
sensorview run --renderer native  # USB builds only (-tags usb)
```

The default address is `http://127.0.0.1:19848`. Only one process can hold it,
so `serve` prints a warning and carries on without a Studio when `run` or
another `serve` already has one. `sensorview ui` opens whichever is running.
To leave the Studio out of `serve` or `run`, pass `--management=false`.
`sensorview ui --no-browser` prints the address instead of opening a browser.

The Studio opens on the configured theme when it is a native one, and otherwise
on the first installed native theme. Web-only themes are not listed.

### On a phone or tablet

`serve` draws a theme's native version on the PC and streams it to the browser
on the phone or tablet, unless you pass `--renderer web` or the native version
fails to load. In that native mode:

- **Apply to panel** validates and saves the theme, then redraws it on every
  connected screen within a frame. The page on the phone does not reload.
- If the saved theme fails to load, for example because an asset is missing,
  Apply reports the error, the previous file is restored, and the screens keep
  showing the working version.
- Applying a theme other than the one `serve` shows only saves it.

When `serve` shows the web version instead (its built `dist/`), the Studio's
edits do not reach the screen, because the web version is a separate design.
The banner says which one is showing: `Serving theme: caelestia (native)`.

The standalone Studio from `sensorview ui` has no screen attached, so there
Apply only saves.

Reloading starts the history charts afresh, and panel settings such as
brightness and orientation are saved but have no effect without a USB panel.

The persistent settings are:

```json
{
  "management": {
    "enabled": true,
    "address": "127.0.0.1:19848"
  }
}
```

Only `localhost`, `127.0.0.1`, or `::1` may be used. The UI is intentionally
not exposed to the LAN. Mutating API calls require a per-process session token.

## Editing workflow

1. Choose or clone a theme in the header.
2. Add a widget or click a numeric sensor to create a bound value widget.
3. Drag or resize on the 4-pixel snapping canvas.
4. Edit frame, layer, rotation, visibility, lock, opacity, style, font, and
   sensor transformation in the inspector.
5. Use **Native preview** to render a PNG with the real Go renderer.
6. Use **Apply to panel** to validate, save, and hot-reload the physical panel.

Edits remain browser-side until Apply. The server writes a draft first,
validates all widget and asset paths, atomically replaces
`native.theme.json`, and restores the previous file if the runtime reload
fails. Undo/redo keeps up to 100 local layout states.

Legacy `trofeo_vertical_v1` themes are protected as read-only sources. Clone
one in the header to create a separate schema V2 theme: the clone operation
copies all assets and expands the procedural V1 dashboard into editable
layers. The source `native.theme.json` is never rewritten, and Apply rejects a
V1 destination. The migrated native overlay is regression-tested against V1
at the pixel level.

The interactive browser canvas favors easy selection and dragging, so browser
font rasterization and gauge CSS can look slightly different from the panel.
Use **Native preview** for the authoritative output; it uses the same Go
renderer, bitmap font, sensor fallbacks, formatters, and compositing path as
the physical device.

The canvas inspector can resize the complete layout. Widget positions, sizes,
and font sizes are scaled proportionally. Reprocess a video background after
changing the canvas dimensions.

## Sensors and charts

The Sensors tab shows registered providers, availability, numeric fields, and
provider-specific options. Providers can be enabled or disabled while the
service is running. Sensor collection is independent from render FPS, so a
24 FPS background does not execute hardware probes 24 times per second.

Available native widgets:

- text, value, and clock
- bar and gauge
- line, area, and sparkline charts with bounded RAM history
- panel and image layers

Bindings support `scale`, `offset`, `min`, `max`, clamping, formatting,
fallback sensor fields/providers, best-item array selection, dynamic maximums,
and native unit formatters. Text layers may combine several named bindings.
History is sampled at sensor cadence and capped at one hour/3600 points per
binding.

## Images, videos, and fonts

The Assets tab accepts PNG, JPEG, WebP, GIF, MP4, WebM, TTF, and OTF inputs
after server-side type validation. An uploaded image may be used directly as a
full-canvas background or selected by an image widget.

Image and video uploads open a crop editor. The crop region is locked to the
canvas aspect ratio. Videos also support start/end trimming. Processing uses
FFmpeg to produce exact canvas-sized JPEG frames:

- one media job at a time
- at most two FFmpeg threads
- lower process priority on Linux
- output promoted atomically only after successful completion

Video is pre-rendered rather than decoded by a browser on every dashboard
frame. In ultra-low-power idle mode the video is omitted and the native theme
is rendered monochrome.

## Theme packages

**Export** downloads the selected theme as a ZIP including assets and generated
media. Build dependencies, drafts, and `node_modules` are excluded.

**Import** accepts a ZIP under a new safe theme name. Archive traversal and
absolute paths are rejected, upload/extraction sizes are bounded, and the
result must contain a valid native theme. Use clone before a large experiment
to preserve a known-good version.

## Resource behavior

Animated backgrounds use the theme's active FPS while desktop input is
detected. After `idle_timeout_seconds`, SensorView switches to the idle FPS,
removes video, and renders monochrome. Static scenes are capped to the sensor
sampling rate automatically.

For the Thermalright LY panel, each presented frame is still a full JPEG USB
transfer because the firmware has no rectangular update command. TurboJPEG
builds minimize encode/decode cost:

```bash
go build -tags turbojpeg -o sensorview .
```

See [Native renderer performance](performance.md) for benchmark and profiling
commands.

## Developing the Studio

The production UI is embedded from `pkg/management/web/dist`, so rebuild it
after changing React/TypeScript sources:

```bash
cd pkg/management/web
npm ci
npm test
npm run build
```

Then run the Go validation suite with `go test -tags turbojpeg ./...`.
