/**
 * dsh-subagent-max — browser half (self-developed).
 *
 * A lightweight Subagents tab: subscribes to the harness host/mux event
 * streams to discover subagent sessions and their live status, and renders a
 * card grid grouped into active/inactive. This is intentionally simpler than
 * the upstream floating-panel viewer — it covers discovery + status + model,
 * without the drag-to-popout panel chrome.
 */
window.__ModuleLoader__.load({
  id: "dsh-subagent-max",
  factory: (require) => {
    var module = { exports: {} };
    var exports = module.exports;
    Object.defineProperty(exports, Symbol.toStringTag, { value: "Module" });
    const React = require("react");
    const jsx = require("react/jsx-runtime");

    const ZH = {
      "tab.title": "子代理",
      "empty.title": "暂无子代理",
      "empty.subtitle": "在会话中派发子代理后，会实时显示在这里",
      "status.running": "运行中",
      "status.done": "已完成",
      "status.loading": "加载中",
    };
    const EN = {
      "tab.title": "Subagents",
      "empty.title": "No subagents yet",
      "empty.subtitle": "Subagents dispatched in this session will appear here in real time",
      "status.running": "Running",
      "status.done": "Done",
      "status.loading": "Loading",
    };

    function createStore(api) {
      const subs = new Map(); // sessionId -> { id, status, model, parentId }
      const listeners = new Set();
      function emit() {
        for (const l of listeners) l();
      }
      function touch(id, patch) {
        const cur = subs.get(id) ?? { id, status: "loading", model: null, parentId: null };
        subs.set(id, { ...cur, ...patch });
        emit();
      }
      function resolveModel(sessionId) {
        try {
          api.sessions.history({ sessionId, maxMessages: 3 }, new AbortController().signal)
            .then((resp) => {
              // Best-effort: walk a few common shapes for a model id string.
              const chain = [
                resp?.request?.header?.model,
                resp?.request?.model,
                resp?.model,
                resp?.messages?.[0]?.request?.header?.model,
              ];
              const model = chain.find((m) => typeof m === "string" && m !== "");
              if (model) touch(sessionId, { model });
            })
            .catch(() => {});
        } catch {}
      }
      function handleHostFrame(frame) {
        if (frame && frame.type === "host/session-added" && frame.origin === "subagent") {
          touch(frame.sessionId, { parentId: frame.parentSessionId ?? null, status: "running" });
          resolveModel(frame.sessionId);
        }
      }
      function handleEvent(sessionId, event) {
        if (!subs.has(sessionId) || !event) return;
        if (event.type === "turn/start") touch(sessionId, { status: "running" });
        else if (event.type === "turn/end") touch(sessionId, { status: "done" });
      }
      return {
        subscribe(fn) {
          listeners.add(fn);
          return () => listeners.delete(fn);
        },
        snapshot() {
          return Array.from(subs.values());
        },
        handleHostFrame,
        handleEvent,
      };
    }

    function Tab({ store, t }) {
      const [rows, setRows] = React.useState(store.snapshot());
      React.useEffect(() => store.subscribe(() => setRows(store.snapshot())), [store]);
      if (rows.length === 0) {
        return jsx.jsx(
          "div",
          { className: css.empty, children: [
            jsx.jsx("div", { className: css.emptyTitle, children: t("empty.title") }),
            jsx.jsx("div", { className: css.emptySub, children: t("empty.subtitle") }),
          ] },
        );
      }
      const active = rows.filter((r) => r.status !== "done");
      const done = rows.filter((r) => r.status === "done");
      const group = (title, list) =>
        list.length === 0
          ? null
          : jsx.jsx(
              "div",
              { className: css.group, children: [
                jsx.jsx("div", { className: css.groupTitle, children: title }),
                jsx.jsx(
                  "div",
                  { className: css.grid, children: list.map((r) =>
                    jsx.jsx(
                      "div",
                      { className: css.card, children: [
                        jsx.jsx("span", { className: css.sid, children: r.id }, "sid"),
                        r.model ? jsx.jsx("span", { className: css.model, children: r.model }, "model") : null,
                        jsx.jsx("span", { className: r.status === "running" ? css.badgeRun : css.badgeDone, children: t("status." + (r.status === "done" ? "done" : r.status === "running" ? "running" : "loading")) }, "status"),
                      ] },
                      r.id,
                    ),
                  ) },
                ),
              ] },
            );
      return jsx.jsx(
        "div",
        { className: css.root, children: [
          group(t("status.running"), active),
          group(t("status.done"), done),
        ] },
      );
    }

    const cssText =
      ".dsm_root{padding:12px;display:flex;flex-direction:column;gap:12px}" +
      ".dsm_group{display:flex;flex-direction:column;gap:6px}" +
      ".dsm_groupTitle{font-size:12px;color:var(--dsw-alias-label-tertiary)}" +
      ".dsm_grid{display:flex;flex-wrap:wrap;gap:8px}" +
      ".dsm_card{display:inline-flex;align-items:center;gap:8px;background:var(--dsw-alias-interactive-bg-hover);border-radius:8px;padding:6px 10px}" +
      ".dsm_sid{font-family:var(--ds-font-family-code,monospace);font-size:11px;color:var(--dsw-alias-label-caption);max-width:220px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}" +
      ".dsm_model{font-family:var(--ds-font-family-code,monospace);font-size:11px;color:var(--dsw-alias-label-secondary)}" +
      ".dsm_badgeRun{font-size:11px;color:var(--dsw-alias-label-primary)}" +
      ".dsm_badgeDone{font-size:11px;color:var(--dsw-alias-label-caption)}" +
      ".dsm_empty{padding:24px;text-align:center}" +
      ".dsm_emptyTitle{font-size:14px;color:var(--dsw-alias-label-secondary)}" +
      ".dsm_emptySub{font-size:12px;color:var(--dsw-alias-label-caption);margin-top:4px}";
    const css = {
      root: "dsm_root",
      group: "dsm_group",
      groupTitle: "dsm_groupTitle",
      grid: "dsm_grid",
      card: "dsm_card",
      sid: "dsm_sid",
      model: "dsm_model",
      badgeRun: "dsm_badgeRun",
      badgeDone: "dsm_badgeDone",
      empty: "dsm_empty",
      emptyTitle: "dsm_emptyTitle",
      emptySub: "dsm_emptySub",
    };
    const tagId = "dsh-subagent-max/panel.css";
    if (typeof document !== "undefined" && document.querySelector("style[data-plugin-css=" + JSON.stringify(tagId) + "]") === null) {
      const tag = document.createElement("style");
      tag.dataset.plugin = "dsh-subagent-max";
      tag.dataset.pluginCss = tagId;
      tag.textContent = cssText;
      document.head.appendChild(tag);
    }

    const inject = ["sessions", "connection", "slots", "locale"];
    function apply(ctx) {
      const api = ctx.connection.api;
      ctx.locale.register("subagent-max", "zh", ZH);
      ctx.locale.register("subagent-max", "en", EN);
      const T = ctx.locale.bind("subagent-max");
      const store = createStore(api);

      let controller = null;
      function pump() {
        if (controller !== null) { try { controller.abort(); } catch {} }
        controller = new AbortController();
        const signal = controller.signal;
        (async () => {
          try {
            const host = api.events.host({}, signal);
            const mux = api.events.mux({}, signal);
            await Promise.all([
              (async () => { for await (const env of host) store.handleHostFrame(env && env.payload); })(),
              (async () => {
                for await (const env of mux) {
                  const frame = env && env.payload;
                  if (frame && frame.type === "session/event") store.handleEvent(frame.sessionId, frame.event);
                }
              })(),
            ]);
          } catch {}
        })();
      }

      ctx.effect(() => {
        pump();
        return () => {
          if (controller !== null) { try { controller.abort(); } catch {} }
        };
      }, "dsh-subagent-max: streams");

      ctx.slots.inject("conversation.view", () =>
        ctx.slots.register(
          { name: "conversation.view", id: "subagent-max", order: 20, label: () => T("tab.title") },
          () => jsx.jsx(Tab, { store, t: T }),
        ),
      );
    }

    exports.apply = apply;
    exports.inject = inject;
    return module.exports;
  },
});
