import { mkdirSync, writeFileSync, readdirSync, rmSync, existsSync } from "fs";
import { dirname, join } from "path";
import { execSync } from "child_process";
import { sendIPC } from "./ipc.js";

export interface MCPServerConfig {
  type: "stdio" | "http";
  command?: string;
  args?: string[];
  url?: string;
  env?: Record<string, string>;
  headers?: Record<string, string>;
}

export interface MarketplaceConfig {
  source: string;
  name?: string;
}

export interface PluginConfig {
  name: string;
  disabled?: boolean;
  requires?: string[];
}

export interface SkillConfig {
  description: string;
  content: string;
  requires?: string[];
  files?: Record<string, string>; // relative path -> base64-encoded content
}

export interface AgentExtensions {
  mcp_servers?: Record<string, MCPServerConfig>;
  marketplaces?: MarketplaceConfig[];
  plugins?: PluginConfig[];
  skills?: Record<string, SkillConfig>;
}

interface ExtensionResult {
  mcpServers: Record<string, MCPServerConfig>;
  errors: string[];
}

function isNixRunning(): boolean {
  try {
    execSync("pgrep -l nix-daemon", { timeout: 5000 });
    return true;
  } catch {
    return false;
  }
}

function isCommandAvailable(cmd: string): boolean {
  try {
    execSync(`which ${cmd}`, { timeout: 5000, stdio: "pipe" });
    return true;
  } catch {
    return false;
  }
}

// Map binary/command names to their actual nix package names.
const packageAliases: Record<string, string> = {
  uvx: "uv",
};

function nixInstall(pkg: string): string | null {
  pkg = packageAliases[pkg] || pkg;
  try {
    console.log(`[extensions] installing nix package: ${pkg}`);
    execSync(`nix profile install nixpkgs#${pkg}`, {
      timeout: 120000,
      stdio: "pipe",
    });
    console.log(`[extensions] installed: ${pkg}`);
    return null;
  } catch (err) {
    const msg = `failed to install nix package ${pkg}: ${err}`;
    console.error(`[extensions] ${msg}`);
    return msg;
  }
}

export function collectDependencies(ext: AgentExtensions): string[] {
  const deps = new Set<string>();

  // MCP servers (stdio): command is the dependency
  if (ext.mcp_servers) {
    for (const srv of Object.values(ext.mcp_servers)) {
      if (srv.type === "stdio" && srv.command) {
        deps.add(srv.command);
      }
    }
  }

  // Plugins: requires field
  if (ext.plugins) {
    for (const p of ext.plugins) {
      if (p.requires) {
        for (const r of p.requires) deps.add(r);
      }
    }
  }

  // Skills: requires field
  if (ext.skills) {
    for (const s of Object.values(ext.skills)) {
      if (s.requires) {
        for (const r of s.requires) deps.add(r);
      }
    }
  }

  return [...deps];
}

function applySkills(
  skills: Record<string, SkillConfig>,
  errors: string[]
): void {
  // Remove skill directories that are no longer in config
  const skillsDir = "/home/praktor/.claude/skills";
  if (existsSync(skillsDir)) {
    try {
      for (const entry of readdirSync(skillsDir, { withFileTypes: true })) {
        if (entry.isDirectory() && !(entry.name in skills)) {
          const dir = join(skillsDir, entry.name);
          rmSync(dir, { recursive: true, force: true });
          console.log(`[extensions] removed skill: ${entry.name}`);
        }
      }
    } catch (err) {
      const msg = `failed to clean up skills: ${err}`;
      console.error(`[extensions] ${msg}`);
      errors.push(msg);
    }
  }

  for (const [name, skill] of Object.entries(skills)) {
    try {
      const dir = `/home/praktor/.claude/skills/${name}`;
      mkdirSync(dir, { recursive: true });

      const content =
        `---\n` +
        `name: ${name}\n` +
        `description: ${skill.description}\n` +
        `---\n\n` +
        skill.content;

      writeFileSync(`${dir}/SKILL.md`, content);

      // Write additional files
      if (skill.files) {
        for (const [relPath, b64] of Object.entries(skill.files)) {
          const filePath = join(dir, relPath);
          mkdirSync(dirname(filePath), { recursive: true });
          writeFileSync(filePath, Buffer.from(b64, "base64"), { mode: 0o755 });
          console.log(`[extensions] wrote skill file: ${name}/${relPath}`);
        }
      }

      console.log(`[extensions] installed skill: ${name}`);
    } catch (err) {
      const msg = `failed to install skill ${name}: ${err}`;
      console.error(`[extensions] ${msg}`);
      errors.push(msg);
    }
  }
}

export function deriveMarketplaceName(source: string): string {
  return source.replace(/^https?:\/\//, "").replace(/[/.:]+/g, "-").replace(/-+$/, "");
}

const DEFAULT_MARKETPLACE: MarketplaceConfig = {
  source: "anthropics/claude-plugins-official",
  name: "claude-plugins-official",
};

function applyMarketplaces(
  marketplaces: MarketplaceConfig[],
  errors: string[]
): void {
  // Ensure the default marketplace is always included
  const hasDefault = marketplaces.some(
    (mp) =>
      mp.source === DEFAULT_MARKETPLACE.source ||
      mp.name === DEFAULT_MARKETPLACE.name
  );
  if (!hasDefault) {
    marketplaces = [DEFAULT_MARKETPLACE, ...marketplaces];
  }

  // Get currently registered marketplaces
  const registered = getInstalledMarketplaces();

  // Build set of desired marketplace names
  const desiredNames = new Set<string>();
  for (const mp of marketplaces) {
    desiredNames.add(mp.name || deriveMarketplaceName(mp.source));
  }

  // Remove marketplaces that are registered but not in config
  // (skip claude-plugins-official as it's built-in)
  for (const name of registered) {
    if (name === "claude-plugins-official") continue;
    if (!desiredNames.has(name)) {
      try {
        console.log(`[extensions] removing marketplace: ${name}`);
        execSync(`claude plugin marketplace remove ${name}`, {
          timeout: 30000,
          stdio: "pipe",
        });
        console.log(`[extensions] removed marketplace: ${name}`);
      } catch (err) {
        const msg = `failed to remove marketplace ${name}: ${err}`;
        console.error(`[extensions] ${msg}`);
        errors.push(msg);
      }
    }
  }

  // Re-read registered list after removals
  const currentRegistered = getInstalledMarketplaces();

  // Register configured marketplaces
  for (const mp of marketplaces) {
    try {
      const derivedName = mp.name || deriveMarketplaceName(mp.source);

      // Check both the derived name and if any registered name contains the source
      const alreadyRegistered = currentRegistered.includes(derivedName) ||
        currentRegistered.some((r) => mp.source.includes(r) || r.includes(derivedName));

      if (alreadyRegistered) {
        console.log(
          `[extensions] marketplace already registered: ${mp.source}`
        );
        continue;
      }

      console.log(`[extensions] registering marketplace: ${mp.source}`);
      execSync(`claude plugin marketplace add ${mp.source}`, {
        timeout: 30000,
        stdio: "pipe",
      });
      console.log(`[extensions] registered marketplace: ${mp.source}`);
    } catch (err) {
      const msg = `failed to register marketplace ${mp.source}: ${err}`;
      console.error(`[extensions] ${msg}`);
      errors.push(msg);
    }
  }
}

function applyPlugins(plugins: PluginConfig[], errors: string[]): void {
  const installed = getInstalledPlugins();
  const installedNames = new Set(installed.map((p) => p.name));

  // Build set of desired plugin names (base name for matching)
  const desiredNames = new Set<string>();
  for (const p of plugins) {
    desiredNames.add(p.name);
    desiredNames.add(p.name.split("@")[0]);
  }

  // Uninstall plugins that are installed but not in config
  for (const ip of installed) {
    const baseName = ip.name.split("@")[0];
    if (!desiredNames.has(ip.name) && !desiredNames.has(baseName)) {
      try {
        console.log(`[extensions] uninstalling plugin: ${ip.name}`);
        execSync(`claude plugin uninstall ${ip.name}`, {
          timeout: 30000,
          stdio: "pipe",
        });
        console.log(`[extensions] uninstalled plugin: ${ip.name}`);
      } catch (err) {
        const msg = `failed to uninstall plugin ${ip.name}: ${err}`;
        console.error(`[extensions] ${msg}`);
        errors.push(msg);
      }
    }
  }

  // Install/enable/disable configured plugins
  for (const plugin of plugins) {
    try {
      const pluginBase = plugin.name.split("@")[0];
      const existing = installed.find(
        (ip) => ip.name === plugin.name || ip.name === pluginBase || ip.name.startsWith(pluginBase + "@")
      );

      if (existing) {
        // Handle enable/disable state
        if (plugin.disabled && existing.enabled) {
          console.log(`[extensions] disabling plugin: ${plugin.name}`);
          execSync(`claude plugin disable ${plugin.name}`, {
            timeout: 30000,
            stdio: "pipe",
          });
          console.log(`[extensions] disabled plugin: ${plugin.name}`);
        } else if (!plugin.disabled && !existing.enabled) {
          console.log(`[extensions] enabling plugin: ${plugin.name}`);
          execSync(`claude plugin enable ${plugin.name}`, {
            timeout: 30000,
            stdio: "pipe",
          });
          console.log(`[extensions] enabled plugin: ${plugin.name}`);
        } else {
          console.log(`[extensions] plugin already installed: ${plugin.name}`);
        }
        continue;
      }

      // Install new plugin
      console.log(`[extensions] installing plugin: ${plugin.name}`);
      execSync(`claude plugin install ${plugin.name}`, {
        timeout: 60000,
        stdio: "pipe",
      });
      console.log(`[extensions] installed plugin: ${plugin.name}`);

      // Disable immediately if configured as disabled
      if (plugin.disabled) {
        console.log(`[extensions] disabling plugin: ${plugin.name}`);
        execSync(`claude plugin disable ${plugin.name}`, {
          timeout: 30000,
          stdio: "pipe",
        });
      }
    } catch (err) {
      const msg = `failed to install plugin ${plugin.name}: ${err}`;
      console.error(`[extensions] ${msg}`);
      errors.push(msg);
    }
  }
}

// Parse names from CLI output lines like "  ❯ name" or "  ❯ name (disabled)"
export function parseNames(output: string): string[] {
  const names: string[] = [];
  for (const line of output.split("\n")) {
    const match = line.match(/❯\s+(\S+)/);
    if (match) {
      names.push(match[1]);
    }
  }
  return names;
}

interface InstalledPlugin {
  name: string;
  enabled: boolean;
}

function getInstalledPlugins(): InstalledPlugin[] {
  try {
    const output = execSync("claude plugin list 2>/dev/null || true", {
      timeout: 10000,
      stdio: "pipe",
    }).toString();
    const plugins: InstalledPlugin[] = [];
    const lines = output.split("\n");
    for (let i = 0; i < lines.length; i++) {
      const match = lines[i].match(/❯\s+(\S+)/);
      if (match) {
        // Look ahead for status line
        let enabled = true;
        for (let j = i + 1; j < Math.min(i + 5, lines.length); j++) {
          if (lines[j].match(/❯\s+\S/)) break; // next entry
          if (lines[j].includes("disabled")) {
            enabled = false;
            break;
          }
        }
        plugins.push({ name: match[1], enabled });
      }
    }
    return plugins;
  } catch {
    return [];
  }
}

function getInstalledMarketplaces(): string[] {
  try {
    const output = execSync(
      "claude plugin marketplace list 2>/dev/null || true",
      { timeout: 10000, stdio: "pipe" }
    ).toString();
    return parseNames(output);
  } catch {
    return [];
  }
}

export async function applyExtensions(): Promise<ExtensionResult> {
  const result: ExtensionResult = { mcpServers: {}, errors: [] };

  const envData = process.env.AGENT_EXTENSIONS;
  let ext: AgentExtensions = {};
  if (envData && envData !== "{}") {
    try {
      ext = JSON.parse(envData);
    } catch (err) {
      result.errors.push(`failed to parse AGENT_EXTENSIONS: ${err}`);
      return result;
    }
  }

  const hasContent =
    (ext.mcp_servers && Object.keys(ext.mcp_servers).length > 0) ||
    (ext.marketplaces && ext.marketplaces.length > 0) ||
    (ext.plugins && ext.plugins.length > 0) ||
    (ext.skills && Object.keys(ext.skills).length > 0);

  // Even with no config, we need nix to clean up previously installed extensions
  if (!isNixRunning()) {
    if (hasContent) {
      result.errors.push(
        "Extensions configured but nix is not enabled. Enable nix for this agent in the config."
      );
    }
    return result;
  }

  if (hasContent) {
    console.log("[extensions] applying agent extensions...");
  }

  // Collect and install dependencies
  const deps = collectDependencies(ext);
  for (const dep of deps) {
    if (!isCommandAvailable(dep)) {
      const err = nixInstall(dep);
      if (err) {
        result.errors.push(err);
      }
    }
  }

  // MCP Servers — return for merging in query() call
  if (ext.mcp_servers) {
    for (const [name, srv] of Object.entries(ext.mcp_servers)) {
      result.mcpServers[name] = srv;
    }
    console.log(
      `[extensions] ${Object.keys(ext.mcp_servers).length} MCP server(s) configured`
    );
  }

  // Skills — always run to remove previously installed skills
  applySkills(ext.skills || {}, result.errors);

  // Marketplaces — always run to uninstall removed ones (must run before plugins)
  applyMarketplaces(ext.marketplaces || [], result.errors);

  // Plugins — always run to uninstall removed ones
  applyPlugins(ext.plugins || [], result.errors);

  if (hasContent) {
    console.log(
      `[extensions] done (${result.errors.length} error(s))`
    );
  }

  // Report installed state back to host via IPC
  try {
    const finalPlugins = getInstalledPlugins();
    const finalMarketplaces = getInstalledMarketplaces();
    console.log(
      `[extensions] reporting status: ${finalMarketplaces.length} marketplace(s), ${finalPlugins.length} plugin(s)`
    );
    await sendIPC("extension_status", {
      marketplaces: finalMarketplaces,
      plugins: finalPlugins.map((p) => ({
        name: p.name,
        enabled: p.enabled,
      })),
    });
  } catch (err) {
    console.error(`[extensions] failed to report extension status: ${err}`);
  }

  return result;
}
