import { describe, it, expect } from "vitest";
import { swarmToLaunchData, type Swarm } from "../pages/Swarms";

const baseSwarm: Swarm = {
  id: "swarm-1",
  name: "Test Swarm",
  lead_agent: "lead",
  status: "completed",
  task: "do the thing",
};

describe("swarmToLaunchData", () => {
  it("preserves name, task and lead_agent", () => {
    const out = swarmToLaunchData(baseSwarm);
    expect(out.name).toBe("Test Swarm");
    expect(out.task).toBe("do the thing");
    expect(out.lead_agent).toBe("lead");
  });

  it("falls back to 'Swarm' when name is empty", () => {
    expect(swarmToLaunchData({ ...baseSwarm, name: "" }).name).toBe("Swarm");
  });

  it("returns empty arrays when agents and synapses are missing", () => {
    const out = swarmToLaunchData(baseSwarm);
    expect(out.agents).toEqual([]);
    expect(out.synapses).toEqual([]);
  });

  it("maps agent fields, defaulting prompt to empty string", () => {
    const out = swarmToLaunchData({
      ...baseSwarm,
      agents: [
        { agent_id: "a1", role: "writer", prompt: "draft it", workspace: "ws1" },
        { agent_id: "a2", role: "editor", prompt: "", workspace: "ws2" },
      ],
    });
    expect(out.agents).toEqual([
      { agent_id: "a1", role: "writer", prompt: "draft it", workspace: "ws1" },
      { agent_id: "a2", role: "editor", prompt: "", workspace: "ws2" },
    ]);
  });

  it("defaults workspace to agent_id when missing", () => {
    const out = swarmToLaunchData({
      ...baseSwarm,
      agents: [
        { agent_id: "a1", role: "r", prompt: "p", workspace: "" },
      ],
    });
    expect(out.agents[0].workspace).toBe("a1");
  });

  it("maps synapses including the bidirectional flag", () => {
    const out = swarmToLaunchData({
      ...baseSwarm,
      synapses: [
        { from: "a", to: "b", bidirectional: false },
        { from: "b", to: "c", bidirectional: true },
      ],
    });
    expect(out.synapses).toEqual([
      { from: "a", to: "b", bidirectional: false },
      { from: "b", to: "c", bidirectional: true },
    ]);
  });
});
