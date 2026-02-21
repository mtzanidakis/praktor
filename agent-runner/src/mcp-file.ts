import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { z } from "zod";
import { readFileSync, statSync } from "fs";
import { basename, extname } from "path";
import { sendIPC } from "./ipc.js";

const MAX_FILE_SIZE = 12 * 1024 * 1024; // 12MB

const MIME_TYPES: Record<string, string> = {
  ".png": "image/png",
  ".jpg": "image/jpeg",
  ".jpeg": "image/jpeg",
  ".gif": "image/gif",
  ".webp": "image/webp",
  ".svg": "image/svg+xml",
  ".pdf": "application/pdf",
  ".txt": "text/plain",
  ".csv": "text/csv",
  ".json": "application/json",
  ".html": "text/html",
  ".xml": "application/xml",
  ".zip": "application/zip",
  ".tar": "application/x-tar",
  ".gz": "application/gzip",
  ".mp3": "audio/mpeg",
  ".mp4": "video/mp4",
  ".wav": "audio/wav",
  ".ogg": "audio/ogg",
};

function detectMimeType(filePath: string): string {
  const ext = extname(filePath).toLowerCase();
  return MIME_TYPES[ext] || "application/octet-stream";
}

const server = new McpServer({
  name: "praktor-file",
  version: "1.0.0",
});

server.tool(
  "file_send",
  "Send a file to the user via Telegram. Supports images (sent as photos), documents, PDFs, etc. Max file size: 12MB.",
  {
    path: z.string().describe("Absolute path to the file in the container"),
    caption: z.string().optional().describe("Optional caption for the file"),
  },
  async ({ path, caption }) => {
    // Check file exists and size
    let stat;
    try {
      stat = statSync(path);
    } catch {
      return {
        content: [{ type: "text" as const, text: `Error: file not found: ${path}` }],
      };
    }

    if (stat.size > MAX_FILE_SIZE) {
      const sizeMB = (stat.size / (1024 * 1024)).toFixed(1);
      return {
        content: [
          {
            type: "text" as const,
            text: `Error: file too large (${sizeMB}MB). Maximum size is 12MB.`,
          },
        ],
      };
    }

    // Read and encode
    const data = readFileSync(path).toString("base64");
    const name = basename(path);
    const mimeType = detectMimeType(path);

    const resp = await sendIPC("send_file", {
      name,
      data,
      mime_type: mimeType,
      caption: caption || "",
    });

    if (resp.error) {
      return {
        content: [{ type: "text" as const, text: `Error: ${resp.error}` }],
      };
    }

    return {
      content: [
        { type: "text" as const, text: `File sent: ${name} (${mimeType})` },
      ],
    };
  }
);

async function main(): Promise<void> {
  const transport = new StdioServerTransport();
  await server.connect(transport);
}

main().catch((err) => {
  console.error("MCP file server error:", err);
  process.exit(1);
});
