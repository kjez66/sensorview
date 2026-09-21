let token = "";

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  if (token && init.method && init.method !== "GET") headers.set("X-SensorView-Token", token);
  if (init.body && !(init.body instanceof FormData)) headers.set("Content-Type", "application/json");
  const response = await fetch(path, { ...init, headers });
  if (!response.ok) {
    const body = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(body.error || response.statusText);
  }
  const contentType = response.headers.get("content-type") || "";
  return (contentType.includes("application/json") ? response.json() : response.blob()) as Promise<T>;
}

export async function startSession() {
  const result = await request<{ token: string }>("/api/v1/session");
  token = result.token;
}

export const api = {
  status: () => request<Record<string, unknown>>("/api/v1/status"),
  config: () => request<Record<string, unknown>>("/api/v1/config"),
  saveConfig: (value: unknown) => request("/api/v1/config", { method: "PUT", body: JSON.stringify(value) }),
  sensors: () => request<import("./types").SensorProvider[]>("/api/v1/sensors/schema"),
  snapshot: () => request<Record<string, unknown>>("/api/v1/sensors/snapshot"),
  themes: () => request<{ active: string; themes: Array<{ Name: string; Metadata: { description?: string }; HasNative: boolean }> }>("/api/v1/themes"),
  createTheme: (name: string, cloneFrom?: string, width?: number, height?: number) => request("/api/v1/themes", {
    method: "POST", body: JSON.stringify({ name, clone_from: cloneFrom, width, height })
  }),
  deleteTheme: (name: string) => request(`/api/v1/themes?name=${encodeURIComponent(name)}`, { method: "DELETE" }),
  async importTheme(name: string, file: File) {
    const form = new FormData();
    form.append("file", file);
    return request(`/api/v1/themes/import?name=${encodeURIComponent(name)}`, { method: "POST", body: form });
  },
  theme: (name: string) => request<import("./types").Theme>(`/api/v1/theme?name=${encodeURIComponent(name)}`),
  saveDraft: (name: string, value: unknown) => request(`/api/v1/theme?name=${encodeURIComponent(name)}`, { method: "PUT", body: JSON.stringify(value) }),
  apply: (name: string) => request(`/api/v1/theme/apply?name=${encodeURIComponent(name)}`, { method: "POST" }),
  async preview(name: string, value: unknown) {
    return request<Blob>(`/api/v1/theme/preview?name=${encodeURIComponent(name)}`, { method: "POST", body: JSON.stringify(value) });
  },
  async upload(name: string, type: string, file: File) {
    const form = new FormData();
    form.append("file", file);
    return request<{ id: string; type: string; path: string; name: string }>(
      `/api/v1/assets/upload?name=${encodeURIComponent(name)}&type=${type}`,
      { method: "POST", body: form }
    );
  },
  process: (value: unknown) => request<{ id: string }>("/api/v1/media/process", { method: "POST", body: JSON.stringify(value) }),
  job: (id: string) => request<Record<string, unknown>>(`/api/v1/media/jobs/${id}`),
  cancelJob: (id: string) => request<Record<string, unknown>>(`/api/v1/media/jobs/${id}`, { method: "DELETE" }),
  logs: () => request<{ logs: string }>("/api/v1/logs?lines=120")
};

export const assetURL = (theme: string, path: string) =>
  `/api/v1/assets/file?theme=${encodeURIComponent(theme)}&path=${encodeURIComponent(path)}`;

export const themeExportURL = (theme: string) =>
  `/api/v1/themes/export?name=${encodeURIComponent(theme)}`;
