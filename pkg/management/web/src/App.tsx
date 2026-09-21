import { useEffect, useMemo, useRef, useState } from "react";
import { api, assetURL, startSession, themeExportURL } from "./api";
import { Binding, defaultTheme, SensorProvider, Theme, Widget } from "./types";
import { bindingValue, formatValue, resizeThemeInPlace } from "./theme-utils";

type Tab = "widgets" | "sensors" | "assets" | "settings" | "logs";
type ThemeSummary = { Name: string; Metadata: { description?: string }; HasNative: boolean };
type DragState = { id: string; startX: number; startY: number; rect: Widget["rect"]; resize: boolean } | null;
type MediaState = {
  file?: File;
  uploaded?: { id: string; path: string; type: string };
  kind: "image" | "video";
  start: number;
  end: number;
  x: number;
  y: number;
  w: number;
  h: number;
  sourceWidth?: number;
  sourceHeight?: number;
};

const widgetDefaults: Record<Widget["type"], Partial<Widget>> = {
  text: { text: "LABEL", rect: { x: 24, y: 24, width: 240, height: 48 }, style: { color: "#f8fbff", font_size: 24 } },
  value: { rect: { x: 24, y: 90, width: 220, height: 72 }, style: { color: "#f8fbff", font_size: 48 } },
  clock: { text: "15:04", rect: { x: 240, y: 24, width: 190, height: 56 }, style: { color: "#f8fbff", font_size: 36, align: "right" } },
  bar: { rect: { x: 24, y: 190, width: 414, height: 24 }, style: { color: "#2de2ff", track_color: "#243247" } },
  gauge: { rect: { x: 24, y: 250, width: 180, height: 180 }, style: { color: "#2de2ff" } },
  line: { rect: { x: 24, y: 470, width: 414, height: 180 }, style: { color: "#2de2ff", border_width: 2 }, history_seconds: 300 },
  area: { rect: { x: 24, y: 680, width: 414, height: 180 }, style: { color: "#ff4df3", border_width: 2 }, history_seconds: 300 },
  sparkline: { rect: { x: 24, y: 890, width: 414, height: 90 }, style: { color: "#71ffa8", border_width: 2 }, history_seconds: 120 },
  panel: { rect: { x: 18, y: 1010, width: 426, height: 280 }, style: { background: "#061326cc", border_color: "#245cff99", border_width: 2 } },
  image: { rect: { x: 24, y: 1320, width: 414, height: 300 }, style: {} }
};

function App() {
  const [ready, setReady] = useState(false);
  const [connected, setConnected] = useState(false);
  const [error, setError] = useState("");
  const [activeTheme, setActiveTheme] = useState("");
  const [themeList, setThemeList] = useState<ThemeSummary[]>([]);
  const [theme, setTheme] = useState<Theme>(defaultTheme());
  const [providers, setProviders] = useState<SensorProvider[]>([]);
  const [snapshot, setSnapshot] = useState<Record<string, unknown>>({});
  const [selected, setSelected] = useState("");
  const [tab, setTab] = useState<Tab>("widgets");
  const [zoom, setZoom] = useState(0.42);
  const [drag, setDrag] = useState<DragState>(null);
  const [previewURL, setPreviewURL] = useState("");
  const [showNative, setShowNative] = useState(false);
  const [dirty, setDirty] = useState(false);
  const [config, setConfig] = useState<Record<string, any>>({});
  const [logs, setLogs] = useState("");
  const [media, setMedia] = useState<MediaState | null>(null);
  const [job, setJob] = useState<Record<string, unknown> | null>(null);
  const undoStack = useRef<Theme[]>([]);
  const redoStack = useRef<Theme[]>([]);
  const [history, setHistory] = useState({ undo: 0, redo: 0 });
  const canvasRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    let socket: WebSocket | undefined;
    let reconnectTimer = 0;
    let cancelled = false;
    const connect = () => {
      if (cancelled) return;
      const scheme = location.protocol === "https:" ? "wss" : "ws";
      socket = new WebSocket(`${scheme}://${location.host}/api/v1/events`);
      socket.onopen = () => setConnected(true);
      socket.onmessage = event => {
        const message = JSON.parse(event.data);
        if (message.type === "snapshot") setSnapshot(message.sensors || {});
      };
      socket.onclose = () => {
        setConnected(false);
        if (!cancelled) reconnectTimer = window.setTimeout(connect, 2000);
      };
      socket.onerror = () => socket?.close();
    };
    (async () => {
      try {
        await startSession();
        const [themes, sensorSchema, currentConfig, currentSnapshot] = await Promise.all([
          api.themes(), api.sensors(), api.config(), api.snapshot()
        ]);
        setActiveTheme(themes.active);
        setThemeList(themes.themes.filter(item => item.HasNative));
        setProviders(sensorSchema);
        setConfig(currentConfig);
        setSnapshot(currentSnapshot);
        if (themes.active) {
          const loaded = await api.theme(themes.active);
          setTheme(loaded.schema_version >= 2 ? loaded : defaultTheme(loaded.width || 462, loaded.height || 1920));
        }
        if (!cancelled) {
          connect();
        }
        setReady(true);
      } catch (cause) {
        setError(String(cause));
      }
    })();
    return () => {
      cancelled = true;
      window.clearTimeout(reconnectTimer);
      socket?.close();
    };
  }, []);

  const selectedWidget = theme.widgets.find(widget => widget.id === selected);
  const sortedWidgets = useMemo(() => [...theme.widgets].sort((a, b) => (a.z_index || 0) - (b.z_index || 0)), [theme.widgets]);

  function updateHistoryState() {
    setHistory({ undo: undoStack.current.length, redo: redoStack.current.length });
  }

  function checkpoint(value: Theme) {
    undoStack.current.push(structuredClone(value));
    if (undoStack.current.length > 100) undoStack.current.shift();
    redoStack.current = [];
    updateHistoryState();
  }

  function mutate(updater: (draft: Theme) => void, recordHistory = true) {
    setTheme(current => {
      if (recordHistory) checkpoint(current);
      const copy = structuredClone(current);
      updater(copy);
      return copy;
    });
    setDirty(true);
    setShowNative(false);
  }

  function undo() {
    const previous = undoStack.current.pop();
    if (!previous) return;
    setTheme(current => {
      redoStack.current.push(structuredClone(current));
      return previous;
    });
    setDirty(true);
    setShowNative(false);
    updateHistoryState();
  }

  function redo() {
    const next = redoStack.current.pop();
    if (!next) return;
    setTheme(current => {
      undoStack.current.push(structuredClone(current));
      return next;
    });
    setDirty(true);
    setShowNative(false);
    updateHistoryState();
  }

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (!(event.ctrlKey || event.metaKey) || event.key.toLowerCase() !== "z") return;
      event.preventDefault();
      if (event.shiftKey) redo();
      else undo();
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  });

  function addWidget(type: Widget["type"], binding?: Binding) {
    const id = `${type}-${crypto.randomUUID().slice(0, 8)}`;
    const widget = { id, type, z_index: theme.widgets.length, ...structuredClone(widgetDefaults[type]) } as Widget;
    if (binding) widget.binding = binding;
    mutate(draft => draft.widgets.push(widget));
    setSelected(id);
  }

  function updateWidget(patch: Partial<Widget>) {
    mutate(draft => {
      const index = draft.widgets.findIndex(widget => widget.id === selected);
      if (index >= 0) draft.widgets[index] = { ...draft.widgets[index], ...patch };
    });
  }

  function removeSelected() {
    mutate(draft => { draft.widgets = draft.widgets.filter(widget => widget.id !== selected); });
    setSelected("");
  }

  async function nativePreview() {
    try {
      setError("");
      const blob = await api.preview(activeTheme, theme);
      if (previewURL) URL.revokeObjectURL(previewURL);
      setPreviewURL(URL.createObjectURL(blob));
      setShowNative(true);
    } catch (cause) {
      setError(String(cause));
    }
  }

  async function saveAndApply() {
    try {
      setError("");
      await api.saveDraft(activeTheme, theme);
      await api.apply(activeTheme);
      setDirty(false);
      await nativePreview();
    } catch (cause) {
      setError(String(cause));
    }
  }

  async function switchTheme(name: string) {
    try {
      const nextConfig = { ...config, theme: name };
      await api.saveConfig(nextConfig);
      const loaded = await api.theme(name);
      setConfig(nextConfig);
      setActiveTheme(name);
      setTheme(loaded);
      setSelected("");
      setDirty(false);
      setShowNative(false);
      undoStack.current = [];
      redoStack.current = [];
      updateHistoryState();
    } catch (cause) {
      setError(String(cause));
    }
  }

  async function createTheme(clone: boolean) {
    const name = window.prompt("New theme name (lowercase, dashes allowed)");
    if (!name) return;
    try {
      await api.createTheme(
        name,
        clone ? activeTheme || undefined : undefined,
        clone ? undefined : theme.canvas.width,
        clone ? undefined : theme.canvas.height
      );
      const themes = await api.themes();
      setThemeList(themes.themes.filter(item => item.HasNative));
      await switchTheme(name);
    } catch (cause) {
      setError(String(cause));
    }
  }

  async function importTheme(file: File) {
    const fallback = file.name.replace(/\.zip$/i, "").toLowerCase().replace(/[^a-z0-9_-]+/g, "-");
    const name = window.prompt("Imported theme name", fallback);
    if (!name) return;
    try {
      await api.importTheme(name, file);
      const themes = await api.themes();
      setThemeList(themes.themes.filter(item => item.HasNative));
      await switchTheme(name);
    } catch (cause) {
      setError(String(cause));
    }
  }

  function pointerDown(event: React.PointerEvent, widget: Widget, resize = false) {
    if (widget.locked) return;
    event.stopPropagation();
    (event.currentTarget as HTMLElement).setPointerCapture(event.pointerId);
    setSelected(widget.id);
    checkpoint(theme);
    setDrag({ id: widget.id, startX: event.clientX, startY: event.clientY, rect: { ...widget.rect }, resize });
  }

  function pointerMove(event: React.PointerEvent) {
    if (!drag) return;
    const dx = Math.round((event.clientX - drag.startX) / zoom / 4) * 4;
    const dy = Math.round((event.clientY - drag.startY) / zoom / 4) * 4;
    mutate(draft => {
      const widget = draft.widgets.find(item => item.id === drag.id);
      if (!widget) return;
      if (drag.resize) {
        widget.rect.width = Math.max(16, drag.rect.width + dx);
        widget.rect.height = Math.max(16, drag.rect.height + dy);
      } else {
        widget.rect.x = Math.max(0, Math.min(draft.canvas.width - widget.rect.width, drag.rect.x + dx));
        widget.rect.y = Math.max(0, Math.min(draft.canvas.height - widget.rect.height, drag.rect.y + dy));
      }
    }, false);
  }

  async function uploadAsset(file: File, kind: "image" | "video" | "font") {
    try {
      const uploaded = await api.upload(activeTheme, kind, file);
      if (kind === "font") {
        mutate(draft => { draft.assets[uploaded.id] = { type: "font", path: uploaded.path }; });
      } else {
        if (kind === "image") {
          mutate(draft => { draft.assets[uploaded.id] = { type: "image", path: uploaded.path }; });
        }
        setMedia({ file, uploaded, kind, start: 0, end: 0, x: 0, y: 0, w: 1, h: 1 });
      }
    } catch (cause) {
      setError(String(cause));
    }
  }

  async function processMedia() {
    if (!media?.uploaded) return;
    try {
      const response = await api.process({
        theme: activeTheme,
        source: media.uploaded.path,
        kind: media.kind,
        crop_x: media.x, crop_y: media.y, crop_w: media.w, crop_h: media.h,
        start: media.start, end: media.end,
        width: theme.canvas.width, height: theme.canvas.height,
        fps: Number((theme.performance?.active_fps as number) || 24),
        quality: 4
      });
      setJob({ ...response, status: "queued", progress: 0 });
      const poll = window.setInterval(async () => {
        const state = await api.job(response.id);
        setJob(state);
        if (state.status === "complete") {
          window.clearInterval(poll);
          mutate(draft => {
            if (media.kind === "image") {
              const id = `processed-${response.id}`;
              draft.assets[id] = { type: "image", path: `${String(state.output)}/frame_0001.jpg` };
              draft.canvas.background = id;
              delete draft.background_sequence;
            } else {
              draft.canvas.background = undefined;
              draft.background_sequence = {
                path: String(state.output), pattern: "frame_%04d.jpg", fps: Number((theme.performance?.active_fps as number) || 24),
                opacity: 1, cache: "lru"
              };
            }
          });
          setMedia(null);
        } else if (state.status === "failed" || state.status === "cancelled") {
          window.clearInterval(poll);
        }
      }, 700);
    } catch (cause) {
      setError(String(cause));
    }
  }

  if (!ready) return <div className="boot"><div className="spinner" />{error || "Starting SensorView Studio…"}</div>;

  return (
    <div className="app">
      <header>
        <div className="brand"><span className="brand-mark">SP</span><div><b>SensorView</b><small>Management Studio</small></div></div>
        <div className="theme-title"><span className={connected ? "status-dot" : "offline"} title={connected ? "Live sensor stream connected" : "Reconnecting sensor stream"} /><select name="active-theme" value={activeTheme} onChange={event => switchTheme(event.target.value)}>{themeList.map(item => <option key={item.Name} value={item.Name}>{item.Name}</option>)}</select><button title="New blank theme" onClick={() => createTheme(false)}>＋</button><button title="Clone active theme" onClick={() => createTheme(true)}>⧉</button>{dirty && <em>Unsaved</em>}</div>
        <div className="toolbar">
          <button disabled={!history.undo} onClick={undo} title="Undo (Ctrl+Z)">↶</button>
          <button disabled={!history.redo} onClick={redo} title="Redo (Ctrl+Shift+Z)">↷</button>
          <button onClick={() => setZoom(Math.max(.15, zoom - .05))}>−</button><span>{Math.round(zoom * 100)}%</span><button onClick={() => setZoom(Math.min(1, zoom + .05))}>+</button>
          <a className="button-link" href={themeExportURL(activeTheme)} download={`${activeTheme}.zip`}>Export</a>
          <label className="button-link">Import<input name="import-theme" className="hidden-file" type="file" accept=".zip,application/zip" onChange={event => event.target.files?.[0] && importTheme(event.target.files[0])} /></label>
          <button onClick={nativePreview}>Native preview</button>
          <button className="primary" disabled={!activeTheme} onClick={saveAndApply}>Apply to panel</button>
        </div>
      </header>

      <aside className="left">
        <nav>
          {(["widgets", "sensors", "assets", "settings", "logs"] as Tab[]).map(value =>
            <button key={value} className={tab === value ? "active" : ""} onClick={() => setTab(value)}>{value}</button>
          )}
        </nav>
        <div className="side-content">
          {tab === "widgets" && <WidgetLibrary onAdd={addWidget} />}
          {tab === "sensors" && <SensorLibrary providers={providers} snapshot={snapshot} config={config} onConfig={async next => { setConfig(next); await api.saveConfig(next); }} onAdd={binding => addWidget("value", binding)} />}
          {tab === "assets" && <AssetPanel onUpload={uploadAsset} theme={theme} activeTheme={activeTheme} onBackground={id => mutate(draft => { draft.canvas.background = id; delete draft.background_sequence; })} onClear={() => mutate(draft => { draft.canvas.background = undefined; delete draft.background_sequence; })} onRemove={id => mutate(draft => {
            delete draft.assets[id];
            if (draft.canvas.background === id) draft.canvas.background = undefined;
            draft.widgets = draft.widgets.filter(widget => !(widget.type === "image" && widget.asset === id));
            for (const widget of draft.widgets) if (widget.style?.font === id) widget.style.font = undefined;
          })} />}
          {tab === "settings" && <Settings config={config} theme={theme} onTheme={mutate} onConfig={setConfig} onSave={async () => { await api.saveConfig(config); }} />}
          {tab === "logs" && <div><button onClick={() => api.logs().then(value => setLogs(value.logs))}>Refresh logs</button><pre className="logs">{logs}</pre></div>}
        </div>
      </aside>

      <main className="workspace" onPointerMove={pointerMove} onPointerUp={() => setDrag(null)}>
        <div className="rulers">Canvas {theme.canvas.width} × {theme.canvas.height} · snap 4px</div>
        <div className="canvas-shell" style={{ width: theme.canvas.width * zoom, height: theme.canvas.height * zoom }}>
          <div ref={canvasRef} className="canvas" style={{ width: theme.canvas.width, height: theme.canvas.height, transform: `scale(${zoom})` }} onPointerDown={() => setSelected("")}>
            <CanvasBackground theme={theme} activeTheme={activeTheme} />
            {showNative && previewURL ? <img className="native-preview" src={previewURL} /> :
              sortedWidgets.filter(widget => widget.visible !== false).map(widget => <WidgetView key={widget.id} widget={widget} selected={selected === widget.id} snapshot={snapshot} activeTheme={activeTheme} theme={theme} onPointerDown={pointerDown} />)}
          </div>
        </div>
      </main>

      <aside className="inspector">
        {selectedWidget ? <Inspector widget={selectedWidget} providers={providers} theme={theme} onChange={updateWidget} onDelete={removeSelected} /> :
          <CanvasInspector theme={theme} onTheme={mutate} />}
      </aside>

      {error && <div className="toast" onClick={() => setError("")}>{error}</div>}
      {media && <MediaDialog media={media} setMedia={setMedia} activeTheme={activeTheme} outputRatio={theme.canvas.width / theme.canvas.height} onProcess={processMedia} job={job} />}
    </div>
  );
}

function WidgetLibrary({ onAdd }: { onAdd: (type: Widget["type"]) => void }) {
  const types: Widget["type"][] = ["text", "value", "clock", "bar", "gauge", "line", "area", "sparkline", "panel", "image"];
  return <div><h3>Widgets</h3><div className="widget-grid">{types.map(type => <button key={type} onClick={() => onAdd(type)}><span>{widgetIcon(type)}</span>{type}</button>)}</div></div>;
}

function SensorLibrary({ providers, snapshot, config, onConfig, onAdd }: { providers: SensorProvider[]; snapshot: Record<string, unknown>; config: Record<string, any>; onConfig: (value: Record<string, any>) => void; onAdd: (binding: Binding) => void }) {
  const [search, setSearch] = useState("");
  const disabled: string[] = config.disabled_sensors || [];
  const setEnabled = (id: string, enabled: boolean) => {
    const nextDisabled = enabled ? disabled.filter(item => item !== id) : [...new Set([...disabled, id])];
    onConfig({ ...config, disabled_sensors: nextDisabled });
  };
  const setOption = (key: string, value: string) => {
    onConfig({ ...config, sensor_options: { ...(config.sensor_options || {}), [key]: value.includes(",") ? value.split(",").map(item => item.trim()) : value } });
  };
  return <div><h3>Live sensors</h3><input name="sensor-search" placeholder="Search sensors…" value={search} onChange={e => setSearch(e.target.value)} />
    {providers.filter(provider => `${provider.meta.ID} ${provider.meta.Name}`.toLowerCase().includes(search.toLowerCase())).map(provider =>
      <details key={provider.meta.ID} open={provider.available}><summary><input name={`sensor-${provider.meta.ID}`} type="checkbox" checked={!disabled.includes(provider.meta.ID)} onClick={event => event.stopPropagation()} onChange={event => setEnabled(provider.meta.ID, event.target.checked)} /><span className={provider.available ? "online" : "offline"} />{provider.meta.Name || provider.meta.ID}</summary>
        {provider.options?.map(option => <label className="sensor-option" key={option.Key}>{option.Key}<input name={`option-${option.Key}`} value={Array.isArray(config.sensor_options?.[option.Key]) ? config.sensor_options[option.Key].join(",") : config.sensor_options?.[option.Key] || ""} placeholder={option.Default} onChange={event => setOption(option.Key, event.target.value)} /></label>)}
        {provider.meta.Fields.filter(field => field.Type.includes("number")).map(field =>
          <button className="sensor-row" key={field.JSONName} onClick={() => onAdd({ provider: provider.meta.ID, field: field.JSONName, min: 0, max: field.Unit === "%" ? 100 : undefined, format: field.Unit ? `%.0f${field.Unit}` : "%.0f" })}>
            <span><b>{field.JSONName}</b><small>{field.Description}</small></span><code>{liveValue(snapshot, provider.meta.ID, field.JSONName)}</code>
          </button>)}
      </details>)}
  </div>;
}

function AssetPanel({ onUpload, theme, activeTheme, onBackground, onClear, onRemove }: { onUpload: (file: File, type: "image" | "video" | "font") => void; theme: Theme; activeTheme: string; onBackground: (id: string) => void; onClear: () => void; onRemove: (id: string) => void }) {
  return <div><h3>Assets</h3>
    <label className="upload">＋ Upload image<input name="upload-image" type="file" accept="image/*" onChange={e => e.target.files?.[0] && onUpload(e.target.files[0], "image")} /></label>
    <label className="upload">＋ Upload video<input name="upload-video" type="file" accept="video/*" onChange={e => e.target.files?.[0] && onUpload(e.target.files[0], "video")} /></label>
    <label className="upload">＋ Upload font<input name="upload-font" type="file" accept=".ttf,.otf" onChange={e => e.target.files?.[0] && onUpload(e.target.files[0], "font")} /></label>
    {(theme.canvas.background || theme.background_sequence) && <button className="wide" onClick={onClear}>Clear background</button>}
    <div className="asset-list">{Object.entries(theme.assets || {}).map(([id, asset]) =>
      <div className="asset-card" key={id}><button onClick={() => asset.type === "image" && onBackground(id)}>
        {asset.type === "image" ? <img src={assetURL(activeTheme, asset.path)} /> : <span>{asset.type}</span>}<small>{id}</small>
      </button><button className="asset-remove danger" title="Remove from theme" onClick={() => onRemove(id)}>×</button></div>)}</div>
  </div>;
}

function Settings({ config, theme, onTheme, onConfig, onSave }: { config: Record<string, any>; theme: Theme; onTheme: (fn: (draft: Theme) => void) => void; onConfig: (value: Record<string, any>) => void; onSave: () => void }) {
  const performance = theme.performance || {};
  return <div><h3>Panel</h3>
    <label>Brightness<input name="brightness" type="range" min="0" max="7" value={config.brightness ?? 7} onChange={e => onConfig({ ...config, brightness: Number(e.target.value) })} /></label>
    <label>Orientation<select name="orientation" value={config.orientation || 0} onChange={e => onConfig({ ...config, orientation: Number(e.target.value) })}>{[0, 90, 180, 270].map(value => <option key={value}>{value}</option>)}</select></label>
    <label>Sensor interval (seconds)<input name="sensor-interval" type="number" min=".5" step=".1" value={config.update_interval || 1} onChange={e => onConfig({ ...config, update_interval: Math.max(.5, Number(e.target.value)) })} /></label>
    <h3>Performance</h3>
    <label>Active FPS<input name="active-fps" type="number" min="1" max="60" value={Number(performance.active_fps || 24)} onChange={e => onTheme(draft => { draft.performance = { ...draft.performance, active_fps: Number(e.target.value) }; })} /></label>
    <label>Idle FPS<input name="idle-fps" type="number" min="1" max="10" value={Number(performance.idle_fps || 1)} onChange={e => onTheme(draft => { draft.performance = { ...draft.performance, idle_fps: Number(e.target.value) }; })} /></label>
    <label>Idle timeout<input name="idle-timeout" type="number" min="5" value={Number(performance.idle_timeout_seconds || 20)} onChange={e => onTheme(draft => { draft.performance = { ...draft.performance, idle_timeout_seconds: Number(e.target.value) }; })} /></label>
    <button className="primary wide" onClick={onSave}>Save panel settings</button>
  </div>;
}

function CanvasInspector({ theme, onTheme }: { theme: Theme; onTheme: (fn: (draft: Theme) => void) => void }) {
  const [width, setWidth] = useState(theme.canvas.width);
  const [height, setHeight] = useState(theme.canvas.height);
  useEffect(() => {
    setWidth(theme.canvas.width);
    setHeight(theme.canvas.height);
  }, [theme.canvas.width, theme.canvas.height]);
  const resize = () => {
    if (width < 16 || height < 16) return;
    onTheme(draft => {
      resizeThemeInPlace(draft, width, height);
    });
  };
  return <div className="empty-inspector"><b>Canvas</b><p>Select a widget to edit it, or resize the entire layout proportionally.</p>
    <div className="two-grid"><label>Width<input name="canvas-width" type="number" min="16" value={width} onChange={event => setWidth(Number(event.target.value))} /></label><label>Height<input name="canvas-height" type="number" min="16" value={height} onChange={event => setHeight(Number(event.target.value))} /></label></div>
    <button className="wide" disabled={width === theme.canvas.width && height === theme.canvas.height} onClick={resize}>Resize layout</button>
    {theme.background_sequence && <small className="hint">Reprocess the video background after changing canvas dimensions.</small>}
  </div>;
}

function CanvasBackground({ theme, activeTheme }: { theme: Theme; activeTheme: string }) {
  const asset = theme.canvas.background ? theme.assets?.[theme.canvas.background] : undefined;
  if (asset?.type === "image") return <img className="canvas-background" src={assetURL(activeTheme, asset.path)} />;
  if (theme.background_sequence?.path) return <img className="canvas-background" src={assetURL(activeTheme, `${theme.background_sequence.path}/frame_0001.jpg`)} />;
  return null;
}

function WidgetView({ widget, selected, snapshot, onPointerDown, activeTheme, theme }: { widget: Widget; selected: boolean; snapshot: Record<string, unknown>; onPointerDown: (event: React.PointerEvent, widget: Widget, resize?: boolean) => void; activeTheme: string; theme: Theme }) {
  const style: React.CSSProperties = {
    left: widget.rect.x, top: widget.rect.y, width: widget.rect.width, height: widget.rect.height,
    zIndex: widget.z_index, opacity: widget.opacity ?? 1, color: widget.style?.color, background: widget.style?.background,
    border: widget.style?.border_width ? `${widget.style.border_width}px solid ${widget.style.border_color || "#245cff"}` : undefined,
    borderRadius: widget.style?.radius,
    fontSize: widget.style?.font_size, justifyContent: widget.style?.align === "center" ? "center" : widget.style?.align === "right" ? "flex-end" : "flex-start",
    transform: widget.rotation ? `rotate(${widget.rotation}deg)` : undefined
  };
  const raw = widget.binding ? bindingValue(snapshot, widget.binding) : undefined;
  const numeric = Number(raw || 0) * (widget.binding?.scale || 1) + (widget.binding?.offset || 0);
  const min = widget.binding?.min ?? 0, max = widget.binding?.max ?? 100;
  const dynamicMax = widget.max_binding ? Number(bindingValue(snapshot, widget.max_binding) || max) : max;
  const pct = Math.max(0, Math.min(100, (numeric - min) * 100 / Math.max(1, dynamicMax - min)));
  let content: React.ReactNode = widget.text || widget.type.toUpperCase();
  if (widget.type === "panel") content = null;
  if (widget.type === "text") {
    let text = widget.binding ? formatValue(raw, widget.binding) : (widget.text || "");
    for (const [name, binding] of Object.entries(widget.bindings || {})) {
      text = text.replaceAll(`{${name}}`, formatValue(bindingValue(snapshot, binding), binding));
    }
    if (widget.uppercase) text = text.toUpperCase();
    if (widget.max_length && text.length > widget.max_length) text = `${text.slice(0, Math.max(0, widget.max_length - 3))}...`;
    content = text;
  }
  if (widget.type === "value") content = formatValue(raw, widget.binding);
  if (widget.type === "clock") {
    const now = new Date();
    content = widget.text?.toLowerCase().includes("mon")
      ? now.toLocaleDateString([], { weekday: "short", month: "short", day: "numeric" }).toUpperCase()
      : `${String(now.getHours()).padStart(2, "0")}:${String(now.getMinutes()).padStart(2, "0")}`;
  }
  if (widget.type === "bar") content = <div className="bar-track" style={{ background: widget.style?.track_color }}><i style={{ width: `${pct}%`, background: widget.style?.color }} /></div>;
  if (widget.type === "gauge") content = <div className="gauge" style={{ background: `conic-gradient(${widget.style?.color || "#2de2ff"} ${pct * 2.6}deg, #17243a 0deg)` }} />;
  if (["line", "area", "sparkline"].includes(widget.type)) content = <FakeChart color={widget.style?.color || "#2de2ff"} fill={widget.type === "area"} />;
  if (widget.type === "image" && widget.asset && theme.assets[widget.asset]) content = <img src={assetURL(activeTheme, theme.assets[widget.asset].path)} />;
  return <div className={`canvas-widget ${selected ? "selected" : ""} type-${widget.type}`} style={style} onPointerDown={event => onPointerDown(event, widget)}>
    {content}{selected && <span className="resize" onPointerDown={event => onPointerDown(event, widget, true)} />}
  </div>;
}

function FakeChart({ color, fill }: { color: string; fill: boolean }) {
  return <svg viewBox="0 0 100 40" preserveAspectRatio="none"><path d="M0 32 L12 20 L24 27 L38 8 L52 17 L64 11 L78 24 L90 7 L100 14" fill={fill ? color + "33" : "none"} stroke={color} strokeWidth="2" vectorEffect="non-scaling-stroke" /></svg>;
}

function Inspector({ widget, providers, theme, onChange, onDelete }: { widget: Widget; providers: SensorProvider[]; theme: Theme; onChange: (patch: Partial<Widget>) => void; onDelete: () => void }) {
  const binding = widget.binding || { provider: "", field: "", min: 0, max: 100 };
  const provider = providers.find(item => item.meta.ID === binding.provider);
  const setRect = (key: keyof Widget["rect"], value: number) => onChange({ rect: { ...widget.rect, [key]: value } });
  const setStyle = (key: string, value: unknown) => onChange({ style: { ...widget.style, [key]: value } });
  const setSeries = (index: number, patch: Partial<Binding>) => {
    const series = [...(widget.series || [])];
    series[index] = { ...series[index], ...patch };
    onChange({ series });
  };
  return <div><div className="inspector-head"><div><b>{widget.type}</b><small>{widget.id}</small></div><button className="danger" onClick={onDelete}>Delete</button></div>
    <h4>Frame</h4><div className="four-grid">{(["x", "y", "width", "height"] as const).map(key => <label key={key}>{key}<input name={`widget-${key}`} type="number" value={widget.rect[key]} onChange={e => setRect(key, Number(e.target.value))} /></label>)}</div>
    <div className="two-grid"><label>Rotation<select name="widget-rotation" value={widget.rotation || 0} onChange={e => onChange({ rotation: Number(e.target.value) as Widget["rotation"] })}>{[0, 90, 180, 270].map(value => <option key={value} value={value}>{value}°</option>)}</select></label><label>Layer<input name="widget-layer" type="number" value={widget.z_index || 0} onChange={e => onChange({ z_index: Number(e.target.value) })} /></label></div>
    <div className="toggle-row"><label><input name="widget-visible" type="checkbox" checked={widget.visible !== false} onChange={e => onChange({ visible: e.target.checked })} /> Visible</label><label><input name="widget-locked" type="checkbox" checked={Boolean(widget.locked)} onChange={e => onChange({ locked: e.target.checked })} /> Locked</label></div>
    <label>Opacity<input name="widget-opacity" type="range" min="0.05" max="1" step=".05" value={widget.opacity ?? 1} onChange={e => onChange({ opacity: Number(e.target.value) })} /></label>
    <h4>Style</h4>
    <label>Color<input name="widget-color" type="color" value={(widget.style?.color || "#f8fbff").slice(0, 7)} onChange={e => setStyle("color", e.target.value)} /></label>
    <label>Background<input name="widget-background" value={widget.style?.background || ""} placeholder="#00000088" onChange={e => setStyle("background", e.target.value)} /></label>
    <div className="two-grid"><label>Border color<input type="color" value={(widget.style?.border_color || "#245cff").slice(0, 7)} onChange={e => setStyle("border_color", e.target.value)} /></label><label>Border width<input type="number" min="0" value={widget.style?.border_width || 0} onChange={e => setStyle("border_width", Number(e.target.value))} /></label></div>
    <div className="two-grid"><label>Corner radius<input type="number" min="0" value={widget.style?.radius || 0} onChange={e => setStyle("radius", Number(e.target.value))} /></label><label>Alignment<select value={widget.style?.align || "left"} onChange={e => setStyle("align", e.target.value)}>{["left", "center", "right"].map(value => <option key={value}>{value}</option>)}</select></label></div>
    {["bar", "gauge"].includes(widget.type) && <label>Track color<input type="color" value={(widget.style?.track_color || "#243247").slice(0, 7)} onChange={e => setStyle("track_color", e.target.value)} /></label>}
    <label>Font size<input name="widget-font-size" type="number" value={widget.style?.font_size || 24} onChange={e => setStyle("font_size", Number(e.target.value))} /></label>
    <label>Font<select name="widget-font" value={widget.style?.font || ""} onChange={e => setStyle("font", e.target.value)}><option value="">Built-in bitmap</option>{Object.entries(theme.assets || {}).filter(([, asset]) => asset.type === "font").map(([id]) => <option key={id} value={id}>{id}</option>)}</select></label>
    {widget.type === "image" && <label>Image asset<select value={widget.asset || ""} onChange={e => onChange({ asset: e.target.value })}><option value="">Select…</option>{Object.entries(theme.assets || {}).filter(([, asset]) => asset.type === "image").map(([id]) => <option key={id} value={id}>{id}</option>)}</select></label>}
    {widget.type === "text" && <label>Text<input value={widget.text || ""} onChange={e => onChange({ text: e.target.value })} /></label>}
    {!["text", "clock", "panel", "image"].includes(widget.type) && <><h4>Sensor binding</h4>
      <label>Provider<select name="binding-provider" value={binding.provider} onChange={e => onChange({ binding: { ...binding, provider: e.target.value, field: "" } })}><option value="">Select…</option>{providers.filter(p => p.available).map(p => <option key={p.meta.ID} value={p.meta.ID}>{p.meta.Name || p.meta.ID}</option>)}</select></label>
      <label>Field<select name="binding-field" value={binding.field} onChange={e => onChange({ binding: { ...binding, field: e.target.value } })}><option value="">Select…</option>{provider?.meta.Fields.filter(f => f.Type.includes("number")).map(f => <option key={f.JSONName} value={f.JSONName}>{f.JSONName} {f.Unit}</option>)}</select></label>
      <div className="two-grid"><label>Array item key<input value={binding.item_key || ""} placeholder="mount_point" onChange={e => onChange({ binding: { ...binding, item_key: e.target.value } })} /></label><label>Array item value<input value={binding.item_value || ""} placeholder="/" onChange={e => onChange({ binding: { ...binding, item_value: e.target.value } })} /></label></div>
      <div className="two-grid"><label>Min<input type="number" value={binding.min ?? 0} onChange={e => onChange({ binding: { ...binding, min: Number(e.target.value) } })} /></label><label>Max<input type="number" value={binding.max ?? 100} onChange={e => onChange({ binding: { ...binding, max: Number(e.target.value) } })} /></label></div>
      <div className="two-grid"><label>Scale<input type="number" step=".01" value={binding.scale ?? 1} onChange={e => onChange({ binding: { ...binding, scale: Number(e.target.value) } })} /></label><label>Offset<input type="number" step=".01" value={binding.offset ?? 0} onChange={e => onChange({ binding: { ...binding, offset: Number(e.target.value) } })} /></label></div>
      <label className="check-label"><input type="checkbox" checked={Boolean(binding.clamp)} onChange={e => onChange({ binding: { ...binding, clamp: e.target.checked } })} /> Clamp transformed value to min/max</label>
      <label>Format<input value={binding.format || "%.0f"} onChange={e => onChange({ binding: { ...binding, format: e.target.value } })} /></label>
      {["line", "area", "sparkline"].includes(widget.type) && <><label>History seconds<input type="number" min="1" max="3600" value={widget.history_seconds || 300} onChange={e => onChange({ history_seconds: Number(e.target.value) })} /></label>
        <h4>Additional series</h4>
        {(widget.series || []).map((series, index) => {
          const seriesProvider = providers.find(item => item.meta.ID === series.provider);
          return <div className="series-row" key={index}><select value={series.provider} onChange={e => setSeries(index, { provider: e.target.value, field: "" })}><option value="">Provider…</option>{providers.filter(item => item.available).map(item => <option key={item.meta.ID} value={item.meta.ID}>{item.meta.Name || item.meta.ID}</option>)}</select><select value={series.field} onChange={e => setSeries(index, { field: e.target.value })}><option value="">Field…</option>{seriesProvider?.meta.Fields.filter(field => field.Type.includes("number")).map(field => <option key={field.JSONName} value={field.JSONName}>{field.JSONName}</option>)}</select><button onClick={() => onChange({ series: (widget.series || []).filter((_, itemIndex) => itemIndex !== index) })}>×</button></div>;
        })}
        <button className="wide" onClick={() => onChange({ series: [...(widget.series || []), { provider: "", field: "", min: binding.min, max: binding.max }] })}>＋ Add series</button>
      </>}
    </>}
  </div>;
}

function MediaDialog({ media, setMedia, activeTheme, outputRatio, onProcess, job }: { media: MediaState; setMedia: (value: MediaState | null) => void; activeTheme: string; outputRatio: number; onProcess: () => void; job: Record<string, unknown> | null }) {
  const [draggingCrop, setDraggingCrop] = useState(false);
  const source = media.uploaded ? assetURL(activeTheme, media.uploaded.path) : "";
  const sourceRatio = (media.sourceWidth || 1) / (media.sourceHeight || 1);
  const setSourceSize = (width: number, height: number, duration = 0) => {
    const ratio = width / height;
    const w = ratio > outputRatio ? outputRatio / ratio : 1;
    const h = ratio > outputRatio ? 1 : ratio / outputRatio;
    setMedia({ ...media, sourceWidth: width, sourceHeight: height, w, h, x: (1 - w) / 2, y: (1 - h) / 2, end: media.end || duration });
  };
  const setPosition = (key: "x" | "y", value: number) => {
    const limit = key === "x" ? 1 - media.w : 1 - media.h;
    setMedia({ ...media, [key]: Math.max(0, Math.min(limit, value)) });
  };
  const setSize = (value: number) => {
    let w = Math.max(.05, Math.min(1, value));
    let h = w * sourceRatio / outputRatio;
    if (h > 1) {
      h = 1;
      w = h * outputRatio / sourceRatio;
    }
    setMedia({ ...media, w, h, x: Math.min(media.x, 1 - w), y: Math.min(media.y, 1 - h) });
  };
  const placeCrop = (event: React.PointerEvent<HTMLDivElement>) => {
    const bounds = event.currentTarget.getBoundingClientRect();
    const centerX = (event.clientX - bounds.left) / bounds.width;
    const centerY = (event.clientY - bounds.top) / bounds.height;
    setMedia({
      ...media,
      x: Math.max(0, Math.min(1 - media.w, centerX - media.w / 2)),
      y: Math.max(0, Math.min(1 - media.h, centerY - media.h / 2))
    });
  };
  return <div className="modal-backdrop"><div className="modal"><div className="modal-head"><div><b>Crop {media.kind}</b><small>Output aspect ratio is locked to the panel canvas</small></div><button onClick={() => setMedia(null)}>×</button></div>
    <div className="media-preview"><div className="media-stage" style={{ aspectRatio: `${media.sourceWidth || 16}/${media.sourceHeight || 9}` }} onPointerDown={event => { setDraggingCrop(true); event.currentTarget.setPointerCapture(event.pointerId); placeCrop(event); }} onPointerMove={event => draggingCrop && placeCrop(event)} onPointerUp={() => setDraggingCrop(false)}>
      {media.kind === "video" ? <video src={source} controls loop onLoadedMetadata={event => setSourceSize(event.currentTarget.videoWidth, event.currentTarget.videoHeight, event.currentTarget.duration)} /> : <img src={source} onLoad={event => setSourceSize(event.currentTarget.naturalWidth, event.currentTarget.naturalHeight)} />}
      <div className="crop-box" style={{ left: `${media.x * 100}%`, top: `${media.y * 100}%`, width: `${media.w * 100}%`, height: `${media.h * 100}%` }} />
    </div></div>
    <div className="crop-controls">
      {(["x", "y"] as const).map(key => <label key={key}>{key.toUpperCase()}<input type="range" min="0" max={key === "x" ? 1 - media.w : 1 - media.h} step=".005" value={media[key]} onChange={e => setPosition(key, Number(e.target.value))} /><span>{media[key].toFixed(3)}</span></label>)}
      <label>SIZE<input type="range" min=".05" max="1" step=".005" value={media.w} onChange={e => setSize(Number(e.target.value))} /><span>{media.w.toFixed(3)}</span></label>
      <label>RATIO<input disabled value={`${media.sourceWidth || "?"}×${media.sourceHeight || "?"}`} /><span>{outputRatio.toFixed(3)}</span></label>
      {media.kind === "video" && <div className="two-grid"><label>Start seconds<input type="number" step=".1" value={media.start} onChange={e => setMedia({ ...media, start: Number(e.target.value) })} /></label><label>End seconds<input type="number" step=".1" value={media.end} onChange={e => setMedia({ ...media, end: Number(e.target.value) })} /></label></div>}
    </div>
    {job && <div className="job"><span>{String(job.status)}</span><progress value={Number(job.progress || 0)} max="1" />{["queued", "running"].includes(String(job.status)) && <button onClick={() => void api.cancelJob(String(job.id))}>Cancel</button>}</div>}
    <button className="primary wide" onClick={onProcess}>Process and use as background</button><small className="hint">Crop is locked to output ratio {outputRatio.toFixed(3)}. FFmpeg runs with two low-priority threads.</small>
  </div></div>;
}

function liveValue(snapshot: Record<string, unknown>, provider: string, field: string) {
  const value = (snapshot[provider] as Record<string, unknown> | undefined)?.[field];
  return value === undefined ? "—" : String(value);
}

function widgetIcon(type: string) {
  return ({ text: "T", value: "42", clock: "◷", bar: "▬", gauge: "◔", line: "⌁", area: "◩", sparkline: "⌁", panel: "□", image: "▧" } as Record<string, string>)[type];
}

export default App;
