// src/server/index.ts
import z from "@deepseek-ai/schemastery";
import { createUserMessage } from "@deepseek-ai/dsh-llm";
import { execFile } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, renameSync, rmSync, statSync, writeFileSync } from "node:fs";
import { dirname, isAbsolute, join, resolve, sep } from "node:path";
import { tmpdir } from "node:os";
var name = "diff-review";
var Config = z.object({
  statusPath: z.string().default("/diff-review/status"),
  applyPath: z.string().default("/diff-review/apply"),
  applyHunkPath: z.string().default("/diff-review/apply-hunk"),
  commitPath: z.string().default("/diff-review/commit"),
  pushPath: z.string().default("/diff-review/push"),
  historyPath: z.string().default("/diff-review/history"),
  commitDiffPath: z.string().default("/diff-review/commit-diff"),
  commentsPath: z.string().default("/diff-review/comments"),
  branchesPath: z.string().default("/diff-review/branches"),
  reviewPath: z.string().default("/diff-review/review"),
  prPath: z.string().default("/diff-review/pr"),
  reposPath: z.string().default("/diff-review/repos"),
  filesPath: z.string().default("/diff-review/files"),
  reviewProvider: z.string().default(""),
  reviewModel: z.string().default(""),
  allowedRoots: z.array(z.string()).default([])
});
var MAX_BUFFER = 64 * 1024 * 1024;
function git(cwd, args) {
  return new Promise((resolvePromise) => {
    execFile("git", ["-C", cwd, "-c", "color.ui=never", ...args], { windowsHide: true, maxBuffer: MAX_BUFFER }, (err, stdout, stderr) => {
      if (err) {
        const code = typeof err.code === "number" ? err.code : 1;
        resolvePromise({ code, stdout: stdout ?? "", stderr: stderr ?? "" });
      } else {
        resolvePromise({ code: 0, stdout: stdout ?? "", stderr: stderr ?? "" });
      }
    });
  });
}
function runCmd(cmd, args, opts = {}) {
  return new Promise((resolvePromise) => {
    execFile(cmd, args, { windowsHide: true, maxBuffer: 8 * 1024 * 1024, timeout: opts.timeoutMs ?? 15e3, ...opts.cwd ? { cwd: opts.cwd } : {} }, (err, stdout, stderr) => {
      if (err) {
        const code = typeof err.code === "number" ? err.code : 1;
        resolvePromise({ code, stdout: stdout ?? "", stderr: stderr ?? "" });
      } else {
        resolvePromise({ code: 0, stdout: stdout ?? "", stderr: stderr ?? "" });
      }
    });
  });
}
function sanitizeRepoPath(raw) {
  if (typeof raw !== "string" || !raw.trim()) return { error: 'missing "path"' };
  const p = raw.trim();
  if (isAbsolute(p)) return { error: `path must be repo-relative: ${p}` };
  if (p.startsWith("-")) return { error: `invalid path: ${p}` };
  const segments = p.split(/[\\/]/);
  if (segments.includes("..")) return { error: `path traversal is not allowed: ${p}` };
  return { path: p };
}
function isRecord(v) {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}
function parsePorcelain(stdout) {
  const records = stdout.split("\0").filter((r) => r.length > 0);
  const out = [];
  for (let i = 0; i < records.length; i++) {
    const rec = records[i];
    if (rec.length < 3) continue;
    const xy = rec.slice(0, 2);
    const path = rec.slice(3);
    if (xy === "??" || xy === "!!") continue;
    if (xy[0] === "R" || xy[0] === "C") {
      const orig = i + 1 < records.length ? records[i + 1] : void 0;
      if (orig !== void 0) i++;
      out.push({ path, origPath: orig, xy });
    } else {
      out.push({ path, xy });
    }
  }
  return out;
}
function countLines(diff) {
  let added = 0;
  let deleted = 0;
  for (const line of diff.split("\n")) {
    if (line.startsWith("+") && !line.startsWith("+++")) added++;
    else if (line.startsWith("-") && !line.startsWith("---")) deleted++;
  }
  return { added, deleted };
}
function splitHunks(diffText) {
  const lines = diffText.split("\n");
  const hunks = [];
  let current = null;
  for (const line of lines) {
    if (line.startsWith("@@")) {
      if (current) hunks.push(current.join("\n"));
      current = [line];
    } else if (current) {
      current.push(line);
    }
  }
  if (current) {
    while (current.length > 0 && current[current.length - 1] === "") current.pop();
    if (current.length > 0) hunks.push(current.join("\n"));
  }
  return hunks;
}
function layerHunks(layer, diffText) {
  if (/^(deleted file mode|rename from)/m.test(diffText)) return [];
  return splitHunks(diffText).map((text) => ({ layer, text }));
}
function syntheticUntrackedDiff(path, content) {
  const normalized = content.replace(/\r\n/g, "\n");
  const lines = normalized.split("\n");
  if (lines.length > 0 && lines[lines.length - 1] === "") lines.pop();
  const header = `--- /dev/null
+++ b/${path}
@@ -0,0 +1,${lines.length} @@
`;
  return header + lines.map((l) => `+${l}`).join("\n");
}
var MAX_UNTRACKED_PREVIEW_BYTES = 1024 * 1024;
async function collectDiff(cwd, change) {
  const untracked = change.xy.startsWith("??");
  let diff = "";
  let binary = false;
  let hunks = [];
  if (untracked) {
    const abs = resolve(cwd, change.path);
    try {
      let size = 0;
      try {
        size = statSync(abs).size;
      } catch {
        size = 0;
      }
      if (size > MAX_UNTRACKED_PREVIEW_BYTES) {
        binary = true;
        diff = "Large untracked file (preview disabled)";
      } else {
        const content = readFileSync(abs, "utf8");
        if (content.includes("\0")) {
          binary = true;
          diff = "Binary file (untracked)";
        } else {
          diff = syntheticUntrackedDiff(change.path, content);
          hunks = layerHunks("unstaged", diff);
        }
      }
    } catch {
      diff = "(unreadable)";
    }
  } else {
    const [staged2, unstaged2] = await Promise.all([
      git(cwd, ["diff", "--cached", "--", change.path]),
      git(cwd, ["diff", "--", change.path])
    ]);
    const stagedText = staged2.stdout.trimEnd();
    const unstagedText = unstaged2.stdout.trimEnd();
    diff = [stagedText, unstagedText].filter(Boolean).join("\n");
    if (!diff.trim()) {
      const [b1, b2] = await Promise.all([
        git(cwd, ["diff", "--cached", "--numstat", "--", change.path]),
        git(cwd, ["diff", "--numstat", "--", change.path])
      ]);
      const numstat = [b1.stdout, b2.stdout].join("\n");
      if (numstat.includes("-	-	")) {
        binary = true;
        diff = "Binary files differ";
      }
    } else {
      hunks = [...layerHunks("staged", stagedText), ...layerHunks("unstaged", unstagedText)];
    }
  }
  const counts = binary ? { added: 0, deleted: 0 } : countLines(diff);
  const staged = untracked ? false : change.xy[0] !== " " && change.xy[0] !== "?";
  const unstaged = untracked ? true : change.xy[1] !== " " && change.xy[1] !== "?";
  const status = untracked ? "??" : change.xy.trim();
  let mtime = 0;
  try {
    mtime = statSync(resolve(cwd, change.path)).mtimeMs;
  } catch {
  }
  return {
    path: change.path,
    origPath: change.origPath,
    xy: change.xy,
    status,
    untracked,
    staged,
    unstaged,
    added: counts.added,
    deleted: counts.deleted,
    diff,
    binary,
    hunks,
    mtime
  };
}
async function collectStatus(cwd) {
  const isRepo = await git(cwd, ["rev-parse", "--is-inside-work-tree"]);
  if (isRepo.code !== 0) {
    return { isRepo: false, branch: null, upstream: null, ahead: 0, behind: 0, files: [], error: "not a git repository" };
  }
  const branchResult = await git(cwd, ["branch", "--show-current"]);
  const branch = branchResult.code === 0 && branchResult.stdout.trim() ? branchResult.stdout.trim() : null;
  const upstreamResult = await git(cwd, ["rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"]);
  const upstream = upstreamResult.code === 0 && upstreamResult.stdout.trim() ? upstreamResult.stdout.trim() : null;
  let ahead = 0;
  let behind = 0;
  if (upstream) {
    const [aheadRes, behindRes] = await Promise.all([
      git(cwd, ["rev-list", "--count", "@{u}..HEAD"]),
      git(cwd, ["rev-list", "--count", "HEAD..@{u}"])
    ]);
    ahead = aheadRes.code === 0 ? Number(aheadRes.stdout.trim()) || 0 : 0;
    behind = behindRes.code === 0 ? Number(behindRes.stdout.trim()) || 0 : 0;
  }
  const [statusResult, othersResult] = await Promise.all([
    git(cwd, ["status", "--porcelain=v1", "-z"]),
    git(cwd, ["ls-files", "--others", "--exclude-standard", "-z"])
  ]);
  const changes = parsePorcelain(statusResult.stdout);
  const untrackedPaths = othersResult.stdout.split("\0").filter(Boolean);
  for (const p of untrackedPaths) changes.push({ path: p, xy: "??" });
  const files = await Promise.all(changes.map((change) => collectDiff(cwd, change)));
  return { isRepo: true, branch, upstream, ahead, behind, files };
}
var BASE_RE = /^[A-Za-z0-9][A-Za-z0-9._/-]*$/;
async function resolveBase(cwd, raw) {
  if (!raw || !raw.trim()) return { error: 'missing "base"' };
  const base = raw.trim();
  if (base.startsWith("-") || !BASE_RE.test(base)) return { error: `invalid base branch: ${base}` };
  const refOk = await git(cwd, ["check-ref-format", `refs/heads/${base}`]);
  if (refOk.code !== 0) return { error: `invalid base branch: ${base}` };
  const verify = await git(cwd, ["rev-parse", "--verify", "--quiet", `${base}^{commit}`]);
  if (verify.code !== 0) return { error: `unknown base branch: ${base}` };
  const mbRes = await git(cwd, ["merge-base", "HEAD", base]);
  if (mbRes.code !== 0) return { error: `cannot find merge base with ${base}` };
  return { mb: mbRes.stdout.trim() };
}
async function collectBaseStatus(cwd, base) {
  const resolved = await resolveBase(cwd, base);
  if ("error" in resolved) {
    return { isRepo: true, branch: null, upstream: null, ahead: 0, behind: 0, files: [], error: resolved.error };
  }
  const [branchResult, nsResult] = await Promise.all([
    git(cwd, ["branch", "--show-current"]),
    git(cwd, ["diff", "--no-renames", "--name-status", "-z", resolved.mb])
  ]);
  const branch = branchResult.code === 0 && branchResult.stdout.trim() ? branchResult.stdout.trim() : null;
  const files = [];
  const records = nsResult.stdout.split("\0").filter(Boolean);
  for (let i = 0; i + 1 < records.length; i += 2) {
    const xy = records[i];
    const path = records[i + 1];
    if (!path || xy.length === 0) continue;
    const status = xy[0] === "A" ? "A" : xy[0] === "D" ? "D" : "M";
    const diffRes = await git(cwd, ["diff", resolved.mb, "--", path]);
    const diff = diffRes.stdout;
    const binary = diff.includes("Binary files");
    const counts = binary ? { added: 0, deleted: 0 } : countLines(diff);
    let mtime = 0;
    try {
      mtime = statSync(resolve(cwd, path)).mtimeMs;
    } catch {
    }
    files.push({
      path,
      xy: `${status} `,
      status,
      untracked: false,
      staged: false,
      unstaged: true,
      added: counts.added,
      deleted: counts.deleted,
      diff: binary ? "Binary files differ" : diff,
      binary,
      hunks: [],
      mtime
    });
  }
  return { isRepo: true, branch, upstream: null, ahead: 0, behind: 0, files };
}
async function collectBranches(cwd) {
  const res = await git(cwd, ["for-each-ref", "--format=%(refname:short)", "refs/heads"]);
  return res.code === 0 ? res.stdout.split("\n").map((s) => s.trim()).filter(Boolean) : [];
}
async function revertPath(cwd, path, untracked) {
  const abs = resolve(cwd, path);
  if (untracked) {
    try {
      if (!abs.startsWith(resolve(cwd) + sep) && abs !== resolve(cwd)) return `refusing to delete outside workspace: ${path}`;
      if (existsSync(abs)) rmSync(abs, { recursive: true, force: true });
      return null;
    } catch (e) {
      return `cannot remove ${path}: ${e instanceof Error ? e.message : String(e)}`;
    }
  }
  const res = await git(cwd, ["restore", "--source=HEAD", "--staged", "--worktree", "--", path]);
  return res.code === 0 ? null : res.stderr.trim() || `git restore failed for ${path}`;
}
async function applyAction(config, raw) {
  const record = isRecord(raw) ? raw : {};
  const cwd = validateWorkspace(record.cwd, config.allowedRoots);
  if ("error" in cwd) return { status: 400, body: { ok: false, error: cwd.error } };
  const action = record.action;
  if (action !== "accept" && action !== "revert" && action !== "unstage") {
    return { status: 400, body: { ok: false, error: 'action must be "accept", "revert" or "unstage"' } };
  }
  let paths = null;
  if (record.path !== void 0) {
    const safe = sanitizeRepoPath(record.path);
    if ("error" in safe) return { status: 400, body: { ok: false, error: safe.error } };
    paths = [safe.path];
  }
  if (action === "accept") {
    const res = await git(cwd.path, paths === null ? ["add", "-A"] : ["add", "--", ...paths]);
    if (res.code !== 0) return { status: 500, body: { ok: false, error: res.stderr.trim() || "git add failed" } };
    return { status: 200, body: { ok: true } };
  }
  if (action === "unstage") {
    const res = await git(cwd.path, paths === null ? ["restore", "--staged", "."] : ["restore", "--staged", "--", ...paths]);
    if (res.code !== 0) return { status: 500, body: { ok: false, error: res.stderr.trim() || "git restore --staged failed" } };
    return { status: 200, body: { ok: true } };
  }
  if (paths === null) {
    const status2 = await collectStatus(cwd.path);
    if (!status2.isRepo) return { status: 400, body: { ok: false, error: "not a git repository" } };
    const errors = [];
    const deleted = [];
    for (const file2 of status2.files) {
      const error2 = await revertPath(cwd.path, file2.path, file2.untracked);
      if (error2) errors.push(error2);
      else if (file2.untracked) deleted.push(file2.path);
    }
    if (errors.length > 0) return { status: 500, body: { ok: false, error: errors.join("; ") } };
    return { status: 200, body: { ok: true, deleted } };
  }
  const status = await collectStatus(cwd.path);
  const file = status.isRepo ? status.files.find((f) => f.path === paths[0]) : void 0;
  const untracked = file?.untracked ?? false;
  const error = await revertPath(cwd.path, paths[0], untracked);
  if (error) return { status: 500, body: { ok: false, error } };
  return { status: 200, body: { ok: true } };
}
var HUNK_RE = /^@@ -\d+(?:,\d+)? \+\d+(?:,\d+)? @@/;
var MAX_HUNK_LEN = 1024 * 1024;
function parseHunk(hunk) {
  const lines = hunk.split("\n");
  const m = /^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@/.exec(lines[0] ?? "");
  if (!m) return null;
  const pre = [];
  const post = [];
  for (const line of lines.slice(1)) {
    if (line.startsWith("\\")) return null;
    if (line.startsWith("+")) post.push(line.slice(1));
    else if (line.startsWith("-")) pre.push(line.slice(1));
    else {
      const text = line.slice(1);
      pre.push(text);
      post.push(text);
    }
  }
  return { oldStart: Number(m[1]), newStart: Number(m[3]), pre, post };
}
function findSequence(lines, seq, start) {
  if (seq.length === 0) return Math.max(0, Math.min(start, lines.length));
  const tryAt = (i) => i >= 0 && i + seq.length <= lines.length ? lines.slice(i, i + seq.length).every((l, k) => l === seq[k]) : false;
  if (tryAt(start)) return start;
  for (let i = 0; i + seq.length <= lines.length; i++) {
    if (tryAt(i)) return i;
  }
  return -1;
}
function applyHunkToText(text, hunk, reverse) {
  const parsed = parseHunk(hunk);
  if (!parsed) return null;
  const lines = text.split("\n");
  if (lines.length > 0 && lines[lines.length - 1] === "") lines.pop();
  const pre = reverse ? parsed.post : parsed.pre;
  const post = reverse ? parsed.pre : parsed.post;
  const start = (reverse ? parsed.newStart : parsed.oldStart) - 1;
  const idx = findSequence(lines, pre, start);
  if (idx === -1) return null;
  const out = [...lines.slice(0, idx), ...post, ...lines.slice(idx + pre.length)];
  const result = out.join("\n");
  return {
    text: text.endsWith("\n") ? `${result}
` : result,
    // Delete the file when the hunk covered its whole content and removed
    // everything (untracked files with no surviving lines).
    deleteFile: idx === 0 && idx + pre.length >= lines.length && post.length === 0
  };
}
async function applyHunkAction(config, raw) {
  const record = isRecord(raw) ? raw : {};
  const cwd = validateWorkspace(record.cwd, config.allowedRoots);
  if ("error" in cwd) return { status: 400, body: { ok: false, error: cwd.error } };
  const action = record.action;
  if (action !== "accept" && action !== "revert" && action !== "unstage") {
    return { status: 400, body: { ok: false, error: 'action must be "accept", "revert" or "unstage"' } };
  }
  const safe = sanitizeRepoPath(record.path);
  if ("error" in safe) return { status: 400, body: { ok: false, error: safe.error } };
  const path = safe.path;
  if (path.includes("\n") || path.includes("	") || path.includes('"')) {
    return { status: 400, body: { ok: false, error: `path is not patch-safe: ${path}` } };
  }
  const hunk = typeof record.hunk === "string" ? record.hunk.trimEnd() : "";
  if (!HUNK_RE.test(hunk)) return { status: 400, body: { ok: false, error: "invalid hunk" } };
  if (hunk.length > MAX_HUNK_LEN) return { status: 400, body: { ok: false, error: "hunk too large" } };
  const status = await collectStatus(cwd.path);
  if (!status.isRepo) return { status: 400, body: { ok: false, error: "not a git repository" } };
  const file = status.files.find((f) => f.path === path);
  if (!file) return { status: 404, body: { ok: false, error: `file not changed: ${path}` } };
  const matched = file.hunks.find((h) => h.text === hunk);
  if (!matched) {
    return { status: 409, body: { ok: false, error: "hunk no longer matches the working tree \u2014 refresh the review" } };
  }
  const header = [
    `diff --git a/${path} b/${path}`,
    file.untracked ? "new file mode 100644" : null,
    file.untracked ? "--- /dev/null" : `--- a/${path}`,
    `+++ b/${path}`
  ].filter((l) => l !== null).join("\n");
  const patch = `${header}
${hunk}
`;
  const dir = mkdtempSync(join(tmpdir(), "dsdr-hunk-"));
  const patchFile = join(dir, "hunk.patch");
  writeFileSync(patchFile, patch);
  try {
    const apply2 = (extra) => git(cwd.path, ["apply", "--whitespace=nowarn", ...extra, patchFile]);
    if (action === "accept") {
      const result = await apply2(["--cached"]);
      if (result.code !== 0) {
        return { status: 409, body: { ok: false, error: result.stderr.trim() || "git apply failed \u2014 refresh the review" } };
      }
      return { status: 200, body: { ok: true } };
    }
    if (action === "unstage") {
      const result = await apply2(["--cached", "--reverse"]);
      if (result.code !== 0) {
        return { status: 409, body: { ok: false, error: result.stderr.trim() || "git apply failed \u2014 refresh the review" } };
      }
      return { status: 200, body: { ok: true } };
    }
    const abs = resolve(cwd.path, path);
    let worktree = null;
    try {
      worktree = readFileSync(abs, "utf8");
    } catch {
      return { status: 409, body: { ok: false, error: `cannot read ${path} \u2014 refresh the review` } };
    }
    const next = applyHunkToText(worktree, hunk, true);
    if (next === null) {
      return { status: 409, body: { ok: false, error: "hunk no longer matches the working tree \u2014 refresh the review" } };
    }
    if (matched.layer === "staged") {
      const result = await apply2(["--cached", "--reverse"]);
      if (result.code !== 0) {
        return { status: 409, body: { ok: false, error: result.stderr.trim() || "git apply failed \u2014 refresh the review" } };
      }
    }
    if (next.deleteFile) {
      if (existsSync(abs)) rmSync(abs, { force: true });
    } else {
      writeFileSync(abs, next.text);
    }
    return { status: 200, body: { ok: true } };
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}
var MAX_COMMIT_MESSAGE = 2e3;
async function commitAction(config, raw) {
  const record = isRecord(raw) ? raw : {};
  const cwd = validateWorkspace(record.cwd, config.allowedRoots);
  if ("error" in cwd) return { status: 400, body: { ok: false, error: cwd.error } };
  const message = typeof record.message === "string" ? record.message.trim() : "";
  if (!message) return { status: 400, body: { ok: false, error: 'missing "message"' } };
  if (message.length > MAX_COMMIT_MESSAGE) return { status: 400, body: { ok: false, error: `message too long (max ${MAX_COMMIT_MESSAGE} chars)` } };
  if (message.startsWith("-")) return { status: 400, body: { ok: false, error: 'message must not start with "-"' } };
  const res = await git(cwd.path, ["commit", "-m", message]);
  if (res.code !== 0) {
    const detail = res.stderr.trim() || res.stdout.trim();
    return { status: 400, body: { ok: false, error: detail || "git commit failed" } };
  }
  const hashRes = await git(cwd.path, ["rev-parse", "--short", "HEAD"]);
  return {
    status: 200,
    body: {
      ok: true,
      hash: hashRes.code === 0 ? hashRes.stdout.trim() : void 0,
      subject: message.split("\n")[0]
    }
  };
}
async function pushAction(config, raw) {
  const record = isRecord(raw) ? raw : {};
  const cwd = validateWorkspace(record.cwd, config.allowedRoots);
  if ("error" in cwd) return { status: 400, body: { ok: false, error: cwd.error } };
  const res = await git(cwd.path, ["push"]);
  if (res.code !== 0) {
    return { status: 500, body: { ok: false, error: res.stderr.trim() || "git push failed" } };
  }
  return { status: 200, body: { ok: true, output: res.stdout.trim() || res.stderr.trim() || "pushed" } };
}
var HISTORY_LIMIT = 30;
async function collectHistory(cwd) {
  const upstreamResult = await git(cwd, ["rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"]);
  const hasUpstream = upstreamResult.code === 0 && upstreamResult.stdout.trim() !== "";
  const ahead = /* @__PURE__ */ new Set();
  if (hasUpstream) {
    const revs = await git(cwd, ["rev-list", "@{u}..HEAD"]);
    if (revs.code === 0) {
      for (const line of revs.stdout.split("\n")) if (line.trim()) ahead.add(line.trim());
    }
  }
  const res = await git(cwd, [
    "log",
    "HEAD",
    `--max-count=${HISTORY_LIMIT}`,
    "--pretty=format:%H%x00%h%x00%an%x00%aI%x00%s%x01"
  ]);
  if (res.code !== 0) {
    return { ok: false, commits: [], error: res.stderr.trim() || "git log failed" };
  }
  const commits = res.stdout.split("").map((record) => record.trim()).filter(Boolean).map((record) => {
    const [hash, short, author, date, ...subjectParts] = record.split("\0");
    return { hash, short, author, date, subject: subjectParts.join("\0"), ahead: hasUpstream ? ahead.has(hash) : true };
  }).filter((c) => c.hash && c.short);
  return { ok: true, commits };
}
var HASH_RE = /^[0-9a-f]{7,40}$/;
async function commitDiffAction(config, query) {
  const cwd = validateWorkspace(query.get("cwd"), config.allowedRoots);
  if ("error" in cwd) return { status: 400, body: { ok: false, error: cwd.error, diff: "", files: [], added: 0, deleted: 0 } };
  const hash = query.get("hash") ?? "";
  if (!HASH_RE.test(hash)) {
    return { status: 400, body: { ok: false, error: 'invalid "hash"', diff: "", files: [], added: 0, deleted: 0 } };
  }
  const [diffRes, numstatRes, nameStatusRes] = await Promise.all([
    git(cwd.path, ["show", hash, "--format=", "--no-color"]),
    git(cwd.path, ["show", hash, "--numstat", "--format="]),
    git(cwd.path, ["show", hash, "--name-status", "--format="])
  ]);
  if (diffRes.code !== 0) {
    return { status: 400, body: { ok: false, error: diffRes.stderr.trim() || "git show failed", diff: "", files: [], added: 0, deleted: 0 } };
  }
  const statusByPath = /* @__PURE__ */ new Map();
  for (const line of nameStatusRes.stdout.split("\n")) {
    const parts = line.split("	");
    if (parts.length < 2) continue;
    const xy = parts[0].trim();
    if (!xy) continue;
    if (xy.startsWith("R") || xy.startsWith("C")) {
      if (parts[2]) statusByPath.set(parts[2], "R");
    } else {
      statusByPath.set(parts[1], xy[0]);
    }
  }
  const files = [];
  let added = 0;
  let deleted = 0;
  for (const line of numstatRes.stdout.split("\n")) {
    const match = /^(\d+|-)\t(\d+|-)\t(.*)$/.exec(line);
    if (!match) continue;
    const a = match[1] === "-" ? 0 : Number(match[1]);
    const d = match[2] === "-" ? 0 : Number(match[2]);
    added += a;
    deleted += d;
    files.push({ path: match[3], status: statusByPath.get(match[3]) ?? "M", added: a, deleted: d });
  }
  return {
    status: 200,
    body: {
      ok: true,
      short: hash.slice(0, 7),
      diff: diffRes.stdout,
      files,
      added,
      deleted
    }
  };
}
var MAX_COMMENTS = 500;
var MAX_COMMENT_TEXT = 2e3;
async function commentsFile(cwd) {
  const res = await git(cwd, ["rev-parse", "--git-dir"]);
  if (res.code !== 0) return null;
  const gitDir = res.stdout.trim();
  const abs = isAbsolute(gitDir) ? gitDir : resolve(cwd, gitDir);
  return join(abs, "diff-review-comments.json");
}
function isCommentShape(v) {
  if (!v || typeof v !== "object") return false;
  const c = v;
  return typeof c.id === "string" && typeof c.path === "string" && typeof c.text === "string" && (typeof c.lineNew === "number" || c.lineNew === null) && (typeof c.lineOld === "number" || c.lineOld === null) && typeof c.createdAt === "string" && (c.scope === void 0 || c.scope === "unstaged" || c.scope === "staged" || c.scope === "branch" || c.scope === "last-turn");
}
async function readComments(cwd) {
  const file = await commentsFile(cwd);
  if (!file || !existsSync(file)) return [];
  try {
    const raw = JSON.parse(readFileSync(file, "utf8"));
    return Array.isArray(raw?.comments) ? raw.comments.filter(isCommentShape) : [];
  } catch {
    return [];
  }
}
async function getCommentsAction(config, query) {
  const cwd = validateWorkspace(query.get("cwd"), config.allowedRoots);
  if ("error" in cwd) return { status: 400, body: { ok: false, comments: [], error: cwd.error } };
  return { status: 200, body: { ok: true, comments: await readComments(cwd.path) } };
}
async function putCommentsAction(config, raw) {
  const record = isRecord(raw) ? raw : {};
  const cwd = validateWorkspace(record.cwd, config.allowedRoots);
  if ("error" in cwd) return { status: 400, body: { ok: false, comments: [], error: cwd.error } };
  const comments = record.comments;
  if (!Array.isArray(comments)) return { status: 400, body: { ok: false, comments: [], error: 'missing "comments" array' } };
  if (comments.length > MAX_COMMENTS) return { status: 400, body: { ok: false, comments: [], error: `too many comments (max ${MAX_COMMENTS})` } };
  for (const c of comments) {
    if (!isRecord(c)) return { status: 400, body: { ok: false, comments: [], error: "invalid comment" } };
    if (typeof c.id !== "string" || !c.id || c.id.length > 64) return { status: 400, body: { ok: false, comments: [], error: "invalid comment id" } };
    if (typeof c.text !== "string" || !c.text.trim() || c.text.length > MAX_COMMENT_TEXT) {
      return { status: 400, body: { ok: false, comments: [], error: `invalid comment text (max ${MAX_COMMENT_TEXT} chars)` } };
    }
    const safe = sanitizeRepoPath(c.path);
    if ("error" in safe) return { status: 400, body: { ok: false, comments: [], error: safe.error } };
    const lineNew = c.lineNew;
    const lineOld = c.lineOld;
    const valid = (v) => v === null || typeof v === "number" && Number.isInteger(v) && v >= 1;
    if (!valid(lineNew) || !valid(lineOld)) return { status: 400, body: { ok: false, comments: [], error: "invalid comment line anchor" } };
    if (lineNew === null && lineOld === null) return { status: 400, body: { ok: false, comments: [], error: "comment must anchor to a line" } };
    if (typeof c.createdAt !== "string" || c.createdAt.length > 64) return { status: 400, body: { ok: false, comments: [], error: "invalid comment createdAt" } };
    if (c.scope !== void 0 && c.scope !== "unstaged" && c.scope !== "staged" && c.scope !== "branch" && c.scope !== "last-turn") {
      return { status: 400, body: { ok: false, comments: [], error: "invalid comment scope" } };
    }
  }
  const file = await commentsFile(cwd.path);
  if (!file) return { status: 400, body: { ok: false, comments: [], error: "not a git repository" } };
  try {
    mkdirSync(dirname(file), { recursive: true });
    writeFileSync(`${file}.tmp`, JSON.stringify({ version: 1, comments }, null, 2));
    renameSync(`${file}.tmp`, file);
  } catch (e) {
    return { status: 500, body: { ok: false, comments: [], error: `cannot write comments: ${e instanceof Error ? e.message : String(e)}` } };
  }
  return { status: 200, body: { ok: true, comments } };
}
var REVIEW_SYSTEM_PROMPT = `You are a senior code reviewer. Review ONLY the changes shown in the user message.

Rules:
- Flag issues that meaningfully impact accuracy, performance, security, or maintainability and were introduced by THESE changes.
- Do NOT flag: trivial style, personal preference, pre-existing bugs not touched by the change, speculative problems, intentional design choices.
- Each finding must be discrete, actionable, and anchored to a specific file and line range in the NEW version of the file.
- Severity: P0 = drop everything (blocking release, ops, or security); P1 = urgent, address next cycle; P2 = fix eventually; P3 = nice to have.
- Confidence 0.0-1.0 reflects how sure you are the issue is real.
- Provide a concrete suggestion (replacement code, at most 15 lines) when you have one.

Respond with STRICT JSON ONLY. No markdown fences, no commentary. Exact shape:
{"verdict":"correct"|"incorrect","findings":[{"priority":"P0"|"P1"|"P2"|"P3","title":"imperative title, max 80 chars","detail":"why it is a problem, with file/line references, one paragraph","file":"repo-relative path","lineStart":N,"lineEnd":N,"confidence":0.0,"suggestion":"optional replacement code"}]}
"verdict" is "incorrect" when at least one P0 or P1 finding exists, otherwise "correct".
When there are no issues return {"verdict":"correct","findings":[]}.`;
var MAX_REVIEW_TOTAL_CHARS = 12e4;
var MAX_REVIEW_FILE_CHARS = 6e4;
async function collectCommitDiffFiles(cwd, hash) {
  const numstat = await git(cwd, ["show", hash, "--numstat", "--format="]);
  if (numstat.code !== 0) return { error: numstat.stderr.trim() || "git show failed" };
  const files = [];
  for (const line of numstat.stdout.split("\n")) {
    const m = /^(\d+|-)\t(\d+|-)\t(.*)$/.exec(line);
    if (!m) continue;
    files.push({ path: m[3], diff: "", added: m[1] === "-" ? 0 : Number(m[1]), deleted: m[2] === "-" ? 0 : Number(m[2]) });
  }
  for (const file of files) {
    const res = await git(cwd, ["show", hash, "--format=", "--no-color", "--", file.path]);
    file.diff = res.stdout;
  }
  return files;
}
function buildReviewPrompt(scope, base, commitHash, files, instructions) {
  const scopeLabel = scope === "commit" ? `commit ${commitHash ?? ""}` : scope === "branch" ? `branch vs ${base ?? ""}` : "uncommitted changes";
  const lines = [`Review scope: ${scopeLabel}`];
  if (instructions) lines.push(`Additional instructions: ${instructions}`);
  let totalAdded = 0;
  let totalDeleted = 0;
  for (const f of files) {
    totalAdded += f.added;
    totalDeleted += f.deleted;
  }
  lines.push(`Files (${files.length}): ${totalAdded}+ ${totalDeleted}-`, "");
  let used = 0;
  let truncated = false;
  for (const f of files) {
    let diff = f.diff;
    if (diff.length > MAX_REVIEW_FILE_CHARS) {
      diff = `${diff.slice(0, MAX_REVIEW_FILE_CHARS)}
\u2026 (diff truncated)`;
      truncated = true;
    }
    const block = `### ${f.path} (${f.added}+ ${f.deleted}-)
${diff}
`;
    if (used + block.length > MAX_REVIEW_TOTAL_CHARS) {
      lines.push("\u2026 (further files omitted)");
      truncated = true;
      break;
    }
    lines.push(block);
    used += block.length;
  }
  return { text: lines.join("\n"), truncated };
}
function resolveReviewModel(ctx, config, sessionId) {
  if (config.reviewProvider && config.reviewModel) {
    return { provider: config.reviewProvider, model: config.reviewModel };
  }
  if (sessionId) {
    const store = ctx.sessions;
    const session = store?.get(sessionId);
    const cfg = session?.requestHeader()?.config;
    if (cfg?.provider && cfg?.model) return { provider: cfg.provider, model: cfg.model };
  }
  return null;
}
var REVIEW_PRIORITIES = ["P0", "P1", "P2", "P3"];
function parseReviewResponse(raw, fileSet) {
  const cleaned = raw.trim().replace(/^```(?:json)?\s*/i, "").replace(/```\s*$/, "").trim();
  let obj = null;
  try {
    obj = JSON.parse(cleaned);
  } catch {
    return { parsed: false, verdict: "correct", findings: [] };
  }
  let verdict = "correct";
  const findings = [];
  if (obj && typeof obj === "object") {
    const rec = obj;
    if (rec.verdict === "incorrect") verdict = "incorrect";
    if (Array.isArray(rec.findings)) {
      for (const f of rec.findings) {
        if (!isRecord(f)) continue;
        const priority = f.priority;
        if (typeof priority !== "string" || !REVIEW_PRIORITIES.includes(priority)) continue;
        const title = typeof f.title === "string" ? f.title.trim().slice(0, 200) : "";
        if (!title) continue;
        const detail = typeof f.detail === "string" ? f.detail.trim().slice(0, 4e3) : "";
        const file = typeof f.file === "string" ? f.file : "";
        if (!fileSet.has(file)) continue;
        const ls = typeof f.lineStart === "number" && Number.isInteger(f.lineStart) && f.lineStart >= 1 ? f.lineStart : 1;
        let le = typeof f.lineEnd === "number" && Number.isInteger(f.lineEnd) && f.lineEnd >= 1 ? f.lineEnd : ls;
        if (le < ls) le = ls;
        const confidence = typeof f.confidence === "number" && Number.isFinite(f.confidence) ? Math.max(0, Math.min(1, f.confidence)) : 0.5;
        const suggestion = typeof f.suggestion === "string" && f.suggestion.trim() ? f.suggestion.trim().slice(0, 2e3) : void 0;
        findings.push({ priority, title, detail, file, lineStart: ls, lineEnd: le, confidence, suggestion });
      }
    }
  }
  if (findings.some((f) => f.priority === "P0" || f.priority === "P1")) verdict = "incorrect";
  return { parsed: true, verdict, findings };
}
async function reviewAction(ctx, config, raw) {
  const record = isRecord(raw) ? raw : {};
  const cwd = validateWorkspace(record.cwd, config.allowedRoots);
  if ("error" in cwd) return { status: 400, body: { ok: false, findings: [], error: cwd.error } };
  const scope = record.scope === "branch" || record.scope === "commit" ? record.scope : "uncommitted";
  let fileDiffs;
  if (scope === "commit") {
    const hash = typeof record.commitHash === "string" ? record.commitHash : "";
    if (!HASH_RE.test(hash)) return { status: 400, body: { ok: false, findings: [], error: 'invalid "commitHash"' } };
    const res = await collectCommitDiffFiles(cwd.path, hash);
    if ("error" in res) return { status: 400, body: { ok: false, findings: [], error: res.error } };
    fileDiffs = res;
  } else if (scope === "branch") {
    const base = typeof record.base === "string" ? record.base : "";
    const resolved = await resolveBase(cwd.path, base);
    if ("error" in resolved) return { status: 400, body: { ok: false, findings: [], error: resolved.error } };
    const status = await collectBaseStatus(cwd.path, base);
    fileDiffs = status.files.map((f) => ({ path: f.path, diff: f.diff, added: f.added, deleted: f.deleted }));
  } else {
    const status = await collectStatus(cwd.path);
    if (!status.isRepo) return { status: 400, body: { ok: false, findings: [], error: status.error ?? "not a git repository" } };
    fileDiffs = status.files.map((f) => ({ path: f.path, diff: f.diff, added: f.added, deleted: f.deleted }));
  }
  fileDiffs = fileDiffs.filter((f) => f.diff.trim().length > 0 && !f.diff.includes("Binary files"));
  if (fileDiffs.length === 0) {
    return { status: 200, body: { ok: true, verdict: "correct", findings: [], model: void 0, truncated: false } };
  }
  const model = resolveReviewModel(ctx, config, typeof record.sessionId === "string" ? record.sessionId : void 0);
  if (!model) {
    return {
      status: 400,
      body: { ok: false, findings: [], error: "no model available \u2014 set reviewModel in the plugin config, or run a review from a session that has already made a request" }
    };
  }
  const llm = ctx.llm;
  if (!llm) return { status: 503, body: { ok: false, findings: [], error: "llm service unavailable" } };
  const instructions = typeof record.instructions === "string" && record.instructions.trim() ? record.instructions.trim().slice(0, 2e3) : "";
  const { text, truncated } = buildReviewPrompt(scope, typeof record.base === "string" ? record.base : void 0, typeof record.commitHash === "string" ? record.commitHash : void 0, fileDiffs, instructions);
  try {
    let out = "";
    const stream = llm.stream({
      provider: model.provider,
      model: model.model,
      messages: [createUserMessage({ content: [{ type: "text", text }], source: { kind: "plugin", plugin: "diff-review" } })],
      system: REVIEW_SYSTEM_PROMPT,
      temperature: 0,
      maxTokens: 8192
    });
    for await (const chunk of stream) {
      if (chunk.type === "text-delta" && typeof chunk.text === "string") out += chunk.text;
    }
    const parsed = parseReviewResponse(out, new Set(fileDiffs.map((f) => f.path)));
    if (!parsed.parsed && out.trim()) {
      parsed.findings.push({
        priority: "P2",
        title: "\u8BC4\u5BA1\u8F93\u51FA\u89E3\u6790\u5931\u8D25",
        detail: `\u6A21\u578B\u8F93\u51FA\u65E0\u6CD5\u89E3\u6790\u4E3A\u7ED3\u6784\u5316\u53D1\u73B0\uFF08\u524D 500 \u5B57\uFF09\uFF1A${out.trim().slice(0, 500)}`,
        file: fileDiffs[0].path,
        lineStart: 1,
        lineEnd: 1,
        confidence: 0.5
      });
    }
    return { status: 200, body: { ok: true, verdict: parsed.verdict, findings: parsed.findings, model, truncated } };
  } catch (e) {
    const message = e instanceof Error ? e.message : String(e);
    return { status: 502, body: { ok: false, findings: [], error: `review failed: ${message}` } };
  }
}
async function prAction(config, query) {
  const cwd = validateWorkspace(query.get("cwd"), config.allowedRoots);
  if ("error" in cwd) return { status: 400, body: { ok: false, comments: [], error: cwd.error } };
  const ghCheck = await runCmd("gh", ["--version"]);
  if (ghCheck.code !== 0) return { status: 200, body: { ok: true, comments: [], error: "gh is not installed" } };
  const branchRes = await git(cwd.path, ["branch", "--show-current"]);
  const branch = branchRes.code === 0 && branchRes.stdout.trim() ? branchRes.stdout.trim() : null;
  if (!branch) return { status: 200, body: { ok: true, comments: [], error: "detached HEAD \u2014 no PR context" } };
  const prView = await runCmd("gh", ["pr", "view", branch, "--json", "number,title,url,author,state,body"]);
  if (prView.code !== 0) return { status: 200, body: { ok: true, comments: [], error: `no pull request for branch ${branch}` } };
  let prMeta = null;
  try {
    prMeta = JSON.parse(prView.stdout);
  } catch {
    return { status: 200, body: { ok: true, comments: [], error: "gh pr view returned unparseable output" } };
  }
  const number = prMeta.number;
  const ownerRepo = await runCmd("gh", ["repo", "view", "--json", "nameWithOwner", "-q", ".nameWithOwner"]);
  const repo = ownerRepo.code === 0 ? ownerRepo.stdout.trim() : "";
  const comments = [];
  if (typeof number === "number" && repo) {
    const cRes = await runCmd("gh", ["api", `repos/${repo}/pulls/${number}/comments`, "--paginate"]);
    if (cRes.code === 0) {
      try {
        const rows = JSON.parse(cRes.stdout);
        for (const row of Array.isArray(rows) ? rows : []) {
          const body = typeof row.body === "string" && row.body.trim() ? row.body.trim() : "";
          if (!body) continue;
          comments.push({
            id: String(row.id ?? ""),
            author: typeof row.user?.login === "string" ? row.user.login : "unknown",
            body,
            path: typeof row.path === "string" ? row.path : null,
            line: typeof row.line === "number" ? row.line : typeof row.original_line === "number" ? row.original_line : null,
            createdAt: typeof row.created_at === "string" ? row.created_at : ""
          });
        }
      } catch {
      }
    }
  }
  return {
    status: 200,
    body: {
      ok: true,
      pr: {
        number: typeof number === "number" ? number : 0,
        title: typeof prMeta.title === "string" ? prMeta.title : "",
        url: typeof prMeta.url === "string" ? prMeta.url : "",
        author: typeof prMeta.author?.login === "string" ? prMeta.author.login : "",
        state: typeof prMeta.state === "string" ? prMeta.state : "",
        body: typeof prMeta.body === "string" ? prMeta.body : void 0
      },
      comments
    }
  };
}
async function reposAction(config, query) {
  const cwd = validateWorkspace(query.get("cwd"), config.allowedRoots);
  if ("error" in cwd) return { status: 400, body: { ok: false, repos: [], error: cwd.error } };
  const seen = /* @__PURE__ */ new Set();
  const repos = [];
  const pushRepo = async (dir) => {
    const inside = await git(dir, ["rev-parse", "--is-inside-work-tree"]);
    if (inside.code !== 0 || inside.stdout.trim() !== "true") return;
    const top = await git(dir, ["rev-parse", "--show-toplevel"]);
    const topPath = top.code === 0 && top.stdout.trim() ? top.stdout.trim() : dir;
    if (seen.has(topPath)) return;
    seen.add(topPath);
    const branchRes = await git(topPath, ["branch", "--show-current"]);
    repos.push({ path: topPath, branch: branchRes.code === 0 && branchRes.stdout.trim() ? branchRes.stdout.trim() : null });
  };
  await pushRepo(cwd.path);
  let entries = [];
  try {
    entries = readdirSync(cwd.path, { withFileTypes: true });
  } catch {
  }
  for (const entry of entries) {
    if (!entry.isDirectory() || entry.name.startsWith(".")) continue;
    await pushRepo(join(cwd.path, entry.name));
  }
  return { status: 200, body: { ok: true, repos } };
}
function validateWorkspace(raw, allowedRoots) {
  if (typeof raw !== "string" || !raw.trim()) return { error: 'missing "cwd"' };
  const p = raw.trim();
  if (!isAbsolute(p)) return { error: `cwd must be absolute: ${p}` };
  if (!existsSync(p)) return { error: `cwd does not exist: ${p}` };
  try {
    if (!statSync(p).isDirectory()) return { error: `cwd is not a directory: ${p}` };
  } catch (e) {
    return { error: `cannot stat cwd: ${e instanceof Error ? e.message : String(e)}` };
  }
  if (allowedRoots.length > 0) {
    const ok = allowedRoots.some((root) => {
      const r = root.replace(/[\\/]+$/, "");
      return p === r || p.startsWith(r + sep);
    });
    if (!ok) return { error: `cwd is outside allowedRoots: ${p}` };
  }
  return { path: p };
}
function jsonResponse(res, status, body) {
  const data = JSON.stringify(body);
  res.writeHead(status, {
    "content-type": "application/json; charset=utf-8",
    "cache-control": "no-store",
    "content-length": Buffer.byteLength(data)
  });
  res.end(data);
}
async function readJsonBody(req) {
  let body = "";
  for await (const chunk of req) body += chunk;
  if (!body) return {};
  try {
    return JSON.parse(body);
  } catch {
    return null;
  }
}
function readQuery(req) {
  return new URLSearchParams(req.url?.split("?")[1] ?? "");
}
var MAX_FILES_LIST = 2e3;
var MAX_FILE_BYTES = 1024 * 1024;
var MAX_IMAGE_BYTES = 5 * 1024 * 1024;
var SKIPPED_FILE_DIRS = /* @__PURE__ */ new Set([".git", "node_modules", ".DS_Store"]);
var IMAGE_MIME = { ".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp", ".svg": "image/svg+xml" };
function extension(path) {
  const dot = path.lastIndexOf(".");
  return dot >= 0 ? path.slice(dot).toLowerCase() : "";
}
function resolveWorkspaceFile(cwd, raw) {
  const safe = sanitizeRepoPath(raw);
  if ("error" in safe) return safe;
  const abs = resolve(cwd, safe.path);
  if (!abs.startsWith(cwd.endsWith(sep) ? cwd : cwd + sep)) return { error: "path escapes workspace" };
  return { path: safe.path.replace(/\\/g, "/"), abs };
}
function listWorkspaceFiles(cwd) {
  const files = [];
  let truncated = false;
  const walk = (dir, prefix, depth) => {
    if (depth > 12 || files.length >= MAX_FILES_LIST) {
      truncated = true;
      return;
    }
    let entries;
    try {
      entries = readdirSync(dir, { withFileTypes: true });
    } catch {
      return;
    }
    for (const entry of entries) {
      if (files.length >= MAX_FILES_LIST) {
        truncated = true;
        return;
      }
      if (entry.isSymbolicLink()) continue;
      if (entry.isDirectory()) {
        if (!SKIPPED_FILE_DIRS.has(entry.name)) walk(join(dir, entry.name), `${prefix}${entry.name}/`, depth + 1);
        continue;
      }
      if (!entry.isFile()) continue;
      const abs = join(dir, entry.name);
      try {
        const stat = statSync(abs);
        files.push({ path: `${prefix}${entry.name}`, size: stat.size, mtime: stat.mtimeMs });
      } catch {
      }
    }
  };
  walk(cwd, "", 0);
  files.sort((a, b) => a.path.localeCompare(b.path));
  return { ok: true, files, ...truncated ? { truncated: true } : {} };
}
function filesAction(config, method, query, record) {
  const rawCwd = method === "POST" && isRecord(record) ? record.cwd : query.get("cwd");
  const cwd = validateWorkspace(rawCwd, config.allowedRoots);
  if ("error" in cwd) return { status: 400, body: { ok: false, error: cwd.error } };
  if (method === "GET") {
    const path = query.get("path");
    if (!path) return { status: 200, body: listWorkspaceFiles(cwd.path) };
    const target = resolveWorkspaceFile(cwd.path, path);
    if ("error" in target) return { status: 400, body: { ok: false, error: target.error } };
    try {
      const stat = statSync(target.abs);
      if (!stat.isFile()) return { status: 400, body: { ok: false, error: "not a file" } };
      const mime = IMAGE_MIME[extension(target.path)];
      if (mime) {
        if (stat.size > MAX_IMAGE_BYTES) return { status: 400, body: { ok: false, error: "image exceeds 5MB preview limit" } };
        const data2 = readFileSync(target.abs);
        return { status: 200, body: { ok: true, path: target.path, kind: "image", mime, dataUrl: "data:" + mime + ";base64," + data2.toString("base64"), mtime: stat.mtimeMs } };
      }
      if (stat.size > MAX_FILE_BYTES) return { status: 400, body: { ok: false, error: "file exceeds 1MB text preview limit" } };
      const data = readFileSync(target.abs);
      if (data.includes(0)) return { status: 200, body: { ok: true, path: target.path, kind: "binary", mtime: stat.mtimeMs } };
      return { status: 200, body: { ok: true, path: target.path, kind: "text", content: data.toString("utf8"), mtime: stat.mtimeMs } };
    } catch (e) {
      return { status: 404, body: { ok: false, error: e instanceof Error ? e.message : "file not found" } };
    }
  }
  if (method === "POST" && isRecord(record)) {
    const target = resolveWorkspaceFile(cwd.path, record.path);
    if ("error" in target) return { status: 400, body: { ok: false, error: target.error } };
    if (typeof record.content !== "string" || Buffer.byteLength(record.content, "utf8") > MAX_FILE_BYTES) return { status: 400, body: { ok: false, error: "content must be text no larger than 1MB" } };
    try {
      const stat = statSync(target.abs);
      if (!stat.isFile() || stat.size > MAX_FILE_BYTES) return { status: 400, body: { ok: false, error: "file is not editable text or exceeds 1MB" } };
      if (typeof record.mtime === "number" && Math.abs(stat.mtimeMs - record.mtime) > 1) return { status: 409, body: { ok: false, error: "file changed on disk; reload before saving" } };
      writeFileSync(target.abs, record.content, "utf8");
      return { status: 200, body: { ok: true, mtime: statSync(target.abs).mtimeMs } };
    } catch (e) {
      return { status: 404, body: { ok: false, error: e instanceof Error ? e.message : "file not found" } };
    }
  }
  return { status: 405, body: { ok: false, error: "method not allowed" } };
}
function apply(ctx, config) {
  ctx.inject(["webServer"], (httpCtx) => {
    httpCtx.effect(
      () => httpCtx.webServer.register({
        kind: "exact",
        path: config.statusPath,
        handler: async (req, res) => {
          if (req.method === "GET" || req.method === "HEAD") {
            const query = readQuery(req);
            const cwd = validateWorkspace(query.get("cwd"), config.allowedRoots);
            if ("error" in cwd) {
              jsonResponse(res, 400, { isRepo: false, branch: null, files: [], error: cwd.error });
              return;
            }
            const base = query.get("base");
            jsonResponse(res, 200, base ? await collectBaseStatus(cwd.path, base) : await collectStatus(cwd.path));
            return;
          }
          jsonResponse(res, 405, { ok: false, error: "method not allowed" });
        }
      }),
      "diff-review: status route"
    );
    httpCtx.effect(
      () => httpCtx.webServer.register({
        kind: "exact",
        path: config.applyPath,
        handler: async (req, res) => {
          if (req.method === "POST") {
            const raw = await readJsonBody(req);
            if (raw === null) {
              jsonResponse(res, 400, { ok: false, error: "invalid JSON body" });
              return;
            }
            const result = await applyAction(config, raw);
            jsonResponse(res, result.status, result.body);
            return;
          }
          jsonResponse(res, 405, { ok: false, error: "method not allowed" });
        }
      }),
      "diff-review: apply route"
    );
    httpCtx.effect(
      () => httpCtx.webServer.register({
        kind: "exact",
        path: config.applyHunkPath,
        handler: async (req, res) => {
          if (req.method === "POST") {
            const raw = await readJsonBody(req);
            if (raw === null) {
              jsonResponse(res, 400, { ok: false, error: "invalid JSON body" });
              return;
            }
            const result = await applyHunkAction(config, raw);
            jsonResponse(res, result.status, result.body);
            return;
          }
          jsonResponse(res, 405, { ok: false, error: "method not allowed" });
        }
      }),
      "diff-review: apply-hunk route"
    );
    httpCtx.effect(
      () => httpCtx.webServer.register({
        kind: "exact",
        path: config.commitPath,
        handler: async (req, res) => {
          if (req.method === "POST") {
            const raw = await readJsonBody(req);
            if (raw === null) {
              jsonResponse(res, 400, { ok: false, error: "invalid JSON body" });
              return;
            }
            const result = await commitAction(config, raw);
            jsonResponse(res, result.status, result.body);
            return;
          }
          jsonResponse(res, 405, { ok: false, error: "method not allowed" });
        }
      }),
      "diff-review: commit route"
    );
    httpCtx.effect(
      () => httpCtx.webServer.register({
        kind: "exact",
        path: config.pushPath,
        handler: async (req, res) => {
          if (req.method === "POST") {
            const raw = await readJsonBody(req);
            if (raw === null) {
              jsonResponse(res, 400, { ok: false, error: "invalid JSON body" });
              return;
            }
            const result = await pushAction(config, raw);
            jsonResponse(res, result.status, result.body);
            return;
          }
          jsonResponse(res, 405, { ok: false, error: "method not allowed" });
        }
      }),
      "diff-review: push route"
    );
    httpCtx.effect(
      () => httpCtx.webServer.register({
        kind: "exact",
        path: config.historyPath,
        handler: async (req, res) => {
          if (req.method === "GET" || req.method === "HEAD") {
            const cwd = validateWorkspace(readQuery(req).get("cwd"), config.allowedRoots);
            if ("error" in cwd) {
              jsonResponse(res, 400, { ok: false, commits: [], error: cwd.error });
              return;
            }
            jsonResponse(res, 200, await collectHistory(cwd.path));
            return;
          }
          jsonResponse(res, 405, { ok: false, error: "method not allowed" });
        }
      }),
      "diff-review: history route"
    );
    httpCtx.effect(
      () => httpCtx.webServer.register({
        kind: "exact",
        path: config.commitDiffPath,
        handler: async (req, res) => {
          if (req.method === "GET" || req.method === "HEAD") {
            const result = await commitDiffAction(config, readQuery(req));
            jsonResponse(res, result.status, result.body);
            return;
          }
          jsonResponse(res, 405, { ok: false, error: "method not allowed" });
        }
      }),
      "diff-review: commit-diff route"
    );
    httpCtx.effect(
      () => httpCtx.webServer.register({
        kind: "exact",
        path: config.commentsPath,
        handler: async (req, res) => {
          if (req.method === "GET" || req.method === "HEAD") {
            const result = await getCommentsAction(config, readQuery(req));
            jsonResponse(res, result.status, result.body);
            return;
          }
          if (req.method === "POST") {
            const raw = await readJsonBody(req);
            if (raw === null) {
              jsonResponse(res, 400, { ok: false, comments: [], error: "invalid JSON body" });
              return;
            }
            const result = await putCommentsAction(config, raw);
            jsonResponse(res, result.status, result.body);
            return;
          }
          jsonResponse(res, 405, { ok: false, error: "method not allowed" });
        }
      }),
      "diff-review: comments route"
    );
    httpCtx.effect(
      () => httpCtx.webServer.register({
        kind: "exact",
        path: config.branchesPath,
        handler: async (req, res) => {
          if (req.method === "GET" || req.method === "HEAD") {
            const query = readQuery(req);
            const cwd = validateWorkspace(query.get("cwd"), config.allowedRoots);
            if ("error" in cwd) {
              jsonResponse(res, 400, { ok: false, branches: [], error: cwd.error });
              return;
            }
            const branches = await collectBranches(cwd.path);
            jsonResponse(res, 200, { ok: true, branches });
            return;
          }
          jsonResponse(res, 405, { ok: false, error: "method not allowed" });
        }
      }),
      "diff-review: branches route"
    );
    httpCtx.effect(
      () => httpCtx.webServer.register({
        kind: "exact",
        path: config.reviewPath,
        handler: async (req, res) => {
          if (req.method === "POST") {
            const raw = await readJsonBody(req);
            if (raw === null) {
              jsonResponse(res, 400, { ok: false, findings: [], error: "invalid JSON body" });
              return;
            }
            const result = await reviewAction(ctx, config, raw);
            jsonResponse(res, result.status, result.body);
            return;
          }
          jsonResponse(res, 405, { ok: false, error: "method not allowed" });
        }
      }),
      "diff-review: review route"
    );
    httpCtx.effect(
      () => httpCtx.webServer.register({
        kind: "exact",
        path: config.prPath,
        handler: async (req, res) => {
          if (req.method === "GET" || req.method === "HEAD") {
            const result = await prAction(config, readQuery(req));
            jsonResponse(res, result.status, result.body);
            return;
          }
          jsonResponse(res, 405, { ok: false, error: "method not allowed" });
        }
      }),
      "diff-review: pr route"
    );
    httpCtx.effect(
      () => httpCtx.webServer.register({
        kind: "exact",
        path: config.reposPath,
        handler: async (req, res) => {
          if (req.method === "GET" || req.method === "HEAD") {
            const result = await reposAction(config, readQuery(req));
            jsonResponse(res, result.status, result.body);
            return;
          }
          jsonResponse(res, 405, { ok: false, error: "method not allowed" });
        }
      }),
      "diff-review: repos route"
    );
    httpCtx.effect(
      () => httpCtx.webServer.register({
        kind: "exact",
        path: config.filesPath,
        handler: async (req, res) => {
          const raw = req.method === "POST" ? await readJsonBody(req) : void 0;
          if (raw === null) {
            jsonResponse(res, 400, { ok: false, error: "invalid JSON body" });
            return;
          }
          const result = filesAction(config, req.method, readQuery(req), raw);
          jsonResponse(res, result.status, result.body);
        }
      }),
      "diff-review: files route"
    );
  });
}
export {
  Config,
  apply,
  name
};
//# sourceMappingURL=index.js.map
