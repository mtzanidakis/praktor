import { describe, it, expect } from "vitest";
import { formatFileSize } from "../components/AgentExtensions";

describe("formatFileSize", () => {
  it("formats values under 1 KB in bytes", () => {
    expect(formatFileSize(0)).toBe("0 B");
    expect(formatFileSize(1)).toBe("1 B");
    expect(formatFileSize(1023)).toBe("1023 B");
  });

  it("formats values under 1 MB in KB with one decimal", () => {
    expect(formatFileSize(1024)).toBe("1.0 KB");
    expect(formatFileSize(1536)).toBe("1.5 KB");
    expect(formatFileSize(1024 * 1023)).toBe("1023.0 KB");
  });

  it("formats values 1 MB and above in MB with one decimal", () => {
    expect(formatFileSize(1024 * 1024)).toBe("1.0 MB");
    expect(formatFileSize(1024 * 1024 * 12)).toBe("12.0 MB");
    expect(formatFileSize(1.5 * 1024 * 1024)).toBe("1.5 MB");
  });

  it("uses KB threshold at exactly 1024 bytes (boundary)", () => {
    expect(formatFileSize(1024)).toBe("1.0 KB");
    expect(formatFileSize(1023)).toBe("1023 B");
  });

  it("uses MB threshold at exactly 1024 * 1024 bytes (boundary)", () => {
    expect(formatFileSize(1024 * 1024)).toBe("1.0 MB");
    expect(formatFileSize(1024 * 1024 - 1)).toBe("1024.0 KB");
  });
});
