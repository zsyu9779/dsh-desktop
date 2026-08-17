/**
 * dsh-file-changes — browser half (self-developed).
 *
 * Accumulates the turn's applied file mutations from the mutation tools'
 * presentation views and renders a compact per-turn panel under the closing
 * message: created/modified badge per file, a diff modal, and a workspace-
 * bound reveal action.
 */
window.__ModuleLoader__.load({
  id: "dsh-file-changes",
  factory: (require) => {
    var module = { exports: {} };
    var exports = module.exports;
    Object.defineProperty(exports, Symbol.toStringTag, { value: "Module" });
    const react = require("react");
    const jsx = require("react/jsx-runtime");
    const runtime_client = require("@deepseek-ai/dsh-client-runtime/client");
    const primitives = require("@deepseek-ai/dsh-client-ui-primitives");

    const NS = "fileChanges";
    const zh = {
      "panel.label": "文件变更",
      "panel.created": "新增",
      "panel.modified": "修改",
      "panel.viewDiff": "查看修改",
      "panel.reveal": "定位",
      "panel.close": "关闭",
      "panel.diffTitle": "变更详情 · {name}",
      "panel.noDiff": "该工具没有提供可渲染的差异内容。",
    };
    const en = {
      "panel.label": "File changes",
      "panel.created": "Created",
      "panel.modified": "Modified",
      "panel.viewDiff": "View diff",
      "panel.reveal": "Reveal",
      "panel.close": "Close",
      "panel.diffTitle": "Changes · {name}",
      "panel.noDiff": "This tool provided no renderable diff content.",
    };

    // Pull per-file hunks out of a tool's diff-card or generic-edit view.
    function collect(view, seq) {
      if (view != null && view.card === "diff" && Array.isArray(view.diffs)) {
        return view.diffs.map((hunk) => ({
          seq,
          path: hunk.path,
          status: hunk.oldText == null ? "created" : "modified",
          hunks: [hunk],
        }));
      }
      if (view != null && view.card === "generic" && view.kind === "edit") {
        return (view.locations ?? []).map((loc) => ({
          seq,
          path: loc.path,
          status: "modified",
          hunks: [],
        }));
      }
      return [];
    }

    function mergeChange(list, item) {
      const i = list.findIndex((c) => c.path === item.path);
      if (i === -1) return [...list, item];
      const prev = list[i];
      const next = list.slice();
      next[i] = {
        ...prev,
        status: prev.status === "created" || item.status === "created" ? "created" : "modified",
        hunks: [...prev.hunks, ...item.hunks],
        seq: item.seq,
      };
      return next;
    }

    const definition = {
      kind: "fileChanges",
      match: (event) => {
        if (event.type === "turn/start") return { id: String(event.data.turn), role: "start" };
        if (event.type === "tool/call") return { id: String(event.data.turn), role: "update" };
        if (event.type === "tool/result" && runtime_client.isAppendSurfaceEvent(event)) {
          return { id: String(event.data.turn), role: "update" };
        }
        return null;
      },
      start: (_ctx, match) => {
        if (match.event.type !== "turn/start") throw new Error("fileChanges start requires turn/start");
        return { turn: match.event.data.turn, calls: new Map(), changes: [] };
      },
      update: (ctx, match) => {
        if (match.event.type === "tool/call") {
          const calls = new Map(ctx.state.calls);
          calls.set(String(match.event.data.callId), match.view?.for === "call" ? match.view.view : null);
          return { ...ctx.state, calls };
        }
        if (match.event.type !== "tool/result") return ctx.state;
        if (match.event.data.message.content[0].isError === true) return ctx.state;
        const callId = String(match.event.data.message.source.callId);
        const callView = ctx.state.calls.get(callId) ?? null;
        const resultView = match.view?.for === "result" ? match.view.view : null;
        let changes = ctx.state.changes;
        for (const item of [...collect(resultView, match.event.seq), ...collect(callView, match.event.seq)]) {
          changes = mergeChange(changes, item);
        }
        return { ...ctx.state, changes };
      },
      buildLocationData: (ctx, scope) =>
        scope !== "turn" || ctx.state === undefined
          ? null
          : { kind: "turn", turn: ctx.state.turn, key: "fileChanges", value: { changes: ctx.state.changes } },
    };

    function selectChanges(owner) {
      const data = owner.turn.data.get("fileChanges");
      return data === undefined ? [] : data.changes.filter((c) => c.seq <= owner.seq);
    }

    function basename(path) {
      const at = Math.max(path.lastIndexOf("/"), path.lastIndexOf("\\"));
      return at === -1 ? path : path.slice(at + 1);
    }

    function reveal(sessions, path) {
      const snapshot = sessions.list.getSnapshot();
      const current = snapshot.current;
      const cwd = current !== undefined ? snapshot.byId[current]?.cwd : undefined;
      fetch("/api/file-changes/reveal", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ path: runtime_client.resolveWorkspacePath(cwd, path) }),
      }).catch(() => {});
    }

    function Panel({ matched: changes, sessions, canReveal, t }) {
      const [openPath, setOpenPath] = react.useState(null);
      if (changes.length === 0) return null;
      const open = changes.find((c) => c.path === openPath) ?? null;

      const rows = changes.map((change) => {
        const hunks = change.hunks ?? [];
        return jsx.jsx(
          "div",
          {
            className: css.item,
            children: [
              jsx.jsx("span", {
                className: change.status === "created" ? css.badgeCreated : css.badgeModified,
                children: change.status === "created" ? t("panel.created") : t("panel.modified"),
              }, "badge"),
              jsx.jsx("span", { className: css.file, title: change.path, children: basename(change.path) }, "file"),
              jsx.jsx("button", {
                type: "button",
                className: css.action,
                disabled: hunks.length === 0,
                onClick: () => setOpenPath(change.path),
                children: t("panel.viewDiff"),
              }, "diff"),
              canReveal
                ? jsx.jsx("button", {
                    type: "button",
                    className: css.action,
                    onClick: () => reveal(sessions, change.path),
                    children: t("panel.reveal"),
                  }, "reveal")
                : null,
            ],
          },
          change.path,
        );
      });

      return jsx.jsx(
        "div",
        {
          className: css.root,
          children: [
            jsx.jsx("span", { className: css.label, children: t("panel.label") }, "label"),
            jsx.jsx("div", { className: css.list, children: rows }, "list"),
            open !== null
              ? jsx.jsx(
                  primitives.Modal,
                  {
                    open: true,
                    onClose: () => setOpenPath(null),
                    title: t("panel.diffTitle", { name: open.path }),
                    closeLabel: t("panel.close"),
                    children:
                      (open.hunks ?? []).length > 0
                        ? jsx.jsx(primitives.DiffBlock, { diffs: open.hunks })
                        : jsx.jsx("div", { className: css.noDiff, children: t("panel.noDiff") }),
                  },
                  "modal",
                )
              : null,
          ],
        },
      );
    }

    const cssText =
      ".fc_root{display:grid;grid-template-columns:max-content minmax(0,1fr);gap:6px 8px;margin-top:16px;font-size:13px;align-items:start}" +
      ".fc_label{color:var(--dsw-alias-label-tertiary)}" +
      ".fc_list{display:flex;flex-wrap:wrap;gap:6px 8px;align-items:center;min-width:0}" +
      ".fc_item{display:inline-flex;align-items:center;gap:6px;background:var(--dsw-alias-interactive-bg-hover);border-radius:6px;padding:2px 8px}" +
      ".fc_badgeCreated,.fc_badgeModified{white-space:nowrap;font-size:11px;line-height:18px;border-radius:9px;padding:0 7px}" +
      ".fc_badgeCreated{background:var(--dsw-alias-interactive-bg-active)}" +
      ".fc_badgeModified{box-shadow:inset 0 0 0 1px var(--dsw-alias-border-l2)}" +
      ".fc_file{max-width:280px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:var(--dsw-alias-label-secondary)}" +
      ".fc_action{color:var(--dsw-alias-label-tertiary);font:inherit;font-size:12px;cursor:pointer;background:none;border:none}" +
      ".fc_action:disabled{opacity:.45;cursor:default}" +
      ".fc_noDiff{color:var(--dsw-alias-label-secondary);padding:8px 0}";
    const css = {
      root: "fc_root",
      label: "fc_label",
      list: "fc_list",
      item: "fc_item",
      badgeCreated: "fc_badgeCreated",
      badgeModified: "fc_badgeModified",
      file: "fc_file",
      action: "fc_action",
      noDiff: "fc_noDiff",
    };
    const tagId = "dsh-file-changes/panel.css";
    if (typeof document !== "undefined" && document.querySelector("style[data-plugin-css=" + JSON.stringify(tagId) + "]") === null) {
      const tag = document.createElement("style");
      tag.dataset.plugin = "dsh-file-changes";
      tag.dataset.pluginCss = tagId;
      tag.textContent = cssText;
      document.head.appendChild(tag);
    }

    const inject = ["slots", "locale", "conversationEvents", "sessions", "connection"];
    function apply(ctx) {
      const connection = ctx.get("connection");
      const sessions = ctx.get("sessions");
      ctx.conversationEvents.register(definition);
      ctx.effect(() => ctx.locale.register(NS, { zh, en }), "dsh-file-changes: dictionaries");
      ctx.slots.inject("conversation.chat.turnTail", () =>
        ctx.slots.register(
          {
            name: "conversation.chat.turnTail",
            select: selectChanges,
            locale: NS,
            inject: () => ({
              sessions,
              canReveal: connection.isLoopback,
            }),
          },
          Panel,
        ),
      );
    }

    exports.apply = apply;
    exports.inject = inject;
    return module.exports;
  },
});
