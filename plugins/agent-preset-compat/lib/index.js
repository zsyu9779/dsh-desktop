/**
 * dsh-agent-preset-compat — keep session records written by an older DSH
 * resolvable after a preset rename.
 *
 * DSH 0.1.2 renamed the shipped agent preset `code` to `ptc`, but the id every
 * session header stores (`agentPreset`) still says `code`, so resuming an old
 * session — or creating one while the saved default is still `code` — fails
 * with `agent-presets: preset "code" not found`. This plugin restores the old
 * id as a user-root alias of the replacement preset. It never edits session
 * history, and a future release that ships the old id again shadows the alias
 * automatically, because the shipped root is scanned before the user root.
 *
 * The alias is a copy, so it is stamped with the composition digest it was made
 * from: a later DSH that changes the replacement preset refreshes our copy on
 * the next start, while a directory this plugin did not write is never touched.
 */

import { createHash } from "node:crypto";
import { existsSync } from "node:fs";
import { cp, mkdir, readFile, rename, rm, writeFile } from "node:fs/promises";
import { homedir } from "node:os";
import { dirname, join } from "node:path";
import z from "@deepseek-ai/schemastery";

/** Stable Cordis plugin name. */
export const name = "agent-preset-compat";

/** The roster owns both the discovery roots and the id list. */
export const inject = ["agentPresets"];

/** The composition file whose content decides whether a copy is still current. */
const COMPOSITION_FILE = "agent.cordis.yml";

/**
 * Stamps a directory as written by this plugin, and from which composition.
 * Discovery ignores files it does not know, so the stamp is invisible to DSH.
 */
const MARKER_FILE = ".dsh-agent-preset-compat.json";

/** The id shape the roster itself accepts, reused to reject a malformed config. */
const PRESET_ID = /^[a-z0-9][a-z0-9-]*$/;

export const Config = z.object({
  /** Retired preset id -> the id that replaced it. */
  aliases: z.dict(z.string()).default({ code: "ptc" }),
});

/**
 * Materialize or refresh every configured alias before the tree settles, so a
 * session resumed during startup already finds the retired id.
 * @param ctx - host plugin context carrying the roster service.
 * @param config - validated {@link Config}.
 */
export async function apply(ctx, config) {
  const aliases = config?.aliases ?? { code: "ptc" };
  const warn = (message) => ctx.logger?.warn?.(`agent-preset-compat: ${message}`);
  const info = (message) => ctx.logger?.info?.(`agent-preset-compat: ${message}`);
  try {
    await reconcile(ctx.agentPresets, aliases, { warn, info });
  } catch (error) {
    // Never fail the boot: an alias is a repair, not a prerequisite.
    warn(messageOf(error));
  }
}

/**
 * Bring every configured alias in line with the current roster.
 * @param roster - the agent-presets service.
 * @param aliases - retired id -> replacement id.
 * @param log - scoped logger helpers.
 */
async function reconcile(roster, aliases, log) {
  const userRoot = roster.roots.find((root) => root.trust === "user");
  if (userRoot === undefined) {
    log.warn("no user preset root is composed; nothing to alias");
    return;
  }
  const rootPath = expandHome(userRoot.path);
  const rows = new Map((await roster.list()).map((preset) => [preset.id, preset]));

  for (const [retiredId, replacementId] of Object.entries(aliases)) {
    if (!PRESET_ID.test(retiredId) || !PRESET_ID.test(replacementId)) {
      log.warn(`ignoring malformed alias ${retiredId} -> ${replacementId}`);
      continue;
    }
    const target = join(rootPath, retiredId);
    const stamp = await readStamp(target);
    const existing = rows.get(retiredId);

    if (existing !== undefined && stamp === undefined) {
      // A preset the user authored (or a shipped one that came back) owns the
      // id; a broken one is reported rather than replaced.
      if (existing.broken !== undefined) {
        log.warn(`preset ${retiredId} exists but cannot compose (${existing.broken}); leaving it alone`);
      }
      continue;
    }

    const replacement = rows.get(replacementId);
    if (replacement === undefined) {
      log.warn(`cannot alias ${retiredId}: preset ${replacementId} is not installed`);
      continue;
    }
    const source = dirname(replacement.path);
    const digest = await compositionDigest(join(source, COMPOSITION_FILE));

    if (stamp !== undefined && stamp.digest === digest) continue;
    if (stamp === undefined && existsSync(target)) {
      log.warn(`${retiredId} exists but holds no preset this plugin wrote; leaving it alone`);
      continue;
    }

    await materialize(source, target, { source: replacementId, digest });
    log.info(`${stamp === undefined ? "aliased" : "refreshed"} preset ${retiredId} -> ${replacementId}`);
  }
}

/**
 * Copy a preset directory into place atomically, stamped with its source.
 * A crash mid-copy leaves only the staging directory, never a half preset that
 * would occupy the id and report itself as broken forever.
 * @param source - preset directory to copy.
 * @param target - destination directory, replaced when it exists.
 * @param stamp - marker payload written inside the copy.
 */
async function materialize(source, target, stamp) {
  const staging = `${target}.dsh-agent-preset-compat-staging`;
  await mkdir(dirname(target), { recursive: true });
  await rm(staging, { recursive: true, force: true });
  await cp(source, staging, { recursive: true });
  await writeFile(join(staging, MARKER_FILE), `${JSON.stringify(stamp, undefined, 2)}\n`);
  await rm(target, { recursive: true, force: true });
  await rename(staging, target);
}

/**
 * Read this plugin's stamp from a directory.
 * @param target - candidate alias directory.
 * @returns the recorded payload, or undefined when this plugin did not write it.
 */
async function readStamp(target) {
  try {
    const parsed = JSON.parse(await readFile(join(target, MARKER_FILE), "utf8"));
    return typeof parsed === "object" && parsed !== null ? parsed : {};
  } catch {
    // No stamp file means the directory is not ours; an unreadable one is ours
    // but stale, so an empty stamp forces a rewrite.
    return existsSync(join(target, MARKER_FILE)) ? {} : undefined;
  }
}

/**
 * Digest the file that decides an alias's content.
 * @param path - composition file path.
 * @returns lowercase hex SHA-256, or "" when the file cannot be read.
 */
async function compositionDigest(path) {
  try {
    return createHash("sha256").update(await readFile(path)).digest("hex");
  } catch {
    return "";
  }
}

/**
 * Expand a leading `~` the way discovery does.
 * @param path - configured root path.
 * @returns an absolute path.
 */
function expandHome(path) {
  if (path === "~") return homedir();
  return path.startsWith("~/") ? join(homedir(), path.slice(2)) : path;
}

function messageOf(error) {
  return error instanceof Error ? error.message : String(error);
}
