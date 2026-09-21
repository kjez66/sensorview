# SensorView

[![CI](https://github.com/kjez66/sensorview/actions/workflows/ci.yml/badge.svg)](https://github.com/kjez66/sensorview/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/kjez66/sensorview)](https://goreportcard.com/report/github.com/kjez66/sensorview)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Turn the old tablet or phone in your drawer into a real-time system monitor.

SensorView runs on your PC, reads its sensors, and serves a live dashboard over
your local network. The second screen is any device with a browser - an ageing
iPad, a retired Android phone, a spare laptop - propped up next to your desk.
Nothing to install on that device, and no dedicated hardware to buy.

> Derived from [oae/sensorpanel](https://github.com/oae/sensorpanel), which
> targets cheap AX206 USB LCD panels. Those panels still work here (see
> [Supported Devices](#supported-devices)), but they are no longer the point:
> SensorView is built around screens you already own, reached over the network.

![Dashboard Example](docs/dashboard-preview.png)

## Features

- **Any browser is the panel** - Serve a theme to a phone, tablet or second machine on your network; no app, no USB display
- **One-port serving** - `sensorview serve` ships the built theme and its sensor WebSocket from a single address
- **Interface-aware startup** - Prints every reachable address with its adapter name, so VPN and Hyper-V addresses are easy to skip
- **Real-time monitoring** - CPU, GPU (NVIDIA/AMD), RAM, disk, and network stats
- **Windows sensors** - Load, memory, network and NVIDIA GPU natively; temperatures and fans via LibreHardwareMonitor
- **Web-based themes** - Create custom themes using React + TypeScript
- **TypeScript SDK** - React hooks for easy theme development with hot reload
- **Single-command dev** - One command starts everything for theme development, bound to your LAN
- **Local Management Studio** - Visually arrange sensors, charts, images, fonts, and cropped video backgrounds
- **Theme renderer selection** - Use Chrome-rendered web themes or low-overhead native JSON themes
- **Media display modes** - Static images, animated GIFs, and a now-playing dashboard
- **Music dashboard** - Cover art, song metadata, progress waveform, and synchronized lyrics
- **Dynamic sensor config** - Enable/disable sensors and configure options at runtime
- **Cross-platform** - Runs on Linux, macOS and Windows; sensor coverage varies, see [Built-in Sensors](#built-in-sensors)
- **Autostart service** - Install as system service on all platforms
- **NixOS support** - Flake with module, udev rules, and systemd service
- **USB panels too** - Inherited AX206 device profiles, regional updates, and an interactive wizard for new panels, behind an opt-in `usb` build tag

## Quick Start

### Requirements

**To run the dashboard on a tablet or phone you need:**

| | |
|---|---|
| Host machine | Linux, macOS or Windows - this is where SensorView runs |
| Display device | Anything with a modern browser, on the same network. No app install |
| Network | Host and device on the same LAN; the host's inbound port must not be firewalled |

**To build it you need Go 1.24 or newer. That is all.**

The default build is pure Go, with no cgo and no system libraries:

```bash
go build .
```

**Optional:**

- **Node.js 18+** - only for developing themes (`sensorview theme dev`). Running a
  prebuilt theme does not need it.
- **Chrome** - downloaded automatically for the headless renderer. Native themes
  (`--renderer native`) skip it entirely.
- **[LibreHardwareMonitor](https://github.com/LibreHardwareMonitor/LibreHardwareMonitor)**
  (Windows) - needed for CPU/motherboard temperatures, fan speeds and voltages.
  Without it you still get load, frequency, memory, disk, network and NVIDIA GPU.
  See [Windows: temperatures, fans and voltages](#windows-temperatures-fans-and-voltages).

#### Building with USB panel support

USB panels are driven through `gousb`, which needs cgo and libusb. That support
is behind the `usb` build tag, so it costs nothing unless you ask for it:

```bash
go build -tags usb .
```

Without the tag, `run`, `device`, `panel` and `benchmark` are still listed but
report that the build has no USB support. With the tag you also need:

- **A C toolchain** - cgo does not support MSVC, so Windows needs mingw-w64.
- **libusb**
- **libturbojpeg** (Linux only, with `-tags turbojpeg,usb`) for fast JPEG encoding.

```bash
# Debian/Ubuntu
sudo apt-get install -y build-essential libusb-1.0-0-dev libturbojpeg0-dev

# Fedora
sudo dnf install -y gcc libusbx-devel turbojpeg-devel

# Arch
sudo pacman -S --needed base-devel libusb libjpeg-turbo

# macOS
xcode-select --install
brew install libusb pkg-config
```

On Windows, MSYS2 provides both the compiler and libusb from one package
manager:

```powershell
winget install MSYS2.MSYS2
C:\msys64\usr\bin\pacman -S --noconfirm mingw-w64-x86_64-gcc mingw-w64-x86_64-libusb

$env:Path = "C:\msys64\mingw64\bin;$env:Path"
$env:CGO_ENABLED = "1"
go build -tags usb .
```

CI uses vcpkg instead, which is equivalent:

```powershell
vcpkg integrate install
vcpkg install libusb:x64-windows

$env:CGO_ENABLED = "1"
$env:CGO_CFLAGS  = "-IC:/vcpkg/installed/x64-windows/include/libusb-1.0"
$env:CGO_LDFLAGS = "-LC:/vcpkg/installed/x64-windows/lib -lusb-1.0"
```

### 1. Build

```bash
# Install Mage (recommended build tool)
go install github.com/magefile/mage@latest

# Build with Mage
mage build

# Or directly with Go
go build .

# With USB panel support (needs a C toolchain and libusb)
go build -tags usb .

# Or with Nix
nix build
```

### 2. Put it on your tablet or phone

Build a theme, then serve it on every interface so other devices can reach it:

```bash
./sensorview theme build trofeo
./sensorview serve trofeo --addr 0.0.0.0:19847
```

Each reachable address is printed with the adapter it belongs to, which matters
on a machine with VPN or virtual-switch adapters:

```
Serving theme: trofeo
[serve] Local:     http://localhost:19847/?ws=19847
[serve] Phone/LAN: http://192.168.1.50:19847/?ws=19847  (Ethernet)
```

Open the `Phone/LAN` address in the browser on your tablet, add it to the home
screen for a full-screen view, and you are done. On Windows the first run may
need an inbound firewall rule for the port.

> **The sensor feed has no authentication.** Anyone who can reach that port can
> read your system metrics. The default (`127.0.0.1:19847`) binds loopback only;
> widen it only on a network you trust.

### 3. (Optional) Drive a USB panel instead

Inherited from the upstream project, and still fully supported - but only in a
build made with `-tags usb`, see [Building with USB panel support](#building-with-usb-panel-support):

```bash
./sensorview device list     # See available devices
./sensorview device select   # Interactive selection

# With built-in renderer
./sensorview run

# Play an animated GIF instead of sensor data
./sensorview run --gif /path/to/animation.gif

# URLs are also supported
./sensorview run --gif https://media.tenor.com/j8dwT9wdyc8AAAAi/evernight-anime.gif

# Display a static PNG, JPEG, or GIF from a file or URL
./sensorview run --image /path/to/wallpaper.png

# Display the active song with artwork, progress waveform, and synchronized lyrics
./sensorview run --music

# With sensor options
./sensorview run --opt disk.mounts=/,/home --opt network.interface=eth*

# Or create and use a custom theme
./sensorview theme create my-theme
./sensorview theme select my-theme
./sensorview run

# Force a renderer for the selected theme
./sensorview run --renderer native   # Native Go renderer, no Chrome
./sensorview run --renderer chrome   # Headless Chrome renderer

# While the native dashboard is running, open its local visual editor
./sensorview ui
```

### 4. (Optional) Install as autostart service

```bash
# Install to start on login
./sensorview service install --opt disk.mounts=/

# Start now
./sensorview service start

# Check status
./sensorview service status
```

## Supported Devices

The primary "device" is any browser on your network - see
[Serve a Theme to a Browser](#serve-a-theme-to-a-browser). No profile needed.

For USB LCD panels (builds made with `-tags usb`), SensorView keeps the modular
device profile system it inherited from
[oae/sensorpanel](https://github.com/oae/sensorpanel). Currently supported:

| Device | Resolution | Color Format | Notes |
|--------|------------|--------------|-------|
| QTKeJi/AIDA64 USB Display | 480x320 | RGB565 BE | VID 0x1908 |
| Thermalright Trofeo Vision 9.16 LCD | 1920x462 | RGB888 → JPEG | USB high-speed LY bulk protocol, VID:PID 0416:5408 |
| Generic AX206-based frames | Various | RGB565 | GEMBIRD, Pearl, Coby, etc. |

**Don't see your device?** Run `sensorview device create` to add support for it!

## Commands

### Run Dashboard

```bash
sensorview run [flags]

Flags:
  -i, --interval float    Update interval in seconds (default 1.0)
  -b, --brightness int    Backlight brightness 0-7 (default 7)
  -s, --sensors strings   Sensors to enable (e.g., cpu,memory,disk). Default: all
  -x, --exclude strings   Sensors to exclude (e.g., network,nvidia_gpu)
  -o, --opt strings       Sensor options (e.g., disk.mounts=/,/home)
      --orientation int   Display orientation in degrees: 0, 90, 180, 270
      --renderer string   Theme renderer: auto, native, or chrome (default auto)
      --target-fps float  Native animation target FPS (0 uses theme settings)
      --jpeg-quality int  LY JPEG quality 1-100 (0 uses theme settings)
      --jpeg-encoder      LY encoder: auto, stdlib, or turbo
      --management        Serve the local Management Studio (default true)
      --management-address string
                          Studio address (localhost only; default 127.0.0.1:19848)
      --gif string        Play an animated GIF file or URL instead of sensor data
      --image string      Display a PNG, JPEG, or GIF file or URL instead of sensor data
      --music             Show now-playing music dashboard instead of sensor data
```

Renderer mode only applies to normal themed sensor dashboards. GIF, image, and
music modes use their dedicated render paths. `auto` selects `native` when the
selected theme has `native.theme.json`; otherwise it uses the existing Chrome
renderer.

### Serve a Theme to a Browser

Turns any browser into the panel. No USB display, no Node toolchain and no
headless Chrome: the built theme and its sensor WebSocket are served from one
port.

```bash
sensorview serve [name] [flags]

Flags:
      --addr string        Address to listen on (default 127.0.0.1:19847)
  -i, --interval float     Sensor update interval in seconds (default 1.0)
  -o, --opt strings        Sensor options (e.g., lhm.url=http://localhost:8085/data.json)
```

Build the theme first, then serve it:

```bash
sensorview theme build trofeo
sensorview serve trofeo
```

To use a phone or tablet as the panel, bind every interface. The addresses to
open are printed on startup, each labelled with its network interface, which
matters on a machine with VPN or Hyper-V adapters:

```bash
sensorview serve trofeo --addr 0.0.0.0:19847
```

```
Serving theme: trofeo
[serve] Local:     http://localhost:19847/?ws=19847
[serve] Phone/LAN: http://192.168.1.50:19847/?ws=19847  (Ethernet)
[serve] Warning: sensor readings are served without authentication to anyone on this network
```

The default port is 19847 because the theme SDK probes it first, so the page and
its WebSocket meet on the same port with no query parameter needed.

> **The sensor feed has no authentication.** Anyone who can reach that port can
> read your system metrics. The default binds loopback only; widen it only on a
> network you trust. Windows may also need an inbound firewall rule for the port.

### Management Studio

Native themes can be created and edited without writing JSON. Start a native
dashboard and open the embedded, localhost-only editor:

```bash
sensorview run --renderer native
sensorview ui
```

The Studio provides a fixed-pixel free canvas, live sensor bindings, bars,
gauges and history charts, static image/image-widget support, custom fonts,
video or image crop/trim processing through FFmpeg, native PNG preview,
undo/redo, layer controls, proportional canvas resizing, theme ZIP
import/export, and controlled hot Apply. Draft edits do not affect the physical
panel until **Apply to panel** is selected.

Legacy Trofeo V1 themes are read-only in Studio. Cloning one creates a separate
pixel-equivalent V2 copy with editable layers and copied assets, leaving the
known-good V1 definition untouched. **Native preview** is authoritative when
the interactive browser canvas differs due to browser font rasterization.

Uploaded media stays inside the selected theme. Videos are cropped and
pre-rendered to exact panel-sized JPEG frames; FFmpeg runs as one low-priority,
two-thread job to avoid disrupting the desktop. The server accepts only
localhost addresses and mutation requests require the browser session token.
See [Management Studio](docs/management-studio.md) for the full workflow.

The music dashboard currently supports Linux MPRIS players such as Spotify,
VLC, and compatible browser players. It requires `playerctl`. Synchronized
lyrics are loaded from LRCLIB when available.
Lyrics are cached locally for faster reuse and fewer network requests.

See [Media and Music Modes](docs/media-modes.md) for format support, layout
behavior, requirements, lyrics behavior, and service setup.

### Sensor Options

Configure sensor behavior with `--opt` flags or in config.json:

```bash
# Show available sensor options
sensorview sensor opts

# Examples
sensorview run --opt disk.mounts=/,/home --opt network.interface=eth*
```

| Option | Type | Description |
|--------|------|-------------|
| `disk.mounts` | `[]string` | Disk mount points to monitor |
| `network.interface` | `string` | Network interface filter (supports `*` wildcard) |
| `nvidia_gpu.smi_path` | `string` | Custom path to nvidia-smi binary |

### Device Management

```bash
sensorview device list      # List connected USB displays
sensorview device select    # Interactive device selection
sensorview device info      # Show current device and profile info
sensorview device create    # Generate code for new device support
sensorview device reset     # Reset to defaults
```

### Theme Management

```bash
sensorview theme list              # List installed themes
sensorview theme create <name>     # Create from React+TypeScript template
sensorview theme select <name>     # Set active theme
sensorview theme dev [name]        # Start dev server with hot reload
sensorview theme build [name]      # Build theme for production
sensorview theme preview [name]    # Open in browser
sensorview theme delete <name>     # Remove theme
sensorview theme path              # Show themes directory
sensorview theme sdk update [name] # Update SDK in existing theme
sensorview theme browser install   # Download Chrome for Testing
sensorview theme browser status    # Check browser availability
sensorview theme browser remove    # Remove cached browser
sensorview ui                      # Open the running native Management Studio
```

### Panel Control

```bash
sensorview panel status       # Check if panel is connected
sensorview panel test         # Display test pattern
sensorview panel on           # Turn backlight on
sensorview panel off          # Turn backlight off
sensorview panel brightness 5 # Set brightness (0-7)
```

### Sensor Management

```bash
sensorview sensor list              # List all registered sensors
sensorview sensor list -a           # List only available sensors on this system
sensorview sensor opts              # List available sensor options
sensorview sensor types             # Generate TypeScript types for all sensors
sensorview sensor types -o types.ts # Output to file
sensorview sensor create            # Interactive wizard to create a new sensor
```

### Service Management (Autostart)

```bash
sensorview service install          # Install as autostart service
sensorview service install --opt disk.mounts=/  # With sensor options
sensorview service install --music  # Start in now-playing mode
sensorview service install --gif https://example.com/animation.gif
sensorview service install --image /path/to/wallpaper.png
sensorview service install --renderer native --orientation 90
sensorview service uninstall        # Remove autostart service
sensorview service start            # Start the service now
sensorview service stop             # Stop the service
sensorview service status           # Show service status
sensorview service logs             # View service logs
sensorview service logs -f          # Follow logs in real-time
```

Cross-platform support:
- **Linux**: systemd user service (`~/.config/systemd/user/`)
- **macOS**: launchd LaunchAgent (`~/Library/LaunchAgents/`)
- **Windows**: Startup folder + Registry

Re-running `service install` updates the existing service definition. Stop and
start the service afterward to apply the new command line.

### Other Commands

```bash
sensorview benchmark                                      # Measure full-frame FPS
sensorview benchmark --region-width 64 --region-height 32 # Measure regional FPS
sensorview benchmark --animation --target-fps 60          # Test moving regional updates
sensorview benchmark --native-theme trofeo --orientation 90 --duration 30s # Measure actual Trofeo theme FPS
sensorview benchmark --native-theme trofeo --orientation 90 --mode idle --duration 60s
sensorview benchmark --native-theme trofeo --orientation 90 --json
sensorview benchmark --native-theme trofeo --cpu-profile /tmp/sensorview.cpu
sensorview prune              # Remove config and cache (keeps themes)
sensorview prune --all        # Also remove themes
```

### Rendering performance

SensorView automatically uses regional updates for run modes when the selected
USB display supports rectangular writes. The first frame is sent as a full
frame. Later frames are compared against the previous RGB565 buffer, unchanged
frames are skipped, and changed pixels are grouped into cost-aware rectangular
updates. If a regional write fails, SensorView falls back to full-frame writes.

Regional updates help most when the layout is mostly static and only small
areas change, such as numeric sensor values, playback progress, or a clock.
They help least when large parts of the screen change every frame, such as
full-screen GIF/video-style animation, large gradients, or moving backgrounds.

The benchmark command can measure the real device instead of relying on theory:

```bash
# Full-frame transfer speed
sensorview benchmark

# Raw rectangular write speed for a centered 64x32 area
sensorview benchmark --region-width 64 --region-height 32 --frames 100

# End-to-end dirty-region animation test
sensorview benchmark --animation --region-width 64 --region-height 32 --target-fps 60
```

On USB full-speed panels such as the QTKeJi/AIDA64 480×320 display, small
regions can update much faster than full frames, but large full-screen changes
are still limited by USB bandwidth and the panel's update behavior.

Thermalright Trofeo Vision 9.16 LCD devices use a different path: SensorView
renders a 1920×462 framebuffer, JPEG-encodes it, and sends it over the LY bulk
protocol as a full frame. This panel does not support regional RGB565 updates,
so pixel-diff rendering cannot reduce work while a full-screen video is active.
The native path uses a wire-oriented framebuffer: source JPEGs are rotated once
into the SensorView cache (losslessly in TurboJPEG builds), decoded directly
into a reusable physical canvas, and overlaid without rotating the complete
frame again.
Compressed source frames, decoded buffers, JPEG encoder state, LY packets, and
ACK buffers are reused. Linux builds use libjpeg-turbo when compiled with
`-tags turbojpeg`.

For the Trofeo display, use the included theme. It includes both a web theme and
a native JSON theme. The default `auto` renderer selects native mode to avoid
running Chrome:

```bash
sensorview theme select trofeo
sensorview run --orientation 90 --renderer native
# Balanced video profile is 8 FPS; temporary overrides are available:
sensorview run --orientation 90 --renderer native --target-fps 12 --jpeg-quality 80
```

The included `caelestia` theme can follow Caelestia Shell's current Material
palette and wallpaper. Its palette map assigns every theme colour to a
Caelestia scheme role, including translucent surfaces. Run the sync directly,
or use it as Caelestia CLI's wallpaper and theme post-hook:

```bash
./scripts/sync-caelestia-theme
```

The script reads `~/.local/state/caelestia/scheme.json` and the current
wallpaper, generates an optimized 462×1920 runtime theme, and hot-reloads the
native renderer through the local Management Studio API. An already matching
theme is left untouched, and an unavailable API falls back to restarting the
active systemd user service.

The included Trofeo theme adapts to desktop activity on Linux: it uses 24 FPS
while you are active, then changes to a monochrome, video-free dashboard after
20 seconds of inactivity. The monochrome frame is redrawn only when a displayed
value or the minute changes, while its cached pixels are resent once per second
to prevent the panel firmware from restoring the Thermalright splash screen.
One epoll watcher reads meaningful keyboard/mouse events and falls back to the
active cadence when the user cannot read `/dev/input`.

Static native scenes are automatically capped to the sensor sampling cadence.
They are not repeatedly rendered at animation FPS when their pixels have not
changed.

Sensor collection is also adaptive. CPU/GPU/network values update once per
second while active and every five seconds while idle; disk, motherboard, and
DIMM values use slower cadences. Hardware discovery and static sysfs metadata
are cached. NVIDIA data uses NVML directly when available and starts
`nvidia-smi` only as a fallback.

On Arch Linux, install `libjpeg-turbo` and build the accelerated binary with:

```bash
go build -tags turbojpeg -o sensorview .
```

The physical benchmark reports process CPU, delivered FPS, heap use, and
per-stage average/p95 timings. `--cpu-profile` and `--heap-profile` produce
standard Go pprof files. See [Native renderer performance](docs/performance.md)
for the benchmark procedure and pipeline details.

## Adding Device Support

Got a USB display that isn't supported yet? Adding support is easy:

```bash
# Run the interactive wizard
./sensorview device create
```

This prompts you for:
- Device name and ID
- USB Vendor ID and Product ID
- Display resolution
- Color format (RGB565/RGB888) and byte order
- Backlight levels

It generates a skeleton Go file in `pkg/device/` that you can customize.

See [docs/adding-devices.md](docs/adding-devices.md) for detailed protocol research tips.

## Adding Custom Sensors

SensorView uses a modular sensor provider system. Each sensor is a Go provider that implements the `sensors.Provider` interface.

### Built-in Sensors

| Sensor | Platforms | Description |
|--------|-----------|-------------|
| `cpu` | Linux, Windows | Load, frequency, core count, model name; temperature on Linux, or on Windows via LibreHardwareMonitor |
| `memory` | Linux, Windows | RAM usage |
| `disk` | Linux, macOS, Windows | Disk usage per mount point |
| `network` | Linux, Windows | Network interface statistics |
| `motherboard` | Linux, Windows | Fan speeds, CPU voltage, DIMM temperatures; Windows needs LibreHardwareMonitor |
| `nvidia_gpu` | Linux, Windows | NVIDIA GPU via NVML, falling back to `nvidia-smi` |
| `amd_gpu` | Linux | AMD GPU via sysfs |
| `hostname` | all | Machine hostname |

On Windows, load and utilisation come from ordinary Win32 APIs, and NVIDIA
readings from `nvml.dll` with no cgo. Temperatures, fan speeds and voltages are
not reachable from user space at all; see below.

There is no AMD GPU support on Windows. AMD's Windows API is ADLX, which has no
Go bindings, and the Go bindings that do exist target ROCm and therefore Linux.

### Windows: temperatures, fans and voltages

Reading these needs MSR and super I/O access through a signed kernel driver, so
SensorView bridges to [LibreHardwareMonitor](https://github.com/LibreHardwareMonitor/LibreHardwareMonitor)
rather than shipping a driver of its own:

1. Install and run LibreHardwareMonitor.
2. Enable **Options → Remote Web Server → Run**. It is off by default.
3. Install [PawnIO](https://pawnio.eu), which is what makes CPU temperatures
   readable. Without it LHM reports GPU temperatures but no CPU ones, and
   `cpu.temperature` stays absent.

The bridge is then detected automatically. Point it elsewhere if needed:

```bash
sensorview run --opt lhm.url=http://localhost:8085/data.json
```

Sensors LHM does not report are left absent rather than zeroed, so a missing
reading shows as unavailable in a theme instead of a real 0 °C. With
LibreHardwareMonitor not running, the `motherboard` sensor is unavailable and
`cpu.temperature` is omitted; nothing else is affected.

Per-DIMM temperatures are mapped when present, but most boards do not expose
them to LHM at all.

### Create a Custom Sensor

```bash
# Run the interactive wizard
./sensorview sensor create
```

This prompts you for:
- Sensor ID and name
- Target platform (Linux, macOS, Windows, or all)
- Category (system, gpu, storage, network, power)
- Field definitions with types and units

It generates a skeleton Go file in `pkg/sensors/` that you can customize.

### Adding Platform-Specific Implementations

If a sensor already exists but only for certain platforms, running `sensor create` with the same ID will prompt you to add an implementation for a different platform:

```bash
./sensorview sensor create
Sensor ID: cpu
Sensor 'cpu' already exists for platforms: linux, windows
Which platform would you like to add?
  1. linux
  2. darwin (macOS)
  3. windows
```

Providers are split per platform by build tag, one file per sensor: `cpu_linux.go`
and `cpu_windows.go`. Keep the field set identical across platforms, or the
generated TypeScript differs depending on where it was generated.

### Update TypeScript Types

After adding or modifying sensors, regenerate the TypeScript types for themes:

```bash
./sensorview sensor types -o path/to/theme/lib/sensorview/types.ts
```

## Theme Development

Themes are React + TypeScript applications that receive sensor data via WebSocket. A bundled SDK provides React hooks for easy integration.

### Create a theme

```bash
sensorview theme create my-theme
```

### Development workflow (single command!)

```bash
# Start everything with one command:
sensorview theme dev my-theme

# With sensor options:
sensorview theme dev my-theme --opt disk.mounts=/ --opt network.interface=eth*

# This automatically:
# - Detects your package manager (npm/yarn/pnpm/bun)
# - Installs dependencies if needed
# - Starts WebSocket sensor server (port 19847)
# - Starts Vite dev server with HMR (port 15173)
# - Opens your browser
```

The Vite dev server binds every interface, so a second device can load the theme
while you edit it. The reachable addresses are printed on startup:

```
[dev] Vite:      http://localhost:15173
[dev] WebSocket: ws://localhost:19847/ws
[dev] Phone/LAN: http://192.168.1.50:15173/?ws=19847  (Ethernet)
```

### Using a phone or tablet as the panel

Open one of the `Phone/LAN` addresses above on the device. Two things to know:

- Use the IP address, not the hostname. Vite 6 blocks unknown `Host` headers as
  DNS-rebinding protection, and IP literals are always allowed.
- Windows may need an inbound firewall rule for ports 15173 and 19847.

For a panel you leave running, prefer [`sensorview serve`](#serve-a-theme-to-a-browser)
over `theme dev`: it serves the built theme from the single binary, with no Node
process alive.

Themes are authored at 480x320 with fixed pixel sizes. On a phone, either author
at the device's resolution or scale the root element with a CSS `transform`.

### Using the SDK

```tsx
import { useSensorData, useConnectionStatus, formatRate } from "../lib/sensorview";

function App() {
  const data = useSensorData();
  const status = useConnectionStatus();

  if (status !== "connected" || !data) {
    return <div>Connecting...</div>;
  }

  return (
    <div>
      <p>CPU: {data.cpu.load.toFixed(0)}%</p>
      <p>GPU: {data.gpu.temperature?.toFixed(0) ?? "--"}°C</p>
      <p>RAM: {data.memory.percent.toFixed(0)}%</p>
    </div>
  );
}
```

### Build and use

```bash
sensorview theme build my-theme
sensorview theme select my-theme
sensorview run
```

See [docs/creating-themes.md](docs/creating-themes.md) for the full guide.

## NixOS Installation

### Add to your flake.nix

```nix
{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    sensorview.url = "github:kjez66/sensorview";
  };

  outputs = { self, nixpkgs, sensorview, ... }: {
    nixosConfigurations.yourhostname = nixpkgs.lib.nixosSystem {
      system = "x86_64-linux";
      modules = [
        ./configuration.nix
        sensorview.nixosModules.default
        {
          services.sensorview = {
            enable = true;
            interval = 1.0;
            brightness = 7;
            theme = "my-theme";  # or null for built-in renderer
          };
        }
      ];
    };
  };
}
```

### Module options

```nix
services.sensorview = {
  enable = true;
  interval = 1.0;        # Update interval in seconds
  brightness = 7;        # Backlight brightness (0-7)
  theme = null;          # Theme name or null for built-in
  renderer = "auto";     # auto, native, or chrome
  sensorOptions = {      # Sensor-specific options
    "disk.mounts" = [ "/" "/home" ];
    "network.interface" = "eth*";
  };
  user = "sensorview";  # Service user
  group = "sensorview"; # Service group (for USB access)
};
```

## File Locations

| Type | Path |
|------|------|
| Config | `~/.config/sensorview/config.json` |
| Themes | `~/.local/share/sensorview/themes/` |
| Browser cache | `~/.cache/sensorview/browser/` |

On macOS these live under `~/Library/Application Support/sensorview/` and
`~/Library/Caches/sensorview/`; on Windows under `%APPDATA%sensorview` and
`%LOCALAPPDATA%sensorview`.

**Upgrading from SensorPanel?** The first run of any `sensorview` command merges
the old `sensorpanel` directories into their `sensorview` equivalents, so your
device selection, themes and browser cache carry over. The merge is entry by
entry and never overwrites: where the same path exists on both sides, directories
are merged recursively and anything else keeps the newer copy, leaving the old
one behind. An installed autostart service is *not* migrated - run
`sensorview service install` again after uninstalling the old one.

## Architecture

### Device Profiles

Each USB display is supported via a device profile that implements:

```go
type DeviceProfile interface {
    ID() string                           // "qtkeji", "my-device"
    Name() string                         // Human-readable name
    Matches(vid, pid uint16) bool         // USB device matching
    Width() int                           // Display width
    Height() int                          // Display height
    ColorFormat() ColorFormat             // RGB565 or RGB888
    ByteOrder() ByteOrder                 // BigEndian or LittleEndian
    BlitCommand(x, y, w, h, len int) []byte  // Build display command
    BacklightCommand(level int) []byte    // Build backlight command
    ConvertImage(img image.Image) []byte  // Convert to device format
}
```

### Sensor Sources (Linux)

| Metric | Source |
|--------|--------|
| CPU Load | `/proc/stat` |
| CPU Temp | `/sys/class/hwmon/*/temp*_input` |
| CPU Freq | `/sys/devices/system/cpu/cpu0/cpufreq/scaling_cur_freq` |
| GPU (NVIDIA) | `nvidia-smi` |
| GPU (AMD) | `/sys/class/drm/card*/device/` |
| RAM | `/proc/meminfo` |
| Disk | `syscall.Statfs` |
| Network | `/proc/net/dev` |

## Troubleshooting

### Device not found

```bash
# List USB devices
lsusb

# Check if sensorview detects it
./sensorview device list
```

If your device shows in `lsusb` but not in sensorview, it may need a new device profile. Run `sensorview device create` to add support.

### Permission denied

Create a udev rule for your device:

```bash
# Replace XXXX and YYYY with your device's VID and PID
sudo tee /etc/udev/rules.d/99-sensorview.rules << EOF
SUBSYSTEM=="usb", ATTR{idVendor}=="XXXX", ATTR{idProduct}=="YYYY", MODE="0666"
EOF

sudo udevadm control --reload-rules
sudo udevadm trigger
```

On NixOS with the module, udev rules are set up automatically for known devices.

### Theme not rendering

```bash
# Check if browser is installed
sensorview theme browser status

# Install browser if needed
sensorview theme browser install

# Check theme is built
ls ~/.local/share/sensorview/themes/my-theme/dist/
```

### No GPU stats

```bash
# NVIDIA: Check nvidia-smi works
nvidia-smi

# AMD: Check sysfs
ls /sys/class/drm/card*/device/gpu_busy_percent
```

## Development with Mage

SensorView uses [Mage](https://magefile.org/) as its build tool. Install it with:

```bash
go install github.com/magefile/mage@latest
```

### Available Targets

```bash
mage -l              # List all targets

mage build           # Build for current platform (default)
mage install         # Build and install to GOPATH/bin
mage test            # Run all tests
mage vet             # Run go vet
mage lint            # Run golangci-lint
mage check           # Run all checks (vet, test, lint)
mage clean           # Remove build artifacts
mage release         # Cross-compile for all platforms (dist/)
mage dev             # Build and run the dashboard
mage devTheme        # Build and start theme dev mode
```

## Testing

SensorView has comprehensive unit tests with good coverage across all core packages.

### Running Tests

```bash
# Run all tests
go test ./...

# Run with verbose output
go test -v ./...

# Run with race detector
go test -race ./...

# Run with coverage
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out    # Summary by function
go tool cover -html=coverage.out    # Interactive HTML report
```

### Coverage by Package

| Package | Coverage | Description |
|---------|----------|-------------|
| `pkg/device` | ~99% | Device profiles and registry |
| `pkg/server` | ~95% | WebSocket server for themes |
| `pkg/paths` | ~77% | XDG directory handling |
| `pkg/sensors` | ~57% | System sensor collection |
| `pkg/panel` | ~50% | USB panel protocol (hardware-dependent) |
| `pkg/config` | ~40% | Configuration and device discovery |
| `pkg/theme` | ~34% | Theme management and building |

Some packages have lower coverage because they interact with hardware (USB devices), external processes (Chromium), or the filesystem in ways that are difficult to test in isolation.

## Contributing

Contributions are welcome! See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

### Ways to contribute

- **Add device support** - Run `sensorview device create` and submit a PR
- **Create themes** - Share your themes with the community
- **Improve docs** - Help others get started
- **Fix bugs** - Check the issue tracker

## Credits

SensorView is a fork of [oae/sensorpanel](https://github.com/oae/sensorpanel)
by Osman Alperen Elhan, who wrote most of the code in this tree. The USB panel
protocols, device profile system, theme pipeline, sensor architecture, native
renderer, management studio and the Caelestia theme all originate there. This
fork redirects the project at network-attached screens - old tablets and phones
- and adds Windows sensor support on top.

## License

MIT License - See LICENSE file for details. Upstream code remains under its
original MIT license.
