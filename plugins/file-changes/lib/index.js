import { execFile } from "node:child_process";
import { existsSync, statSync } from "node:fs";
import { dirname, isAbsolute, normalize, resolve, sep } from "node:path";
import { promisify } from "node:util";
import z from "@deepseek-ai/schemastery";

const execFileAsync = promisify(execFile);

export const name = "file-changes";
export const inject = ["webServer"];

export const Config = z.object({
  // Absolute roots a reveal may target. When empty, fall back to the runtime
  // workspace (DSH_WORKSPACE) then the user's home directory.
  allowedRoots: z.array(z.string()).default([]),
});

const MAX_BODY_BYTES = 64 * 1024;

function isLoopbackOrigin(origin) {
  try {
    const host = new URL(origin).hostname;
    return host === "127.0.0.1" || host === "localhost" || host === "[::1]" || host === "::1";
  } catch {
    return false;
  }
}

// revealRoots resolves the directories reveal is allowed to touch.
function revealRoots(config) {
  if (Array.isArray(config.allowedRoots) && config.allowedRoots.length > 0) {
    return config.allowedRoots.map((r) => resolve(r));
  }
  const roots = [];
  const ws = process.env.DSH_WORKSPACE;
  if (typeof ws === "string" && ws !== "" && isAbsolute(ws)) roots.push(resolve(ws));
  const home = process.env.HOME;
  if (typeof home === "string" && home !== "" && isAbsolute(home)) roots.push(resolve(home));
  return roots;
}

function withinRoot(target, root) {
  const prefix = root.endsWith(sep) ? root : root + sep;
  return target === root || target.startsWith(prefix);
}

export function apply(ctx, config) {
  ctx.webServer.register({
    kind: "exact",
    path: "/api/file-changes/reveal",
    handler: async (req, res) => {
      const send = (status, body) => {
        res.writeHead(status, { "content-type": "application/json", "cache-control": "no-store" });
        res.end(JSON.stringify(body));
      };
      try {
        if (req.method !== "POST") return send(405, { ok: false, error: "method not allowed" });
        const contentType = String(req.headers["content-type"] ?? "");
        if (!contentType.toLowerCase().startsWith("application/json")) {
          return send(415, { ok: false, error: "application/json required" });
        }
        const origin = req.headers.origin;
        if (typeof origin === "string" && origin !== "" && !isLoopbackOrigin(origin)) {
          return send(403, { ok: false, error: "forbidden origin" });
        }

        let body = "";
        for await (const chunk of req) {
          body += chunk;
          if (body.length > MAX_BODY_BYTES) return send(413, { ok: false, error: "body too large" });
        }
        let payload;
        try {
          payload = JSON.parse(body);
        } catch {
          return send(400, { ok: false, error: "invalid JSON" });
        }

        const raw = typeof payload?.path === "string" ? payload.path : "";
        if (!isAbsolute(raw)) return send(400, { ok: false, error: "absolute path required" });
        const target = normalize(resolve(raw));

        const roots = revealRoots(config);
        if (roots.length > 0 && !roots.some((root) => withinRoot(target, root))) {
          return send(403, { ok: false, error: "path outside workspace" });
        }

        if (!existsSync(target)) return send(404, { ok: false, error: "path not found" });
        const stats = statSync(target);
        if (!stats.isFile() && !stats.isDirectory()) {
          return send(400, { ok: false, error: "unsupported path kind" });
        }

        if (process.platform === "darwin") {
          await execFileAsync("open", ["-R", target]);
        } else if (process.platform === "win32") {
          await execFileAsync("explorer", ["/select,", target]);
        } else {
          await execFileAsync("xdg-open", [stats.isDirectory() ? target : dirname(target)]);
        }
        send(200, { ok: true });
      } catch (error) {
        send(500, { ok: false, error: error instanceof Error ? error.message : String(error) });
      }
    },
  });
}
