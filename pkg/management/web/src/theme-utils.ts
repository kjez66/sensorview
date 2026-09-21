import { Binding, Theme } from "./types";

export function resizeThemeInPlace(theme: Theme, width: number, height: number) {
  const scaleX = width / theme.canvas.width;
  const scaleY = height / theme.canvas.height;
  for (const widget of theme.widgets) {
    widget.rect.x = Math.round(widget.rect.x * scaleX);
    widget.rect.y = Math.round(widget.rect.y * scaleY);
    widget.rect.width = Math.max(1, Math.round(widget.rect.width * scaleX));
    widget.rect.height = Math.max(1, Math.round(widget.rect.height * scaleY));
    if (widget.style?.font_size) {
      widget.style.font_size = Math.max(1, Math.round(widget.style.font_size * Math.min(scaleX, scaleY)));
    }
  }
  theme.canvas.width = width;
  theme.canvas.height = height;
  theme.width = width;
  theme.height = height;
}

export function bindingValue(snapshot: Record<string, unknown>, binding: Binding): unknown {
  const direct = directBindingValue(snapshot, binding);
  if (direct !== undefined && (!binding.fallback_on_zero || Number(direct) !== 0)) return direct;
  for (const fallback of binding.fallbacks || []) {
    const value = bindingValue(snapshot, fallback);
    if (value !== undefined && (!binding.fallback_on_zero || Number(value) !== 0)) return value;
  }
  return direct;
}

function directBindingValue(snapshot: Record<string, unknown>, binding: Binding): unknown {
  if (binding.provider === "system" && binding.field === "hostname") return snapshot.hostname || "sensorview";
  const provider = snapshot[binding.provider] as Record<string, unknown> | undefined;
  if (!provider) return undefined;
  const findField = (value: unknown): unknown => {
    if (Array.isArray(value)) {
      for (const item of value) {
        const found = findField(item);
        if (found !== undefined) return found;
      }
    } else if (value && typeof value === "object") {
      const record = value as Record<string, unknown>;
      if (record[binding.field] !== undefined) return record[binding.field];
      for (const item of Object.values(record)) {
        const found = findField(item);
        if (found !== undefined) return found;
      }
    }
    return undefined;
  };
  if (binding.select === "best") {
    const items = Object.values(provider).flatMap(value => Array.isArray(value) ? value : []);
    const best = items.reduce<Record<string, unknown> | undefined>((current, item) => {
      if (!item || typeof item !== "object") return current;
      const record = item as Record<string, unknown>;
      const score = Number(record.percent || record.used_percent || 0) +
        Number(record.rx_bytes_per_sec || record.rx_rate || 0) +
        Number(record.tx_bytes_per_sec || record.tx_rate || 0);
      const currentScore = current ? Number(current.percent || current.used_percent || 0) +
        Number(current.rx_bytes_per_sec || current.rx_rate || 0) +
        Number(current.tx_bytes_per_sec || current.tx_rate || 0) : -1;
      return score > currentScore ? record : current;
    }, undefined);
    return best ? findField(best) : undefined;
  }
  if (!binding.item_key) return provider[binding.field] ?? findField(provider);
  const walk = (value: unknown): unknown => {
    if (Array.isArray(value)) {
      for (const item of value) {
        const found = walk(item);
        if (found !== undefined) return found;
      }
    }
    if (value && typeof value === "object") {
      const record = value as Record<string, unknown>;
      if (String(record[binding.item_key!]) === binding.item_value) return record[binding.field];
      for (const item of Object.values(record)) {
        const found = walk(item);
        if (found !== undefined) return found;
      }
    }
    return undefined;
  };
  return walk(provider);
}

export function formatValue(raw: unknown, binding?: Binding) {
  if (typeof raw === "string") {
    let text = binding?.uppercase ? raw.toUpperCase() : raw;
    if (binding?.max_length && text.length > binding.max_length) {
      text = binding.max_length <= 3 ? text.slice(0, binding.max_length) : `${text.slice(0, binding.max_length - 3)}...`;
    }
    return text;
  }
  let value = Number(raw || 0);
  value = value * (binding?.scale || 1) + (binding?.offset || 0);
  if (binding?.formatter === "clock") return value >= 1000 ? `${(value / 1000).toFixed(2)}G` : `${value.toFixed(0)}M`;
  if (binding?.formatter === "rpm") return value > 0 ? `${value.toFixed(0)}RPM` : "--";
  if (binding?.formatter === "gb_mb") return value > 0 ? `${(value / 1024).toFixed(1)}GB` : "--";
  if (binding?.formatter === "bps") {
    const units = ["B/s", "KB/s", "MB/s", "GB/s", "TB/s"];
    let unit = 0;
    while (value >= 1024 && unit < units.length - 1) { value /= 1024; unit++; }
    return `${value.toFixed(1)}${units[unit]}`;
  }
  if (binding?.formatter === "temperature_or_dash") return value > 0 ? `${value.toFixed(0)}C` : "--";
  if (!binding?.format) return String(Math.round(value));
  return binding.format
    .replace(/%\.?(\d*)f/, (_, digits) => value.toFixed(Number(digits || 0)))
    .replaceAll("%%", "%")
    .replace("{value}", String(value));
}
