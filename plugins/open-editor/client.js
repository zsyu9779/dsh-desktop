window.__ModuleLoader__.load({
	id: "dsh-plugin-open-editor",
	factory: function (require) {
		var module = { exports: {} };
		var exports = module.exports;
		(function (module, exports, require) {
"use strict";
var __defProp = Object.defineProperty;
var __getOwnPropDesc = Object.getOwnPropertyDescriptor;
var __getOwnPropNames = Object.getOwnPropertyNames;
var __hasOwnProp = Object.prototype.hasOwnProperty;
var __export = (target, all) => {
  for (var name2 in all)
    __defProp(target, name2, { get: all[name2], enumerable: true });
};
var __copyProps = (to, from, except, desc) => {
  if (from && typeof from === "object" || typeof from === "function") {
    for (let key of __getOwnPropNames(from))
      if (!__hasOwnProp.call(to, key) && key !== except)
        __defProp(to, key, { get: () => from[key], enumerable: !(desc = __getOwnPropDesc(from, key)) || desc.enumerable });
  }
  return to;
};
var __toCommonJS = (mod) => __copyProps(__defProp({}, "__esModule", { value: true }), mod);

// src/client/index.tsx
var index_exports = {};
__export(index_exports, {
  apply: () => apply,
  inject: () => inject,
  name: () => name
});
module.exports = __toCommonJS(index_exports);
var import_react = require("react");
var import_jsx_runtime = require("react/jsx-runtime");
var name = "open-editor";
var inject = ["sessions", "slots", "locale"];
var LOCALE_NS = "open-editor";
var CATALOG_URL = "open-editor/editors";
var OPEN_URL = "open-editor/open";
var STYLE_TAG = "dsh-plugin-open-editor/action.css";
var ACTION_CSS = `
.dsoe-root{position:relative;display:inline-flex;align-items:center}
.dsoe-trigger{min-height:28px;color:var(--dsw-alias-label-tertiary);cursor:pointer;background:0 0;border:0;border-radius:6px;align-items:center;gap:4px;padding:3px 6px;font:inherit;font-size:12px;line-height:18px;display:inline-flex}
.dsoe-trigger:hover,.dsoe-trigger:focus-visible{color:var(--dsw-alias-label-secondary)}
.dsoe-trigger[disabled]{cursor:default;opacity:.55}
.dsoe-trigger-main{padding-right:2px}
.dsoe-caret{display:inline-flex;align-items:center;padding:0 2px}
.dsoe-caret svg{transition:transform .12s}
.dsoe-caret-open svg{transform:rotate(180deg)}
.dsoe-label{margin-left:2px}
.dsoe-menu{z-index:100;box-sizing:border-box;border:1px solid var(--dsw-alias-border-l2);background:var(--dsw-specific-menu);min-width:210px;max-width:min(320px,calc(100vw - 32px));box-shadow:var(--dsw-shadow-lv3);border-radius:12px;flex-direction:column;gap:1px;margin:0;padding:4px;list-style:none;display:flex;position:absolute;top:calc(100% + 5px);left:0}
.dsoe-head{color:var(--dsw-alias-label-tertiary);font-size:11px;line-height:16px;padding:5px 8px 4px}
.dsoe-path{color:var(--dsw-alias-label-secondary);font-family:var(--dsw-font-mono);font-size:11px;line-height:16px;white-space:nowrap;text-overflow:ellipsis;padding:0 8px 6px;max-width:280px;overflow:hidden}
.dsoe-item{box-sizing:border-box;width:100%;min-height:32px;color:var(--dsw-alias-label-primary);border-radius:8px;align-items:center;gap:8px;padding:6px 8px;font:inherit;font-size:13px;line-height:18px;cursor:pointer;background:0 0;border:0;text-align:left;display:flex}
.dsoe-item:hover:not(:disabled){background:var(--dsw-alias-interactive-bg-hover)}
.dsoe-item:disabled{color:var(--dsw-alias-label-tertiary);cursor:default;opacity:.55}
.dsoe-item-label{flex:1;min-width:0;white-space:nowrap;text-overflow:ellipsis;overflow:hidden}
.dsoe-item-mark{flex:none;display:inline-flex;align-items:center}
.dsoe-item-missing{flex:none;color:var(--dsw-alias-label-tertiary);font-size:11px}
.dsoe-sep{height:1px;background:var(--dsw-alias-border-l1);margin:4px 8px;flex:none}
.dsoe-notice{box-sizing:border-box;width:100%;border-radius:8px;align-items:center;gap:6px;padding:6px 8px;font-size:12px;line-height:16px;display:flex}
.dsoe-notice-ok{color:var(--dsw-alias-state-success-primary)}
.dsoe-notice-error{color:var(--dsw-alias-state-error-primary)}
.dsoe-spinner{flex:none;width:12px;height:12px;border-radius:50%;border:2px solid var(--dsw-alias-border-l2);border-top-color:var(--dsw-alias-label-secondary);animation:dsoe-spin .8s linear infinite}
@keyframes dsoe-spin{to{transform:rotate(360deg)}}
`;
if (typeof document !== "undefined" && document.querySelector(`style[data-plugin-css=${JSON.stringify(STYLE_TAG)}]`) === null) {
  const tag = document.createElement("style");
  tag.dataset.plugin = "dsh-plugin-open-editor";
  tag.dataset.pluginCss = STYLE_TAG;
  tag.textContent = ACTION_CSS;
  document.head.appendChild(tag);
}
var zh = {
  "button.label": "\u5728\u7F16\u8F91\u5668\u4E2D\u6253\u5F00",
  "button.aria": "\u5728\u7F16\u8F91\u5668\u4E2D\u6253\u5F00\u5F53\u524D\u9879\u76EE\uFF08\u70B9\u51FB\u6253\u5F00\u9ED8\u8BA4\u7F16\u8F91\u5668\uFF0C\u70B9\u51FB\u7BAD\u5934\u9009\u62E9\u7F16\u8F91\u5668\uFF09",
  "menu.title": "\u6253\u5F00\u5F53\u524D\u9879\u76EE",
  "menu.openDefault": "\u7528\u9ED8\u8BA4\u7F16\u8F91\u5668\u6253\u5F00",
  "notice.opened": "\u5DF2\u7528 {name} \u6253\u5F00",
  "notice.failed": "\u6253\u5F00\u5931\u8D25",
  "notice.editorMissing": "\u672A\u627E\u5230 {name}\uFF0C\u8BF7\u68C0\u67E5\u662F\u5426\u5B89\u88C5\u5E76\u52A0\u5165 PATH",
  "notice.noPath": "\u5F53\u524D\u4F1A\u8BDD\u6CA1\u6709\u9879\u76EE\u76EE\u5F55",
  "notice.busy": "\u6B63\u5728\u6253\u5F00\u2026",
  "status.missing": "\u672A\u5B89\u88C5",
  "status.default": "\u9ED8\u8BA4"
};
var en = {
  "button.label": "Open in editor",
  "button.aria": "Open the current project in an editor (click to open the default editor, click the arrow to choose)",
  "menu.title": "Open current project",
  "menu.openDefault": "Open with default editor",
  "notice.opened": "Opened with {name}",
  "notice.failed": "Failed to open",
  "notice.editorMissing": "{name} not found \u2014 is it installed and on PATH?",
  "notice.noPath": "This session has no project directory",
  "notice.busy": "Opening\u2026",
  "status.missing": "not installed",
  "status.default": "default"
};
function IconFolderOpen() {
  return /* @__PURE__ */ (0, import_jsx_runtime.jsx)("svg", { width: "14", height: "14", viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: "2", strokeLinecap: "round", strokeLinejoin: "round", "aria-hidden": "true", children: /* @__PURE__ */ (0, import_jsx_runtime.jsx)("path", { d: "m6 14 1.5-2.9A2 2 0 0 1 9.24 10H20a2 2 0 0 1 1.94 2.5l-1.54 6a2 2 0 0 1-1.95 1.5H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h3.9a2 2 0 0 1 1.69.9l.81 1.2a2 2 0 0 0 1.67.9H18a2 2 0 0 1 2 2v2" }) });
}
function IconChevronDown() {
  return /* @__PURE__ */ (0, import_jsx_runtime.jsx)("svg", { width: "12", height: "12", viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: "2", strokeLinecap: "round", strokeLinejoin: "round", "aria-hidden": "true", children: /* @__PURE__ */ (0, import_jsx_runtime.jsx)("path", { d: "m6 9 6 6 6-6" }) });
}
function IconCheck() {
  return /* @__PURE__ */ (0, import_jsx_runtime.jsx)("svg", { width: "12", height: "12", viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: "2.5", strokeLinecap: "round", strokeLinejoin: "round", "aria-hidden": "true", children: /* @__PURE__ */ (0, import_jsx_runtime.jsx)("path", { d: "M20 6 9 17l-5-5" }) });
}
function OpenEditorAction({ sessionId, useSessions, t }) {
  const cwd = useSessions((s) => s.byId[sessionId]?.cwd);
  const [open, setOpen] = (0, import_react.useState)(false);
  const [catalog, setCatalog] = (0, import_react.useState)(null);
  const [busyId, setBusyId] = (0, import_react.useState)(null);
  const [notice, setNotice] = (0, import_react.useState)(null);
  const rootRef = (0, import_react.useRef)(null);
  const triggerRef = (0, import_react.useRef)(null);
  const noticeTimer = (0, import_react.useRef)(void 0);
  (0, import_react.useEffect)(() => {
    let alive = true;
    void fetch(CATALOG_URL, { headers: { accept: "application/json" } }).then((res) => res.ok ? res.json() : null).then((data) => {
      if (alive && data) setCatalog(data);
    }).catch(() => {
    });
    return () => {
      alive = false;
    };
  }, []);
  (0, import_react.useEffect)(() => {
    if (!open) return;
    const closeOutside = (event) => {
      if (event.target instanceof Node && !rootRef.current?.contains(event.target)) setOpen(false);
    };
    const closeOnKey = (event) => {
      if (event.key === "Escape") {
        setOpen(false);
        triggerRef.current?.focus();
      }
    };
    document.addEventListener("pointerdown", closeOutside);
    document.addEventListener("keydown", closeOnKey);
    return () => {
      document.removeEventListener("pointerdown", closeOutside);
      document.removeEventListener("keydown", closeOnKey);
    };
  }, [open]);
  (0, import_react.useEffect)(() => {
    if (!notice) return;
    noticeTimer.current = setTimeout(() => setNotice(null), 3e3);
    return () => clearTimeout(noticeTimer.current);
  }, [notice]);
  const labelOf = (id) => catalog?.editors.find((e) => e.id === id)?.label ?? id;
  const defaultEditor = catalog?.default;
  const defaultLabel = defaultEditor ? labelOf(defaultEditor) : "";
  const openEditor = async (editor) => {
    if (!cwd) {
      setNotice({ kind: "error", text: t("notice.noPath") });
      return;
    }
    setBusyId(editor);
    setNotice(null);
    try {
      const res = await fetch(OPEN_URL, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ editor, path: cwd })
      });
      const data = await res.json().catch(() => null);
      if (res.ok && data?.ok) {
        setNotice({ kind: "ok", text: t("notice.opened", { name: data.label }) });
      } else if (data && !data.ok && data.code === "editor-not-found") {
        const name2 = data.error?.match(/^(.+?) is not installed/)?.[1] ?? editor;
        setNotice({ kind: "error", text: t("notice.editorMissing", { name: name2 }) });
      } else {
        setNotice({ kind: "error", text: data && !data.ok ? data.error : t("notice.failed") });
      }
    } catch {
      setNotice({ kind: "error", text: t("notice.failed") });
    } finally {
      setBusyId(null);
      setOpen(false);
    }
  };
  const editors = (0, import_react.useMemo)(() => catalog?.editors ?? [], [catalog]);
  if (!cwd) return null;
  const busy = busyId !== null;
  return /* @__PURE__ */ (0, import_jsx_runtime.jsxs)("div", { ref: rootRef, className: "dsoe-root", children: [
    /* @__PURE__ */ (0, import_jsx_runtime.jsxs)(
      "button",
      {
        ref: triggerRef,
        type: "button",
        className: "dsoe-trigger",
        "aria-label": t("button.aria"),
        disabled: busy,
        onClick: () => {
          if (defaultEditor) void openEditor(defaultEditor);
          else setOpen((v) => !v);
        },
        children: [
          busy ? /* @__PURE__ */ (0, import_jsx_runtime.jsx)("span", { className: "dsoe-spinner", "aria-hidden": "true" }) : /* @__PURE__ */ (0, import_jsx_runtime.jsx)(IconFolderOpen, {}),
          /* @__PURE__ */ (0, import_jsx_runtime.jsx)("span", { className: "dsoe-label", children: busy ? t("notice.busy") : defaultLabel || t("button.label") })
        ]
      }
    ),
    /* @__PURE__ */ (0, import_jsx_runtime.jsx)(
      "button",
      {
        type: "button",
        className: `dsoe-trigger dsoe-caret${open ? " dsoe-caret-open" : ""}`,
        "aria-label": t("menu.title"),
        "aria-expanded": open,
        disabled: busy,
        onClick: () => setOpen((v) => !v),
        children: /* @__PURE__ */ (0, import_jsx_runtime.jsx)(IconChevronDown, {})
      }
    ),
    open ? /* @__PURE__ */ (0, import_jsx_runtime.jsxs)("ul", { className: "dsoe-menu", role: "menu", "aria-label": t("menu.title"), children: [
      /* @__PURE__ */ (0, import_jsx_runtime.jsx)("li", { className: "dsoe-head", role: "presentation", children: t("menu.title") }),
      /* @__PURE__ */ (0, import_jsx_runtime.jsx)("li", { className: "dsoe-path", role: "presentation", title: cwd, children: cwd }),
      editors.map((editor, index) => {
        const isDefault = editor.id === defaultEditor;
        const isFileManager = editor.id === "explorer";
        return /* @__PURE__ */ (0, import_jsx_runtime.jsxs)("li", { role: "none", children: [
          isFileManager && index > 0 ? /* @__PURE__ */ (0, import_jsx_runtime.jsx)("div", { className: "dsoe-sep", role: "separator" }) : null,
          /* @__PURE__ */ (0, import_jsx_runtime.jsxs)(
            "button",
            {
              type: "button",
              role: "menuitem",
              className: "dsoe-item",
              disabled: !editor.available,
              onClick: () => void openEditor(editor.id),
              children: [
                /* @__PURE__ */ (0, import_jsx_runtime.jsx)("span", { className: "dsoe-item-mark", children: isDefault ? /* @__PURE__ */ (0, import_jsx_runtime.jsx)(IconCheck, {}) : null }),
                /* @__PURE__ */ (0, import_jsx_runtime.jsx)("span", { className: "dsoe-item-label", children: editor.label }),
                !editor.available ? /* @__PURE__ */ (0, import_jsx_runtime.jsx)("span", { className: "dsoe-item-missing", children: t("status.missing") }) : isDefault ? /* @__PURE__ */ (0, import_jsx_runtime.jsx)("span", { className: "dsoe-item-missing", children: t("status.default") }) : null
              ]
            }
          )
        ] }, editor.id);
      }),
      notice ? /* @__PURE__ */ (0, import_jsx_runtime.jsx)("li", { className: `dsoe-notice dsoe-notice-${notice.kind}`, role: "status", children: notice.text }) : null
    ] }) : null
  ] });
}
function apply(ctx) {
  ctx.effect(() => ctx.locale.register(LOCALE_NS, { zh, en }), "open-editor: locale dictionary");
  ctx.slots.inject(
    "conversation.session.header.actions",
    () => ctx.slots.register(
      {
        name: "conversation.session.header.actions",
        id: "open-editor",
        order: 60,
        locale: LOCALE_NS
      },
      OpenEditorAction
    )
  );
}
//# sourceMappingURL=data:application/json;base64,ewogICJ2ZXJzaW9uIjogMywKICAic291cmNlcyI6IFsic3JjL2NsaWVudC9pbmRleC50c3giXSwKICAic291cmNlc0NvbnRlbnQiOiBbIi8qKlxuICogT3Blbi1pbi1lZGl0b3IgcGx1Z2luIFx1MjAxNCBjbGllbnQgaGFsZi5cbiAqXG4gKiBDb250cmlidXRlcyBvbmUgY29udHJvbCB0byB0aGUgc2Vzc2lvbiBoZWFkZXIncyBhY3Rpb24gcm93XG4gKiAoYGNvbnZlcnNhdGlvbi5zZXNzaW9uLmhlYWRlci5hY3Rpb25zYCk6IGEgc3BsaXQgdHJpZ2dlciB0aGF0IG9wZW5zIHRoZVxuICogY3VycmVudCBzZXNzaW9uJ3Mgd29ya3NwYWNlIGRpcmVjdG9yeSBpbiB5b3VyIGVkaXRvci5cbiAqXG4gKiAtIFRoZSBtYWluIHBhcnQgb2YgdGhlIHRyaWdnZXIgb3BlbnMgdGhlIGNvbmZpZ3VyZWQgZGVmYXVsdCBlZGl0b3JcbiAqICAgKHVzdWFsbHkgVlMgQ29kZSkgd2l0aCBvbmUgY2xpY2sgXHUyMDE0IHRoZSBDb2RleC1zdHlsZSBmYXN0IHBhdGguXG4gKiAtIFRoZSBjYXJldCBvcGVucyBhIHBpY2tlciBsaXN0aW5nIGV2ZXJ5IGJ1aWx0LWluIGVkaXRvciBwbHVzIHRoZSBPUyBmaWxlXG4gKiAgIG1hbmFnZXI7IGVudHJpZXMgdGhlIGhvc3QgY291bGQgbm90IGZpbmQgb24gUEFUSCByZW5kZXIgZGlzYWJsZWQuXG4gKiAtIFRoZSB3b3Jrc3BhY2UgcGF0aCBjb21lcyBmcm9tIHRoZSBzZXNzaW9uIHN1bW1hcnkncyBgY3dkYCAodGhlIGhvc3RcbiAqICAgYWx3YXlzIGhhcyBpdCk7IHRoZSBjb250cm9sIGhpZGVzIGl0c2VsZiB3aGVuIGEgc2Vzc2lvbiBoYXMgbm9uZS5cbiAqIC0gQWxsIGxhdW5jaGluZyBoYXBwZW5zIHNlcnZlci1zaWRlIHRocm91Z2ggdGhlIHBsdWdpbidzIEhUVFAgcm91dGU7IHRoZVxuICogICBicm93c2VyIG5ldmVyIG5lZWRzIHNoZWxsIGFjY2Vzcy5cbiAqL1xuaW1wb3J0IHsgdXNlRWZmZWN0LCB1c2VNZW1vLCB1c2VSZWYsIHVzZVN0YXRlIH0gZnJvbSAncmVhY3QnXG5pbXBvcnQgdHlwZSB7IENsaWVudENvbnRleHQsIFNlc3Npb25MaXN0U3RhdGUgfSBmcm9tICdAZGVlcHNlZWstYWkvZHNoLWNsaWVudC1ydW50aW1lL2NsaWVudCdcbmltcG9ydCB0eXBlIHsgUHJvcHNMb2NhbGUsIFByb3BzUnVudGltZSwgU25hcHNob3RTZWxlY3Rvckhvb2sgfSBmcm9tICdAZGVlcHNlZWstYWkvZHNoLWNsaWVudC11aS1zbG90cydcbi8vIFR5cGUtb25seSBpbXBvcnRzIHB1bGxpbmcgdGhlIGhlYWRlci1hY3Rpb24gc2xvdCBjb250cmFjdCBhbmQgdGhlXG4vLyBzZXNzaW9uL2dsb2JhbCBzdGFuZGFyZC1raXQgbWVtYmVycyBpbnRvIHRoaXMgcHJvZ3JhbS5cbmltcG9ydCB0eXBlIHt9IGZyb20gJ0BkZWVwc2Vlay1haS9kc2gtY2xpZW50LXVpLWNvbnZlcnNhdGlvbi9jbGllbnQnXG5pbXBvcnQgdHlwZSB7fSBmcm9tICdAZGVlcHNlZWstYWkvZHNoLWNsaWVudC1sb2NhbGUvY2xpZW50J1xuaW1wb3J0IHR5cGUgeyBFZGl0b3JDYXRhbG9nUmVzcG9uc2UsIE9wZW5SZXNwb25zZSB9IGZyb20gJy4uL3NoYXJlZC90eXBlcy50cydcblxuZXhwb3J0IGNvbnN0IG5hbWUgPSAnb3Blbi1lZGl0b3InXG5cbi8qKiBSZXF1aXJlZCBjbGllbnQgc2VydmljZXMgKGZpYmVyIGluamVjdCBcdTIwMTQgd2FpdHMgZm9yIHNlc3Npb25zL3Nsb3RzL2xvY2FsZSkuICovXG5leHBvcnQgY29uc3QgaW5qZWN0ID0gWydzZXNzaW9ucycsICdzbG90cycsICdsb2NhbGUnXVxuXG5jb25zdCBMT0NBTEVfTlMgPSAnb3Blbi1lZGl0b3InXG5jb25zdCBDQVRBTE9HX1VSTCA9ICdvcGVuLWVkaXRvci9lZGl0b3JzJ1xuY29uc3QgT1BFTl9VUkwgPSAnb3Blbi1lZGl0b3Ivb3BlbidcbmNvbnN0IFNUWUxFX1RBRyA9ICdkc2gtcGx1Z2luLW9wZW4tZWRpdG9yL2FjdGlvbi5jc3MnXG5cbi8qKlxuICogQ29udHJvbCBzdHlsZXMuIEluamVjdGVkIG9uY2UgcGVyIG1hdGVyaWFsaXphdGlvbiB3aXRoIHRoZSBsb2FkZXInc1xuICogYGRhdGEtcGx1Z2luLWNzc2AgY29udHJhY3Qgc28gdGhlIGNsaWVudCBITVIgZHJpdmVyIGNhbiBpbnZlbnRvcnkvcmVtb3ZlIGl0LlxuICogUGFsZXR0ZSBjb21lcyBmcm9tIERTSCdzIC0tZHN3LSogZGVzaWduIHRva2VucywgbWlycm9yaW5nIHRoZSBpbi10cmVlXG4gKiBoZWFkZXIgYWN0aW9ucyAoam9icyAvIHN1YmFnZW50IGNhdGFsb2cpLlxuICovXG5jb25zdCBBQ1RJT05fQ1NTID0gYFxuLmRzb2Utcm9vdHtwb3NpdGlvbjpyZWxhdGl2ZTtkaXNwbGF5OmlubGluZS1mbGV4O2FsaWduLWl0ZW1zOmNlbnRlcn1cbi5kc29lLXRyaWdnZXJ7bWluLWhlaWdodDoyOHB4O2NvbG9yOnZhcigtLWRzdy1hbGlhcy1sYWJlbC10ZXJ0aWFyeSk7Y3Vyc29yOnBvaW50ZXI7YmFja2dyb3VuZDowIDA7Ym9yZGVyOjA7Ym9yZGVyLXJhZGl1czo2cHg7YWxpZ24taXRlbXM6Y2VudGVyO2dhcDo0cHg7cGFkZGluZzozcHggNnB4O2ZvbnQ6aW5oZXJpdDtmb250LXNpemU6MTJweDtsaW5lLWhlaWdodDoxOHB4O2Rpc3BsYXk6aW5saW5lLWZsZXh9XG4uZHNvZS10cmlnZ2VyOmhvdmVyLC5kc29lLXRyaWdnZXI6Zm9jdXMtdmlzaWJsZXtjb2xvcjp2YXIoLS1kc3ctYWxpYXMtbGFiZWwtc2Vjb25kYXJ5KX1cbi5kc29lLXRyaWdnZXJbZGlzYWJsZWRde2N1cnNvcjpkZWZhdWx0O29wYWNpdHk6LjU1fVxuLmRzb2UtdHJpZ2dlci1tYWlue3BhZGRpbmctcmlnaHQ6MnB4fVxuLmRzb2UtY2FyZXR7ZGlzcGxheTppbmxpbmUtZmxleDthbGlnbi1pdGVtczpjZW50ZXI7cGFkZGluZzowIDJweH1cbi5kc29lLWNhcmV0IHN2Z3t0cmFuc2l0aW9uOnRyYW5zZm9ybSAuMTJzfVxuLmRzb2UtY2FyZXQtb3BlbiBzdmd7dHJhbnNmb3JtOnJvdGF0ZSgxODBkZWcpfVxuLmRzb2UtbGFiZWx7bWFyZ2luLWxlZnQ6MnB4fVxuLmRzb2UtbWVudXt6LWluZGV4OjEwMDtib3gtc2l6aW5nOmJvcmRlci1ib3g7Ym9yZGVyOjFweCBzb2xpZCB2YXIoLS1kc3ctYWxpYXMtYm9yZGVyLWwyKTtiYWNrZ3JvdW5kOnZhcigtLWRzdy1zcGVjaWZpYy1tZW51KTttaW4td2lkdGg6MjEwcHg7bWF4LXdpZHRoOm1pbigzMjBweCxjYWxjKDEwMHZ3IC0gMzJweCkpO2JveC1zaGFkb3c6dmFyKC0tZHN3LXNoYWRvdy1sdjMpO2JvcmRlci1yYWRpdXM6MTJweDtmbGV4LWRpcmVjdGlvbjpjb2x1bW47Z2FwOjFweDttYXJnaW46MDtwYWRkaW5nOjRweDtsaXN0LXN0eWxlOm5vbmU7ZGlzcGxheTpmbGV4O3Bvc2l0aW9uOmFic29sdXRlO3RvcDpjYWxjKDEwMCUgKyA1cHgpO2xlZnQ6MH1cbi5kc29lLWhlYWR7Y29sb3I6dmFyKC0tZHN3LWFsaWFzLWxhYmVsLXRlcnRpYXJ5KTtmb250LXNpemU6MTFweDtsaW5lLWhlaWdodDoxNnB4O3BhZGRpbmc6NXB4IDhweCA0cHh9XG4uZHNvZS1wYXRoe2NvbG9yOnZhcigtLWRzdy1hbGlhcy1sYWJlbC1zZWNvbmRhcnkpO2ZvbnQtZmFtaWx5OnZhcigtLWRzdy1mb250LW1vbm8pO2ZvbnQtc2l6ZToxMXB4O2xpbmUtaGVpZ2h0OjE2cHg7d2hpdGUtc3BhY2U6bm93cmFwO3RleHQtb3ZlcmZsb3c6ZWxsaXBzaXM7cGFkZGluZzowIDhweCA2cHg7bWF4LXdpZHRoOjI4MHB4O292ZXJmbG93OmhpZGRlbn1cbi5kc29lLWl0ZW17Ym94LXNpemluZzpib3JkZXItYm94O3dpZHRoOjEwMCU7bWluLWhlaWdodDozMnB4O2NvbG9yOnZhcigtLWRzdy1hbGlhcy1sYWJlbC1wcmltYXJ5KTtib3JkZXItcmFkaXVzOjhweDthbGlnbi1pdGVtczpjZW50ZXI7Z2FwOjhweDtwYWRkaW5nOjZweCA4cHg7Zm9udDppbmhlcml0O2ZvbnQtc2l6ZToxM3B4O2xpbmUtaGVpZ2h0OjE4cHg7Y3Vyc29yOnBvaW50ZXI7YmFja2dyb3VuZDowIDA7Ym9yZGVyOjA7dGV4dC1hbGlnbjpsZWZ0O2Rpc3BsYXk6ZmxleH1cbi5kc29lLWl0ZW06aG92ZXI6bm90KDpkaXNhYmxlZCl7YmFja2dyb3VuZDp2YXIoLS1kc3ctYWxpYXMtaW50ZXJhY3RpdmUtYmctaG92ZXIpfVxuLmRzb2UtaXRlbTpkaXNhYmxlZHtjb2xvcjp2YXIoLS1kc3ctYWxpYXMtbGFiZWwtdGVydGlhcnkpO2N1cnNvcjpkZWZhdWx0O29wYWNpdHk6LjU1fVxuLmRzb2UtaXRlbS1sYWJlbHtmbGV4OjE7bWluLXdpZHRoOjA7d2hpdGUtc3BhY2U6bm93cmFwO3RleHQtb3ZlcmZsb3c6ZWxsaXBzaXM7b3ZlcmZsb3c6aGlkZGVufVxuLmRzb2UtaXRlbS1tYXJre2ZsZXg6bm9uZTtkaXNwbGF5OmlubGluZS1mbGV4O2FsaWduLWl0ZW1zOmNlbnRlcn1cbi5kc29lLWl0ZW0tbWlzc2luZ3tmbGV4Om5vbmU7Y29sb3I6dmFyKC0tZHN3LWFsaWFzLWxhYmVsLXRlcnRpYXJ5KTtmb250LXNpemU6MTFweH1cbi5kc29lLXNlcHtoZWlnaHQ6MXB4O2JhY2tncm91bmQ6dmFyKC0tZHN3LWFsaWFzLWJvcmRlci1sMSk7bWFyZ2luOjRweCA4cHg7ZmxleDpub25lfVxuLmRzb2Utbm90aWNle2JveC1zaXppbmc6Ym9yZGVyLWJveDt3aWR0aDoxMDAlO2JvcmRlci1yYWRpdXM6OHB4O2FsaWduLWl0ZW1zOmNlbnRlcjtnYXA6NnB4O3BhZGRpbmc6NnB4IDhweDtmb250LXNpemU6MTJweDtsaW5lLWhlaWdodDoxNnB4O2Rpc3BsYXk6ZmxleH1cbi5kc29lLW5vdGljZS1va3tjb2xvcjp2YXIoLS1kc3ctYWxpYXMtc3RhdGUtc3VjY2Vzcy1wcmltYXJ5KX1cbi5kc29lLW5vdGljZS1lcnJvcntjb2xvcjp2YXIoLS1kc3ctYWxpYXMtc3RhdGUtZXJyb3ItcHJpbWFyeSl9XG4uZHNvZS1zcGlubmVye2ZsZXg6bm9uZTt3aWR0aDoxMnB4O2hlaWdodDoxMnB4O2JvcmRlci1yYWRpdXM6NTAlO2JvcmRlcjoycHggc29saWQgdmFyKC0tZHN3LWFsaWFzLWJvcmRlci1sMik7Ym9yZGVyLXRvcC1jb2xvcjp2YXIoLS1kc3ctYWxpYXMtbGFiZWwtc2Vjb25kYXJ5KTthbmltYXRpb246ZHNvZS1zcGluIC44cyBsaW5lYXIgaW5maW5pdGV9XG5Aa2V5ZnJhbWVzIGRzb2Utc3Bpbnt0b3t0cmFuc2Zvcm06cm90YXRlKDM2MGRlZyl9fVxuYFxuaWYgKHR5cGVvZiBkb2N1bWVudCAhPT0gJ3VuZGVmaW5lZCcgJiYgZG9jdW1lbnQucXVlcnlTZWxlY3Rvcihgc3R5bGVbZGF0YS1wbHVnaW4tY3NzPSR7SlNPTi5zdHJpbmdpZnkoU1RZTEVfVEFHKX1dYCkgPT09IG51bGwpIHtcbiAgY29uc3QgdGFnID0gZG9jdW1lbnQuY3JlYXRlRWxlbWVudCgnc3R5bGUnKVxuICB0YWcuZGF0YXNldC5wbHVnaW4gPSAnZHNoLXBsdWdpbi1vcGVuLWVkaXRvcidcbiAgdGFnLmRhdGFzZXQucGx1Z2luQ3NzID0gU1RZTEVfVEFHXG4gIHRhZy50ZXh0Q29udGVudCA9IEFDVElPTl9DU1NcbiAgZG9jdW1lbnQuaGVhZC5hcHBlbmRDaGlsZCh0YWcpXG59XG5cbi8qKiBTaW1wbGlmaWVkIENoaW5lc2UgZGljdGlvbmFyeSAoa2V5LXNldCBzb3VyY2Ugb2YgdHJ1dGgpLiAqL1xuY29uc3QgemggPSB7XG4gICdidXR0b24ubGFiZWwnOiAnXHU1NzI4XHU3RjE2XHU4RjkxXHU1NjY4XHU0RTJEXHU2MjUzXHU1RjAwJyxcbiAgJ2J1dHRvbi5hcmlhJzogJ1x1NTcyOFx1N0YxNlx1OEY5MVx1NTY2OFx1NEUyRFx1NjI1M1x1NUYwMFx1NUY1M1x1NTI0RFx1OTg3OVx1NzZFRVx1RkYwOFx1NzBCOVx1NTFGQlx1NjI1M1x1NUYwMFx1OUVEOFx1OEJBNFx1N0YxNlx1OEY5MVx1NTY2OFx1RkYwQ1x1NzBCOVx1NTFGQlx1N0JBRFx1NTkzNFx1OTAwOVx1NjJFOVx1N0YxNlx1OEY5MVx1NTY2OFx1RkYwOScsXG4gICdtZW51LnRpdGxlJzogJ1x1NjI1M1x1NUYwMFx1NUY1M1x1NTI0RFx1OTg3OVx1NzZFRScsXG4gICdtZW51Lm9wZW5EZWZhdWx0JzogJ1x1NzUyOFx1OUVEOFx1OEJBNFx1N0YxNlx1OEY5MVx1NTY2OFx1NjI1M1x1NUYwMCcsXG4gICdub3RpY2Uub3BlbmVkJzogJ1x1NURGMlx1NzUyOCB7bmFtZX0gXHU2MjUzXHU1RjAwJyxcbiAgJ25vdGljZS5mYWlsZWQnOiAnXHU2MjUzXHU1RjAwXHU1OTMxXHU4RDI1JyxcbiAgJ25vdGljZS5lZGl0b3JNaXNzaW5nJzogJ1x1NjcyQVx1NjI3RVx1NTIzMCB7bmFtZX1cdUZGMENcdThCRjdcdTY4QzBcdTY3RTVcdTY2MkZcdTU0MjZcdTVCODlcdTg4QzVcdTVFNzZcdTUyQTBcdTUxNjUgUEFUSCcsXG4gICdub3RpY2Uubm9QYXRoJzogJ1x1NUY1M1x1NTI0RFx1NEYxQVx1OEJERFx1NkNBMVx1NjcwOVx1OTg3OVx1NzZFRVx1NzZFRVx1NUY1NScsXG4gICdub3RpY2UuYnVzeSc6ICdcdTZCNjNcdTU3MjhcdTYyNTNcdTVGMDBcdTIwMjYnLFxuICAnc3RhdHVzLm1pc3NpbmcnOiAnXHU2NzJBXHU1Qjg5XHU4OEM1JyxcbiAgJ3N0YXR1cy5kZWZhdWx0JzogJ1x1OUVEOFx1OEJBNCcsXG59IGFzIGNvbnN0XG5cbi8qKiBFbmdsaXNoIGRpY3Rpb25hcnksIGNoZWNrZWQgY29tcGxldGUgYWdhaW5zdCB0aGUgemgga2V5IHNldC4gKi9cbmNvbnN0IGVuOiBSZWNvcmQ8a2V5b2YgdHlwZW9mIHpoLCBzdHJpbmc+ID0ge1xuICAnYnV0dG9uLmxhYmVsJzogJ09wZW4gaW4gZWRpdG9yJyxcbiAgJ2J1dHRvbi5hcmlhJzogJ09wZW4gdGhlIGN1cnJlbnQgcHJvamVjdCBpbiBhbiBlZGl0b3IgKGNsaWNrIHRvIG9wZW4gdGhlIGRlZmF1bHQgZWRpdG9yLCBjbGljayB0aGUgYXJyb3cgdG8gY2hvb3NlKScsXG4gICdtZW51LnRpdGxlJzogJ09wZW4gY3VycmVudCBwcm9qZWN0JyxcbiAgJ21lbnUub3BlbkRlZmF1bHQnOiAnT3BlbiB3aXRoIGRlZmF1bHQgZWRpdG9yJyxcbiAgJ25vdGljZS5vcGVuZWQnOiAnT3BlbmVkIHdpdGgge25hbWV9JyxcbiAgJ25vdGljZS5mYWlsZWQnOiAnRmFpbGVkIHRvIG9wZW4nLFxuICAnbm90aWNlLmVkaXRvck1pc3NpbmcnOiAne25hbWV9IG5vdCBmb3VuZCBcdTIwMTQgaXMgaXQgaW5zdGFsbGVkIGFuZCBvbiBQQVRIPycsXG4gICdub3RpY2Uubm9QYXRoJzogJ1RoaXMgc2Vzc2lvbiBoYXMgbm8gcHJvamVjdCBkaXJlY3RvcnknLFxuICAnbm90aWNlLmJ1c3knOiAnT3BlbmluZ1x1MjAyNicsXG4gICdzdGF0dXMubWlzc2luZyc6ICdub3QgaW5zdGFsbGVkJyxcbiAgJ3N0YXR1cy5kZWZhdWx0JzogJ2RlZmF1bHQnLFxufVxuXG50eXBlIE9wZW5FZGl0b3JBY3Rpb25Qcm9wcyA9IFByb3BzUnVudGltZTwnY29udmVyc2F0aW9uLnNlc3Npb24uaGVhZGVyLmFjdGlvbnMnPiAmIFByb3BzTG9jYWxlPCdvcGVuLWVkaXRvcic+XG5cbmludGVyZmFjZSBOb3RpY2Uge1xuICBraW5kOiAnb2snIHwgJ2Vycm9yJ1xuICB0ZXh0OiBzdHJpbmdcbn1cblxuLyoqIEZvbGRlci1vcGVuIGljb24gKGx1Y2lkZSkuICovXG5mdW5jdGlvbiBJY29uRm9sZGVyT3BlbigpIHtcbiAgcmV0dXJuIChcbiAgICA8c3ZnIHdpZHRoPVwiMTRcIiBoZWlnaHQ9XCIxNFwiIHZpZXdCb3g9XCIwIDAgMjQgMjRcIiBmaWxsPVwibm9uZVwiIHN0cm9rZT1cImN1cnJlbnRDb2xvclwiIHN0cm9rZVdpZHRoPVwiMlwiIHN0cm9rZUxpbmVjYXA9XCJyb3VuZFwiIHN0cm9rZUxpbmVqb2luPVwicm91bmRcIiBhcmlhLWhpZGRlbj1cInRydWVcIj5cbiAgICAgIDxwYXRoIGQ9XCJtNiAxNCAxLjUtMi45QTIgMiAwIDAgMSA5LjI0IDEwSDIwYTIgMiAwIDAgMSAxLjk0IDIuNWwtMS41NCA2YTIgMiAwIDAgMS0xLjk1IDEuNUg0YTIgMiAwIDAgMS0yLTJWNWEyIDIgMCAwIDEgMi0yaDMuOWEyIDIgMCAwIDEgMS42OS45bC44MSAxLjJhMiAyIDAgMCAwIDEuNjcuOUgxOGEyIDIgMCAwIDEgMiAydjJcIiAvPlxuICAgIDwvc3ZnPlxuICApXG59XG5cbi8qKiBDaGV2cm9uLWRvd24gaWNvbiAobHVjaWRlKS4gKi9cbmZ1bmN0aW9uIEljb25DaGV2cm9uRG93bigpIHtcbiAgcmV0dXJuIChcbiAgICA8c3ZnIHdpZHRoPVwiMTJcIiBoZWlnaHQ9XCIxMlwiIHZpZXdCb3g9XCIwIDAgMjQgMjRcIiBmaWxsPVwibm9uZVwiIHN0cm9rZT1cImN1cnJlbnRDb2xvclwiIHN0cm9rZVdpZHRoPVwiMlwiIHN0cm9rZUxpbmVjYXA9XCJyb3VuZFwiIHN0cm9rZUxpbmVqb2luPVwicm91bmRcIiBhcmlhLWhpZGRlbj1cInRydWVcIj5cbiAgICAgIDxwYXRoIGQ9XCJtNiA5IDYgNiA2LTZcIiAvPlxuICAgIDwvc3ZnPlxuICApXG59XG5cbi8qKiBDaGVjayBpY29uIChsdWNpZGUpLiAqL1xuZnVuY3Rpb24gSWNvbkNoZWNrKCkge1xuICByZXR1cm4gKFxuICAgIDxzdmcgd2lkdGg9XCIxMlwiIGhlaWdodD1cIjEyXCIgdmlld0JveD1cIjAgMCAyNCAyNFwiIGZpbGw9XCJub25lXCIgc3Ryb2tlPVwiY3VycmVudENvbG9yXCIgc3Ryb2tlV2lkdGg9XCIyLjVcIiBzdHJva2VMaW5lY2FwPVwicm91bmRcIiBzdHJva2VMaW5lam9pbj1cInJvdW5kXCIgYXJpYS1oaWRkZW49XCJ0cnVlXCI+XG4gICAgICA8cGF0aCBkPVwiTTIwIDYgOSAxN2wtNS01XCIgLz5cbiAgICA8L3N2Zz5cbiAgKVxufVxuXG4vKipcbiAqIFNlc3Npb24taGVhZGVyIGFjdGlvbjogb3BlbiB0aGUgc2Vzc2lvbidzIHdvcmtzcGFjZSBpbiBhbiBlZGl0b3IuXG4gKiBSZW5kZXJzIG5vdGhpbmcgd2hpbGUgdGhlIHNlc3Npb24gaGFzIG5vIGBjd2RgIChubyB3b3Jrc3BhY2UpLlxuICovXG5mdW5jdGlvbiBPcGVuRWRpdG9yQWN0aW9uKHsgc2Vzc2lvbklkLCB1c2VTZXNzaW9ucywgdCB9OiBPcGVuRWRpdG9yQWN0aW9uUHJvcHMpIHtcbiAgY29uc3QgY3dkID0gdXNlU2Vzc2lvbnMoKHM6IFNlc3Npb25MaXN0U3RhdGUpID0+IHMuYnlJZFtzZXNzaW9uSWRdPy5jd2QpXG4gIGNvbnN0IFtvcGVuLCBzZXRPcGVuXSA9IHVzZVN0YXRlKGZhbHNlKVxuICBjb25zdCBbY2F0YWxvZywgc2V0Q2F0YWxvZ10gPSB1c2VTdGF0ZTxFZGl0b3JDYXRhbG9nUmVzcG9uc2UgfCBudWxsPihudWxsKVxuICBjb25zdCBbYnVzeUlkLCBzZXRCdXN5SWRdID0gdXNlU3RhdGU8c3RyaW5nIHwgbnVsbD4obnVsbClcbiAgY29uc3QgW25vdGljZSwgc2V0Tm90aWNlXSA9IHVzZVN0YXRlPE5vdGljZSB8IG51bGw+KG51bGwpXG4gIGNvbnN0IHJvb3RSZWYgPSB1c2VSZWY8SFRNTERpdkVsZW1lbnQ+KG51bGwpXG4gIGNvbnN0IHRyaWdnZXJSZWYgPSB1c2VSZWY8SFRNTEJ1dHRvbkVsZW1lbnQ+KG51bGwpXG4gIGNvbnN0IG5vdGljZVRpbWVyID0gdXNlUmVmPFJldHVyblR5cGU8dHlwZW9mIHNldFRpbWVvdXQ+IHwgdW5kZWZpbmVkPih1bmRlZmluZWQpXG5cbiAgLy8gTG9hZCB0aGUgaG9zdC1zaWRlIGVkaXRvciBjYXRhbG9nIG9uY2UgKGxhYmVscyArIGF2YWlsYWJpbGl0eSkuXG4gIHVzZUVmZmVjdCgoKSA9PiB7XG4gICAgbGV0IGFsaXZlID0gdHJ1ZVxuICAgIHZvaWQgZmV0Y2goQ0FUQUxPR19VUkwsIHsgaGVhZGVyczogeyBhY2NlcHQ6ICdhcHBsaWNhdGlvbi9qc29uJyB9IH0pXG4gICAgICAudGhlbigocmVzKSA9PiAocmVzLm9rID8gKHJlcy5qc29uKCkgYXMgUHJvbWlzZTxFZGl0b3JDYXRhbG9nUmVzcG9uc2U+KSA6IG51bGwpKVxuICAgICAgLnRoZW4oKGRhdGEpID0+IHtcbiAgICAgICAgaWYgKGFsaXZlICYmIGRhdGEpIHNldENhdGFsb2coZGF0YSlcbiAgICAgIH0pXG4gICAgICAuY2F0Y2goKCkgPT4ge1xuICAgICAgICAvLyBjYXRhbG9nIGlzIGEgbmljZXR5IFx1MjAxNCB0aGUgb3BlbiByb3V0ZSB2YWxpZGF0ZXMgYW55d2F5XG4gICAgICB9KVxuICAgIHJldHVybiAoKSA9PiB7XG4gICAgICBhbGl2ZSA9IGZhbHNlXG4gICAgfVxuICB9LCBbXSlcblxuICAvLyBDbG9zZSBvbiBvdXRzaWRlIHBvaW50ZXIgYW5kIEVzY2FwZSwgbGlrZSB0aGUgb3RoZXIgaGVhZGVyIHBvcG92ZXJzLlxuICB1c2VFZmZlY3QoKCkgPT4ge1xuICAgIGlmICghb3BlbikgcmV0dXJuXG4gICAgY29uc3QgY2xvc2VPdXRzaWRlID0gKGV2ZW50OiBQb2ludGVyRXZlbnQpID0+IHtcbiAgICAgIGlmIChldmVudC50YXJnZXQgaW5zdGFuY2VvZiBOb2RlICYmICFyb290UmVmLmN1cnJlbnQ/LmNvbnRhaW5zKGV2ZW50LnRhcmdldCkpIHNldE9wZW4oZmFsc2UpXG4gICAgfVxuICAgIGNvbnN0IGNsb3NlT25LZXkgPSAoZXZlbnQ6IEtleWJvYXJkRXZlbnQpID0+IHtcbiAgICAgIGlmIChldmVudC5rZXkgPT09ICdFc2NhcGUnKSB7XG4gICAgICAgIHNldE9wZW4oZmFsc2UpXG4gICAgICAgIHRyaWdnZXJSZWYuY3VycmVudD8uZm9jdXMoKVxuICAgICAgfVxuICAgIH1cbiAgICBkb2N1bWVudC5hZGRFdmVudExpc3RlbmVyKCdwb2ludGVyZG93bicsIGNsb3NlT3V0c2lkZSlcbiAgICBkb2N1bWVudC5hZGRFdmVudExpc3RlbmVyKCdrZXlkb3duJywgY2xvc2VPbktleSlcbiAgICByZXR1cm4gKCkgPT4ge1xuICAgICAgZG9jdW1lbnQucmVtb3ZlRXZlbnRMaXN0ZW5lcigncG9pbnRlcmRvd24nLCBjbG9zZU91dHNpZGUpXG4gICAgICBkb2N1bWVudC5yZW1vdmVFdmVudExpc3RlbmVyKCdrZXlkb3duJywgY2xvc2VPbktleSlcbiAgICB9XG4gIH0sIFtvcGVuXSlcblxuICAvLyBBdXRvLWRpc21pc3MgdHJhbnNpZW50IG5vdGljZXMuXG4gIHVzZUVmZmVjdCgoKSA9PiB7XG4gICAgaWYgKCFub3RpY2UpIHJldHVyblxuICAgIG5vdGljZVRpbWVyLmN1cnJlbnQgPSBzZXRUaW1lb3V0KCgpID0+IHNldE5vdGljZShudWxsKSwgMzAwMClcbiAgICByZXR1cm4gKCkgPT4gY2xlYXJUaW1lb3V0KG5vdGljZVRpbWVyLmN1cnJlbnQpXG4gIH0sIFtub3RpY2VdKVxuXG4gIGNvbnN0IGxhYmVsT2YgPSAoaWQ6IHN0cmluZykgPT4gY2F0YWxvZz8uZWRpdG9ycy5maW5kKChlKSA9PiBlLmlkID09PSBpZCk/LmxhYmVsID8/IGlkXG4gIGNvbnN0IGRlZmF1bHRFZGl0b3IgPSBjYXRhbG9nPy5kZWZhdWx0XG4gIGNvbnN0IGRlZmF1bHRMYWJlbCA9IGRlZmF1bHRFZGl0b3IgPyBsYWJlbE9mKGRlZmF1bHRFZGl0b3IpIDogJydcblxuICBjb25zdCBvcGVuRWRpdG9yID0gYXN5bmMgKGVkaXRvcjogc3RyaW5nKSA9PiB7XG4gICAgaWYgKCFjd2QpIHtcbiAgICAgIHNldE5vdGljZSh7IGtpbmQ6ICdlcnJvcicsIHRleHQ6IHQoJ25vdGljZS5ub1BhdGgnKSB9KVxuICAgICAgcmV0dXJuXG4gICAgfVxuICAgIHNldEJ1c3lJZChlZGl0b3IpXG4gICAgc2V0Tm90aWNlKG51bGwpXG4gICAgdHJ5IHtcbiAgICAgIGNvbnN0IHJlcyA9IGF3YWl0IGZldGNoKE9QRU5fVVJMLCB7XG4gICAgICAgIG1ldGhvZDogJ1BPU1QnLFxuICAgICAgICBoZWFkZXJzOiB7ICdjb250ZW50LXR5cGUnOiAnYXBwbGljYXRpb24vanNvbicgfSxcbiAgICAgICAgYm9keTogSlNPTi5zdHJpbmdpZnkoeyBlZGl0b3IsIHBhdGg6IGN3ZCB9KSxcbiAgICAgIH0pXG4gICAgICBjb25zdCBkYXRhID0gKGF3YWl0IHJlcy5qc29uKCkuY2F0Y2goKCkgPT4gbnVsbCkpIGFzIE9wZW5SZXNwb25zZSB8IG51bGxcbiAgICAgIGlmIChyZXMub2sgJiYgZGF0YT8ub2spIHtcbiAgICAgICAgc2V0Tm90aWNlKHsga2luZDogJ29rJywgdGV4dDogdCgnbm90aWNlLm9wZW5lZCcsIHsgbmFtZTogZGF0YS5sYWJlbCB9KSB9KVxuICAgICAgfSBlbHNlIGlmIChkYXRhICYmICFkYXRhLm9rICYmIGRhdGEuY29kZSA9PT0gJ2VkaXRvci1ub3QtZm91bmQnKSB7XG4gICAgICAgIGNvbnN0IG5hbWUgPSBkYXRhLmVycm9yPy5tYXRjaCgvXiguKz8pIGlzIG5vdCBpbnN0YWxsZWQvKT8uWzFdID8/IGVkaXRvclxuICAgICAgICBzZXROb3RpY2UoeyBraW5kOiAnZXJyb3InLCB0ZXh0OiB0KCdub3RpY2UuZWRpdG9yTWlzc2luZycsIHsgbmFtZSB9KSB9KVxuICAgICAgfSBlbHNlIHtcbiAgICAgICAgc2V0Tm90aWNlKHsga2luZDogJ2Vycm9yJywgdGV4dDogZGF0YSAmJiAhZGF0YS5vayA/IGRhdGEuZXJyb3IgOiB0KCdub3RpY2UuZmFpbGVkJykgfSlcbiAgICAgIH1cbiAgICB9IGNhdGNoIHtcbiAgICAgIHNldE5vdGljZSh7IGtpbmQ6ICdlcnJvcicsIHRleHQ6IHQoJ25vdGljZS5mYWlsZWQnKSB9KVxuICAgIH0gZmluYWxseSB7XG4gICAgICBzZXRCdXN5SWQobnVsbClcbiAgICAgIHNldE9wZW4oZmFsc2UpXG4gICAgfVxuICB9XG5cbiAgY29uc3QgZWRpdG9ycyA9IHVzZU1lbW8oKCkgPT4gY2F0YWxvZz8uZWRpdG9ycyA/PyBbXSwgW2NhdGFsb2ddKVxuICBpZiAoIWN3ZCkgcmV0dXJuIG51bGxcblxuICBjb25zdCBidXN5ID0gYnVzeUlkICE9PSBudWxsXG5cbiAgcmV0dXJuIChcbiAgICA8ZGl2IHJlZj17cm9vdFJlZn0gY2xhc3NOYW1lPVwiZHNvZS1yb290XCI+XG4gICAgICA8YnV0dG9uXG4gICAgICAgIHJlZj17dHJpZ2dlclJlZn1cbiAgICAgICAgdHlwZT1cImJ1dHRvblwiXG4gICAgICAgIGNsYXNzTmFtZT1cImRzb2UtdHJpZ2dlclwiXG4gICAgICAgIGFyaWEtbGFiZWw9e3QoJ2J1dHRvbi5hcmlhJyl9XG4gICAgICAgIGRpc2FibGVkPXtidXN5fVxuICAgICAgICBvbkNsaWNrPXsoKSA9PiB7XG4gICAgICAgICAgaWYgKGRlZmF1bHRFZGl0b3IpIHZvaWQgb3BlbkVkaXRvcihkZWZhdWx0RWRpdG9yKVxuICAgICAgICAgIGVsc2Ugc2V0T3BlbigodikgPT4gIXYpXG4gICAgICAgIH19XG4gICAgICA+XG4gICAgICAgIHtidXN5ID8gPHNwYW4gY2xhc3NOYW1lPVwiZHNvZS1zcGlubmVyXCIgYXJpYS1oaWRkZW49XCJ0cnVlXCIgLz4gOiA8SWNvbkZvbGRlck9wZW4gLz59XG4gICAgICAgIDxzcGFuIGNsYXNzTmFtZT1cImRzb2UtbGFiZWxcIj57YnVzeSA/IHQoJ25vdGljZS5idXN5JykgOiBkZWZhdWx0TGFiZWwgfHwgdCgnYnV0dG9uLmxhYmVsJyl9PC9zcGFuPlxuICAgICAgPC9idXR0b24+XG4gICAgICA8YnV0dG9uXG4gICAgICAgIHR5cGU9XCJidXR0b25cIlxuICAgICAgICBjbGFzc05hbWU9e2Bkc29lLXRyaWdnZXIgZHNvZS1jYXJldCR7b3BlbiA/ICcgZHNvZS1jYXJldC1vcGVuJyA6ICcnfWB9XG4gICAgICAgIGFyaWEtbGFiZWw9e3QoJ21lbnUudGl0bGUnKX1cbiAgICAgICAgYXJpYS1leHBhbmRlZD17b3Blbn1cbiAgICAgICAgZGlzYWJsZWQ9e2J1c3l9XG4gICAgICAgIG9uQ2xpY2s9eygpID0+IHNldE9wZW4oKHYpID0+ICF2KX1cbiAgICAgID5cbiAgICAgICAgPEljb25DaGV2cm9uRG93biAvPlxuICAgICAgPC9idXR0b24+XG4gICAgICB7b3BlbiA/IChcbiAgICAgICAgPHVsIGNsYXNzTmFtZT1cImRzb2UtbWVudVwiIHJvbGU9XCJtZW51XCIgYXJpYS1sYWJlbD17dCgnbWVudS50aXRsZScpfT5cbiAgICAgICAgICA8bGkgY2xhc3NOYW1lPVwiZHNvZS1oZWFkXCIgcm9sZT1cInByZXNlbnRhdGlvblwiPnt0KCdtZW51LnRpdGxlJyl9PC9saT5cbiAgICAgICAgICA8bGkgY2xhc3NOYW1lPVwiZHNvZS1wYXRoXCIgcm9sZT1cInByZXNlbnRhdGlvblwiIHRpdGxlPXtjd2R9Pntjd2R9PC9saT5cbiAgICAgICAgICB7ZWRpdG9ycy5tYXAoKGVkaXRvciwgaW5kZXgpID0+IHtcbiAgICAgICAgICAgIGNvbnN0IGlzRGVmYXVsdCA9IGVkaXRvci5pZCA9PT0gZGVmYXVsdEVkaXRvclxuICAgICAgICAgICAgY29uc3QgaXNGaWxlTWFuYWdlciA9IGVkaXRvci5pZCA9PT0gJ2V4cGxvcmVyJ1xuICAgICAgICAgICAgcmV0dXJuIChcbiAgICAgICAgICAgICAgPGxpIGtleT17ZWRpdG9yLmlkfSByb2xlPVwibm9uZVwiPlxuICAgICAgICAgICAgICAgIHtpc0ZpbGVNYW5hZ2VyICYmIGluZGV4ID4gMCA/IDxkaXYgY2xhc3NOYW1lPVwiZHNvZS1zZXBcIiByb2xlPVwic2VwYXJhdG9yXCIgLz4gOiBudWxsfVxuICAgICAgICAgICAgICAgIDxidXR0b25cbiAgICAgICAgICAgICAgICAgIHR5cGU9XCJidXR0b25cIlxuICAgICAgICAgICAgICAgICAgcm9sZT1cIm1lbnVpdGVtXCJcbiAgICAgICAgICAgICAgICAgIGNsYXNzTmFtZT1cImRzb2UtaXRlbVwiXG4gICAgICAgICAgICAgICAgICBkaXNhYmxlZD17IWVkaXRvci5hdmFpbGFibGV9XG4gICAgICAgICAgICAgICAgICBvbkNsaWNrPXsoKSA9PiB2b2lkIG9wZW5FZGl0b3IoZWRpdG9yLmlkKX1cbiAgICAgICAgICAgICAgICA+XG4gICAgICAgICAgICAgICAgICA8c3BhbiBjbGFzc05hbWU9XCJkc29lLWl0ZW0tbWFya1wiPntpc0RlZmF1bHQgPyA8SWNvbkNoZWNrIC8+IDogbnVsbH08L3NwYW4+XG4gICAgICAgICAgICAgICAgICA8c3BhbiBjbGFzc05hbWU9XCJkc29lLWl0ZW0tbGFiZWxcIj57ZWRpdG9yLmxhYmVsfTwvc3Bhbj5cbiAgICAgICAgICAgICAgICAgIHshZWRpdG9yLmF2YWlsYWJsZSA/IChcbiAgICAgICAgICAgICAgICAgICAgPHNwYW4gY2xhc3NOYW1lPVwiZHNvZS1pdGVtLW1pc3NpbmdcIj57dCgnc3RhdHVzLm1pc3NpbmcnKX08L3NwYW4+XG4gICAgICAgICAgICAgICAgICApIDogaXNEZWZhdWx0ID8gKFxuICAgICAgICAgICAgICAgICAgICA8c3BhbiBjbGFzc05hbWU9XCJkc29lLWl0ZW0tbWlzc2luZ1wiPnt0KCdzdGF0dXMuZGVmYXVsdCcpfTwvc3Bhbj5cbiAgICAgICAgICAgICAgICAgICkgOiBudWxsfVxuICAgICAgICAgICAgICAgIDwvYnV0dG9uPlxuICAgICAgICAgICAgICA8L2xpPlxuICAgICAgICAgICAgKVxuICAgICAgICAgIH0pfVxuICAgICAgICAgIHtub3RpY2UgPyAoXG4gICAgICAgICAgICA8bGkgY2xhc3NOYW1lPXtgZHNvZS1ub3RpY2UgZHNvZS1ub3RpY2UtJHtub3RpY2Uua2luZH1gfSByb2xlPVwic3RhdHVzXCI+XG4gICAgICAgICAgICAgIHtub3RpY2UudGV4dH1cbiAgICAgICAgICAgIDwvbGk+XG4gICAgICAgICAgKSA6IG51bGx9XG4gICAgICAgIDwvdWw+XG4gICAgICApIDogbnVsbH1cbiAgICA8L2Rpdj5cbiAgKVxufVxuXG4vKiogQ2xpZW50IHBsdWdpbiBib2R5LiAqL1xuZXhwb3J0IGZ1bmN0aW9uIGFwcGx5KGN0eDogQ2xpZW50Q29udGV4dCk6IHZvaWQge1xuICBjdHguZWZmZWN0KCgpID0+IGN0eC5sb2NhbGUucmVnaXN0ZXIoTE9DQUxFX05TLCB7IHpoLCBlbiB9KSwgJ29wZW4tZWRpdG9yOiBsb2NhbGUgZGljdGlvbmFyeScpXG4gIGN0eC5zbG90cy5pbmplY3QoJ2NvbnZlcnNhdGlvbi5zZXNzaW9uLmhlYWRlci5hY3Rpb25zJywgKCkgPT5cbiAgICBjdHguc2xvdHMucmVnaXN0ZXIoXG4gICAgICB7XG4gICAgICAgIG5hbWU6ICdjb252ZXJzYXRpb24uc2Vzc2lvbi5oZWFkZXIuYWN0aW9ucycsXG4gICAgICAgIGlkOiAnb3Blbi1lZGl0b3InLFxuICAgICAgICBvcmRlcjogNjAsXG4gICAgICAgIGxvY2FsZTogTE9DQUxFX05TLFxuICAgICAgfSxcbiAgICAgIE9wZW5FZGl0b3JBY3Rpb24sXG4gICAgKSxcbiAgKVxufVxuIl0sCiAgIm1hcHBpbmdzIjogIjs7Ozs7Ozs7Ozs7Ozs7Ozs7Ozs7QUFBQTtBQUFBO0FBQUE7QUFBQTtBQUFBO0FBQUE7QUFBQTtBQWdCQSxtQkFBcUQ7QUFvRy9DO0FBM0ZDLElBQU0sT0FBTztBQUdiLElBQU0sU0FBUyxDQUFDLFlBQVksU0FBUyxRQUFRO0FBRXBELElBQU0sWUFBWTtBQUNsQixJQUFNLGNBQWM7QUFDcEIsSUFBTSxXQUFXO0FBQ2pCLElBQU0sWUFBWTtBQVFsQixJQUFNLGFBQWE7QUFBQTtBQUFBO0FBQUE7QUFBQTtBQUFBO0FBQUE7QUFBQTtBQUFBO0FBQUE7QUFBQTtBQUFBO0FBQUE7QUFBQTtBQUFBO0FBQUE7QUFBQTtBQUFBO0FBQUE7QUFBQTtBQUFBO0FBQUE7QUFBQTtBQUFBO0FBQUE7QUFBQTtBQTBCbkIsSUFBSSxPQUFPLGFBQWEsZUFBZSxTQUFTLGNBQWMseUJBQXlCLEtBQUssVUFBVSxTQUFTLENBQUMsR0FBRyxNQUFNLE1BQU07QUFDN0gsUUFBTSxNQUFNLFNBQVMsY0FBYyxPQUFPO0FBQzFDLE1BQUksUUFBUSxTQUFTO0FBQ3JCLE1BQUksUUFBUSxZQUFZO0FBQ3hCLE1BQUksY0FBYztBQUNsQixXQUFTLEtBQUssWUFBWSxHQUFHO0FBQy9CO0FBR0EsSUFBTSxL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

		})(module, module.exports, require);
		return module.exports;
	}
});
