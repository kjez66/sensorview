export type Binding = {
  provider: string;
  field: string;
  item_key?: string;
  item_value?: string;
  select?: "best";
  scale?: number;
  offset?: number;
  min?: number;
  max?: number;
  clamp?: boolean;
  format?: string;
  formatter?: "clock" | "rpm" | "gb_mb" | "bps" | "temperature_or_dash";
  max_length?: number;
  uppercase?: boolean;
  fallback_on_zero?: boolean;
  fallbacks?: Binding[];
};

export type Widget = {
  id: string;
  type: "text" | "value" | "clock" | "bar" | "gauge" | "line" | "area" | "sparkline" | "panel" | "image";
  rect: { x: number; y: number; width: number; height: number };
  z_index?: number;
  rotation?: 0 | 90 | 180 | 270;
  opacity?: number;
  visible?: boolean;
  locked?: boolean;
  text?: string;
  uppercase?: boolean;
  max_length?: number;
  asset?: string;
  binding?: Binding;
  bindings?: Record<string, Binding>;
  max_binding?: Binding;
  series?: Binding[];
  history_seconds?: number;
  style?: {
    color?: string;
    background?: string;
    track_color?: string;
    border_color?: string;
    border_width?: number;
    radius?: number;
    font_size?: number;
    font?: string;
    align?: "left" | "center" | "right";
    fill?: boolean;
  };
};

export type Theme = {
  schema_version: number;
  name: string;
  layout: string;
  width: number;
  height: number;
  background?: string;
  canvas: { width: number; height: number; background?: string };
  widgets: Widget[];
  assets: Record<string, { type: string; path: string }>;
  background_sequence?: {
    path: string;
    pattern: string;
    frames?: number;
    fps: number;
    opacity: number;
    cache: string;
  };
  performance?: Record<string, unknown>;
};

export type SensorField = {
  Name: string;
  JSONName: string;
  TSName: string;
  Type: string;
  Unit: string;
  Description: string;
};

export type SensorProvider = {
  meta: {
    ID: string;
    Name: string;
    Description: string;
    Category: string;
    Fields: SensorField[];
    IsArray: boolean;
    ArrayKey: string;
  };
  available: boolean;
  options?: Array<{ Key: string; Type: string; Default: string; Description: string }>;
};

export const defaultTheme = (width = 462, height = 1920): Theme => ({
  schema_version: 2,
  name: "Studio Theme",
  layout: "freeform_v2",
  width,
  height,
  background: "#000000",
  canvas: { width, height },
  widgets: [],
  assets: {},
  performance: {
    profile: "balanced",
    target_fps: 8,
    active_fps: 24,
    idle_fps: 1,
    idle_timeout_seconds: 20,
    jpeg_quality: 68,
    prefetch_frames: 1,
    jpeg_encoder: "auto"
  }
});
