import { build } from "esbuild";

const banner = 'import{createRequire as __cr}from"module";var require=__cr(import.meta.url);';

const entries = [
  { entry: "src/index.ts", out: "out/index.mjs" },
  { entry: "src/mcp-tasks.ts", out: "out/mcp-tasks.mjs" },
  { entry: "src/mcp-profile.ts", out: "out/mcp-profile.mjs" },
  { entry: "src/mcp-memory.ts", out: "out/mcp-memory.mjs", external: ["@huggingface/transformers", "onnxruntime-node", "sharp"] },
  { entry: "src/mcp-swarm.ts", out: "out/mcp-swarm.mjs" },
  { entry: "src/mcp-nix.ts", out: "out/mcp-nix.mjs" },
  { entry: "src/mcp-file.ts", out: "out/mcp-file.mjs" },
  { entry: "src/mcp-history.ts", out: "out/mcp-history.mjs" },
];

for (const { entry, out, external } of entries) {
  await build({
    entryPoints: [entry],
    bundle: true,
    format: "esm",
    platform: "node",
    target: "node24",
    outfile: out,
    banner: { js: banner },
    ...(external ? { external } : {}),
  });
}
