import { mkdirSync, writeFileSync, readFileSync, existsSync } from "fs";
import { execSync } from "child_process";
import { sendIPC } from "./ipc.js";

interface MCPServerConfig {
  type: "stdio" | "http";
  command?: string;
  args?: string[];
  url?: string;
  env?: Record<string, string>;
  headers?: Record<string, string>;
}

interface MarketplaceConfig {
  source: string;
  name?: string;
}

interface PluginConfig {
  name: string;
  disabled?: boolean;
  requires?: string[];
}

interface SkillConfig {
  description: string;
  content: string;
  requires?: string[];
}

interface AgentExtensions {
  mcp_servers?: Record<string, MCPServerConfig>;
  marketplaces?: MarketplaceConfig[];
  plugins?: PluginConfig[];
  skills?: Record<string, SkillConfig>;
  settings?: Record<string, unknown>;
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

function nixInstall(pkg: string): string | null {
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

function collectDependencies(ext: AgentExtensions): string[] {
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
      console.log(`[extensions] installed skill: ${name}`);
    } catch (err) {
      const msg = `failed to install skill ${name}: ${err}`;
      console.error(`[extensions] ${msg}`);
      errors.push(msg);
    }
  }
}

function applySettings(
  settings: Record<string, unknown>,
  errors: string[]
): void {
  try {
    const settingsPath = "/home/praktor/.claude/settings.json";
    let existing: Record<string, unknown> = {};

    if (existsSync(settingsPath)) {
      try {
        existing = JSON.parse(readFileSync(settingsPath, "utf-8"));
      } catch {
        // corrupt file, overwrite
      }
    }

    // Deep merge: settings from extensions override existing
    const merged = deepMerge(existing, settings);

    mkdirSync("/home/praktor/.claude", { recursive: true });
    writeFileSync(settingsPath, JSON.stringify(merged, null, 2));
    console.log("[extensions] applied settings");
  } catch (err) {
    const msg = `failed to apply settings: ${err}`;
    console.error(`[extensions] ${msg}`);
    errors.push(msg);
  }
}

function deepMerge(
  target: Record<string, unknown>,
  source: Record<string, unknown>
): Record<string, unknown> {
  const result = { ...target };
  for (const [key, value] of Object.entries(source)) {
    if (
      value &&
      typeof value === "object" &&
      !Array.isArray(value) &&
      result[key] &&
      typeof result[key] === "object" &&
      !Array.isArray(result[key])
    ) {
      result[key] = deepMerge(
        result[key] as Record<string, unknown>,
        value as Record<string, unknown>
      );
    } else {
      result[key] = value;
    }
  }
  return result;
}

function deriveMarketplaceName(source: string): string {
  return source.replace(/^https?:\/\//, "").replace(/[/.:]+/g, "-").replace(/-+$/, "");
}

function applyMarketplaces(
  marketplaces: MarketplaceConfig[],
  errors: string[]
): void {
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
      // Skip if the official marketplace source is configured — it's always present
      if (
        mp.source === "anthropics/claude-plugins-official" ||
        mp.name === "claude-plugins-official"
      ) {
        console.log(`[extensions] marketplace built-in, skipping: ${mp.source}`);
        continue;
      }

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
function parseNames(output: string): string[] {
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
    (ext.skills && Object.keys(ext.skills).length > 0) ||
    (ext.settings && Object.keys(ext.settings).length > 0);

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

  // Skills
  if (ext.skills && Object.keys(ext.skills).length > 0) {
    applySkills(ext.skills, result.errors);
  }

  // Settings
  if (ext.settings && Object.keys(ext.settings).length > 0) {
    applySettings(ext.settings, result.errors);
  }

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
