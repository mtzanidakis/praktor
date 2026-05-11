import { describe, it, expect } from "vitest";
import { parseScheduleForEdit } from "../pages/Tasks";

describe("parseScheduleForEdit", () => {
  it("returns the original string when the input is not JSON", () => {
    expect(parseScheduleForEdit("0 8 * * *")).toBe("0 8 * * *");
    expect(parseScheduleForEdit("+5m")).toBe("+5m");
    expect(parseScheduleForEdit("")).toBe("");
  });

  it("extracts cron_expr for cron schedules", () => {
    expect(
      parseScheduleForEdit('{"kind":"cron","cron_expr":"0 8 * * *"}')
    ).toBe("0 8 * * *");
  });

  it("formats interval schedules in hours when divisible", () => {
    expect(
      parseScheduleForEdit('{"kind":"interval","interval_ms":7200000}')
    ).toBe("+2h");
  });

  it("formats interval schedules in minutes when not divisible by hours", () => {
    expect(
      parseScheduleForEdit('{"kind":"interval","interval_ms":300000}')
    ).toBe("+5m");
  });

  it("formats interval schedules in seconds when not divisible by minutes", () => {
    expect(
      parseScheduleForEdit('{"kind":"interval","interval_ms":45000}')
    ).toBe("+45s");
  });

  it("prefers hours over minutes when both would work", () => {
    // 3600000 ms = 60min = 1h. Hour formatting wins.
    expect(
      parseScheduleForEdit('{"kind":"interval","interval_ms":3600000}')
    ).toBe("+1h");
  });

  it("formats one-shot schedules using locale string", () => {
    const at = Date.UTC(2026, 4, 11, 7, 30, 0); // 2026-05-11 07:30 UTC
    const result = parseScheduleForEdit(
      `{"kind":"once","at_ms":${at}}`
    );
    // toLocaleString output varies by env; just verify it's not the JSON
    // and contains some plausible date component.
    expect(result).not.toContain("kind");
    expect(result.length).toBeGreaterThan(0);
  });

  it("returns the raw JSON when interval_ms is zero", () => {
    const json = '{"kind":"interval","interval_ms":0}';
    expect(parseScheduleForEdit(json)).toBe(json);
  });

  it("returns the raw JSON for unknown kinds", () => {
    const json = '{"kind":"weird","foo":42}';
    expect(parseScheduleForEdit(json)).toBe(json);
  });

  it("falls back to the raw string on malformed JSON", () => {
    expect(parseScheduleForEdit("{not json")).toBe("{not json");
  });
});
