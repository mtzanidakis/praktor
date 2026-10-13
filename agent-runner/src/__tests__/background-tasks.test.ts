import { describe, it, expect, beforeEach } from "vitest";
import {
  backgroundTasksByQuery,
  setBg,
  totalBgTasks,
} from "../index.js";

describe("background task counters", () => {
  beforeEach(() => {
    backgroundTasksByQuery.clear();
  });

  it("reports zero when nothing is active", () => {
    expect(totalBgTasks()).toBe(0);
  });

  it("records the live count of a run", () => {
    setBg("q1", 2);
    expect(totalBgTasks()).toBe(2);
  });

  it("replaces the count on each change (level signal)", () => {
    setBg("q1", 3);
    setBg("q1", 1);
    expect(totalBgTasks()).toBe(1);
  });

  it("sums counts across concurrent runs", () => {
    setBg("__regular", 1);
    setBg("task-1", 2);
    setBg("task-2", 1);
    expect(totalBgTasks()).toBe(4);
  });

  it("a zero count removes the entry", () => {
    setBg("q1", 2);
    setBg("q1", 0);
    expect(backgroundTasksByQuery.has("q1")).toBe(false);
    expect(totalBgTasks()).toBe(0);
  });

  it("one run's change does not affect another run", () => {
    setBg("__regular", 1);
    setBg("task-1", 2);
    setBg("task-1", 0);
    expect(backgroundTasksByQuery.get("__regular")).toBe(1);
    expect(totalBgTasks()).toBe(1);
  });
});
