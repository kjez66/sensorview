import { describe, expect, it } from "vitest";
import { bindingValue, formatValue, resizeThemeInPlace } from "./theme-utils";
import { defaultTheme } from "./types";

describe("theme utilities", () => {
  it("resizes widget geometry and font size proportionally", () => {
    const theme = defaultTheme(100, 200);
    theme.widgets.push({
      id: "value",
      type: "value",
      rect: { x: 10, y: 20, width: 30, height: 40 },
      style: { font_size: 20 }
    });
    resizeThemeInPlace(theme, 200, 100);
    expect(theme.widgets[0].rect).toEqual({ x: 20, y: 10, width: 60, height: 20 });
    expect(theme.widgets[0].style?.font_size).toBe(10);
    expect(theme.canvas).toMatchObject({ width: 200, height: 100 });
  });

  it("resolves selected items inside nested sensor arrays", () => {
    const value = bindingValue({
      disk: { disks: [{ mount: "/", percent: 71 }, { mount: "/home", percent: 42 }] }
    }, {
      provider: "disk", field: "percent", item_key: "mount", item_value: "/home"
    });
    expect(value).toBe(42);
  });

  it("formats printf percentages without duplicate percent signs", () => {
    expect(formatValue(57.4, { provider: "cpu", field: "load", format: "%.0f%%" })).toBe("57%");
  });

  it("uses sensor fallbacks and native unit formatters", () => {
    const binding = {
      provider: "cpu", field: "fan_speed", fallback_on_zero: true,
      fallbacks: [{ provider: "motherboard", field: "cpu_fan" }],
      formatter: "rpm" as const
    };
    const value = bindingValue({
      cpu: { fan_speed: 0 },
      motherboard: { cpu_fan: 1420 }
    }, binding);
    expect(formatValue(value, binding)).toBe("1420RPM");
  });

  it("selects the most relevant item from sensor arrays", () => {
    const binding = { provider: "disk", field: "percent", select: "best" as const };
    const value = bindingValue({
      disk: { disks: [{ mount: "/boot", percent: 12 }, { mount: "/", percent: 86 }] }
    }, binding);
    expect(value).toBe(86);
  });
});
