/**
 * dsh-file-changes — host half.
 *
 * Three jobs:
 *   1. Tap the tools registry for NESTED dispatches (Code Mode SDK sub-calls
 *      and transport sub-dispatches — executions carrying `parent`). Native
 *      model-direct calls already reach the browser with presentation views
 *      (the client half collects those), but nested dispatches carry no views
 *      on the wire, so this listener rebuilds their change records from the
 *      tool arguments. Records are kept in memory per session and served to
 *      the client half through /api/file-changes/changes.
 *   2. Serve /api/file-changes/reveal, which reveals one absolute path in the
 *      operating system's file manager (macOS: open -R, Windows:
 *      explorer /select,, desktop Linux: open the containing directory).
 *   3. (via cordis.patch.yml) join the web profile so the client half ships
 *      in the browser roster.
 */
import { execFile } from "node:child_process";
import { existsSync, statSync } from "node:fs";
import { dirname, isAbsolute, normalize, resolve } from "node:path";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);

export const name = "file-changes";
export const inject = ["webServer"];

/** Per-session ring budget: records older than this or beyond this count drop. */
const MAX_RECORD_AGE_MS = 2 * 60 * 60 * 1000;
const MAX_RECORDS_PER_SESSION = 2000;
const MAX_BODY_BYTES = 64 * 1024;

function isLoopbackOrigin(origin) {
  try {
    const host = new URL(origin).hostname;
    return host === "127.0.0.1" || host === "localhost" || host === "[::1]" || host === "::1";
  } catch {
    return false;
  }
}

/** The file path a mutation tool names: file_path first, then the editor's path. */
function toolPath(args) {
  if (typeof args !== "object" || args === null) return undefined;
  const candidate = args.file_path ?? args.path;
  return typeof candidate === "string" && candidate !== "" ? candidate : undefined;
}

/** Session cwd for one execution, when the live agent exposes it. */
function agentCwd(agent) {
  try {
    const cwd = agent?.session?.header?.cwd;
    return typeof cwd === "string" && cwd !== "" ? cwd : undefined;
  } catch {
    return undefined;
  }
}

export function apply(ctx) {
  /** sessionId -> recent change records ({time, path, status, hunks}). */
  const sessions = new Map();
  /** callId -> pre-execution existence probe for write/create classification. */
  const preExistence = new Map();

  function record(sessionId, change) {
    if (sessionId === undefined) return;
    let list = sessions.get(sessionId);
    if (list === undefined) {
      list = [];
      sessions.set(sessionId, list);
    }
    list.push({ time: Date.now(), ...change });
    if (list.length > MAX_RECORDS_PER_SESSION) {
      const now = Date.now();
      sessions.set(sessionId, list.filter((item) => now - item.time <= MAX_RECORD_AGE_MS).slice(-MAX_RECORDS_PER_SESSION));
    }
  }

  // Waterfall: must call next(). Probe existence BEFORE a nested write runs so
  // the result listener can tell "created" from "overwrite".
  ctx.on("tools/execute", (exec, next) => {
    try {
      if (exec.parent !== undefined && exec.agent !== undefined) {
        const path = toolPath(exec.arguments);
        if (path !== undefined) {
          const cwd = agentCwd(exec.agent);
          const absolute = cwd !== undefined && !isAbsolute(path) ? resolve(cwd, path) : path;
          preExistence.set(String(exec.callId), { existed: existsSync(absolute) });
        }
      }
    } catch {
      // observation is best-effort; never disturb the dispatch
    }
    return next();
  });

  // Nested-dispatch outcomes: rebuild change records from the tool arguments.
  ctx.on("tools/result", (exec, result) => {
    try {
      if (exec.parent === undefined || exec.agent === undefined) return;
      const probe = preExistence.get(String(exec.callId));
      preExistence.delete(String(exec.callId));
      if (result.isError === true) return;
      const args = exec.arguments;
      const path = toolPath(args);
      if (path === undefined) return;
      let change = null;
      if (exec.name === "write") {
        if (typeof args.content !== "string") return;
        change = {
          path,
          status: probe !== undefined && probe.existed ? "modified" : "created",
          hunks: [{ path, oldText: null, newText: args.content }]
        };
      } else if (exec.name === "edit") {
        const oldText = typeof args.old_string === "string" ? args.old_string : "";
        const newText = typeof args.new_string === "string" ? args.new_string : "";
        change = { path, status: "modified", hunks: [{ path, oldText, newText }] };
      } else if (exec.name === "str_replace_editor" && typeof args.command === "string") {
        if (args.command === "create") {
          const newText = typeof args.file_text === "string" ? args.file_text : "";
          change = {
            path,
            status: probe !== undefined && probe.existed ? "modified" : "created",
            hunks: [{ path, oldText: null, newText }]
          };
        } else if (args.command === "str_replace") {
          const oldText = typeof args.old_str === "string" ? args.old_str : "";
          const newText = typeof args.new_str === "string" ? args.new_str : "";
          change = { path, status: "modified", hunks: [{ path, oldText, newText }] };
        } else if (args.command === "insert") {
          const newText = typeof args.new_str === "string" ? args.new_str : "";
          change = { path, status: "modified", hunks: [{ path, oldText: "", newText }] };
        }
      }
      if (change !== null) record(exec.agent.id, change);
    } catch {
      // observation is best-effort
    }
  });

  /** Reveal one absolute path in the OS file manager. */
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
        const path = typeof payload?.path === "string" ? payload.path : "";
        if (!isAbsolute(path)) return send(400, { ok: false, error: "absolute path required" });
        const target = normalize(path);
        if (!existsSync(target)) return send(404, { ok: false, error: "path not found" });
        const stats = statSync(target);
        if (!stats.isFile() && !stats.isDirectory()) return send(400, { ok: false, error: "unsupported path kind" });
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
    }
  });

  /** Recent change records for one session (client filters by turn time window). */
  ctx.webServer.register({
    kind: "exact",
    path: "/api/file-changes/changes",
    handler: (req, res) => {
      res.writeHead(200, { "content-type": "application/json", "cache-control": "no-store" });
      try {
        const url = new URL(req.url ?? "/", "http://localhost");
        const sessionId = url.searchParams.get("sessionId") ?? "";
        const now = Date.now();
        const list = sessions.get(sessionId) ?? [];
        res.end(JSON.stringify({ changes: list.filter((item) => now - item.time <= MAX_RECORD_AGE_MS) }));
      } catch {
        res.end(JSON.stringify({ changes: [] }));
      }
    }
  });
}
