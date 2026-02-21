import { build } from "esbuild";

const banner = 'import{createRequire as __cr}from"module";var require=__cr(import.meta.url);';

const entries = [
  ["src/index.ts", "out/index.mjs"],
  ["src/mcp-tasks.ts", "out/mcp-tasks.mjs"],
  ["src/mcp-profile.ts", "out/mcp-profile.mjs"],
  ["src/mcp-memory.ts", "out/mcp-memory.mjs"],
  ["src/mcp-swarm.ts", "out/mcp-swarm.mjs"],
  ["src/mcp-nix.ts", "out/mcp-nix.mjs"],
];

for (const [entryPoint, outfile] of entries) {
  await build({
    entryPoints: [entryPoint],
    bundle: true,
    format: "esm",
    platform: "node",
    target: "node24",
    outfile,
    banner: { js: banner },
  });
}
