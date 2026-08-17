// dsh-subagent-max client entry (v3): visual layer aligned to design prototype.
window.__ModuleLoader__.load({
  id: "@aaravarr/dsh-subagent-max",
  factory: function (require) {
    var React = require("react");
    var primitives = require("@deepseek-ai/dsh-client-ui-primitives");
    var MarkdownText = primitives.MarkdownText;
    var StateDot = primitives.StateDot;
    var TerminalBlock = primitives.TerminalBlock;
    var ReadBlock = primitives.ReadBlock;
    var DiffBlock = primitives.DiffBlock;
    var SearchBlock = primitives.SearchBlock;
    var WebBlock = primitives.WebBlock;
    var IconChevronDownOutline14 = primitives.IconChevronDownOutline14;
    var IconChevronRightOutline14 = primitives.IconChevronRightOutline14;
    var IconChecklistOutline14 = primitives.IconChecklistOutline14;
    var IconThinkOutline14 = primitives.IconThinkOutline14;
    var IconRightUpOutline16 = primitives.IconRightUpOutline16;
    var IconBranchOutline16 = primitives.IconBranchOutline16;
    var IconSearchOutline16 = primitives.IconSearchOutline16;
    var IconBrowseOutline16 = primitives.IconBrowseOutline16;
    var IconApiOutline14 = primitives.IconApiOutline14;
    var IconEditOutline16 = primitives.IconEditOutline16;
    var IconCodeOutline16 = primitives.IconCodeOutline16;
    var DisclosureRow = primitives.DisclosureRow;
    var IconSparkle16 = primitives.IconSparkle16;
    var IconCloseOutline16 = primitives.IconCloseOutline16;
    var Toast = primitives.Toast;

    var MAX_EVENTS = 5000;
    var BASE_BACKOFF_MS = 250;
    var MAX_BACKOFF_MS = 15000;
    var lastActivity = {};
    var seedingActivity = {};
    var LA_STORAGE_KEY = "dsh-subagent-max:lastActivity";
    var laDirty = false;
    function loadLastActivity() {
      try {
        var raw = window.localStorage.getItem(LA_STORAGE_KEY);
        if (raw) {
          var parsed = JSON.parse(raw);
          for (var k in parsed) { if (lastActivity[k] === undefined) lastActivity[k] = parsed[k]; }
        }
      } catch (e) {}
    }
    function persistLastActivity() {
      try { window.localStorage.setItem(LA_STORAGE_KEY, JSON.stringify(lastActivity)); } catch (e) {}
    }
    var T = null;
    var localeCtx = null;

    var ZH = { "tab.title": "子代理", "panel.error.ended": "子代理运行失败", "tool.input": "输入", "tool.result": "结果", "tool.noOutput": "（无输出）", "prompt.label": "任务", "think.title": "Think", "tool.groupTitle": "Tool calls", "status.running": "运行中", "status.error": "失败", "status.loading": "加载中", "status.done": "已完成", "switcher.empty": "无子代理", "panel.loadingOlder": "加载更早…", "panel.loadingHistory": "加载历史…", "panel.waiting": "等待子代理输出…", "panel.streaming": "Deep diving...", "notice.receivedMessage": " 收到新消息", "notice.started": " 已开始", "open.popup": "打开小窗", "close": "关闭", "empty.title": "暂无子代理", "empty.subtitle": "在会话中派发子代理后，会实时显示在这里", "drag.release": "松开以打开", "mode.continuable": "可继续", "mode.oneshot": "一次性", "time.justNow": "刚刚", "time.minAgo": "分钟前", "time.hourAgo": "小时前", "time.dayAgo": "天前", "group.active": "活跃中", "group.inactive": "不活跃" };
    var EN = { "tab.title": "Subagents", "panel.error.ended": "Subagent run failed", "tool.input": "Input", "tool.result": "Result", "tool.noOutput": "(no output)", "prompt.label": "Task", "think.title": "Think", "tool.groupTitle": "Tool calls", "status.running": "Running", "status.error": "Failed", "status.loading": "Loading", "status.done": "Done", "switcher.empty": "No subagents", "panel.loadingOlder": "Loading earlier…", "panel.loadingHistory": "Loading history…", "panel.waiting": "Waiting for subagent output…", "panel.streaming": "Deep diving...", "notice.receivedMessage": " received a new message", "notice.started": " started", "open.popup": "Open popup", "close": "Close", "empty.title": "No subagents yet", "empty.subtitle": "Subagents dispatched in this session will appear here in real time", "drag.release": "Release to open", "mode.continuable": "Continuable", "mode.oneshot": "One-shot", "time.justNow": "now", "time.minAgo": "m ago", "time.hourAgo": "h ago", "time.dayAgo": "d ago", "group.active": "Active", "group.inactive": "Inactive" };

    function useLocale() {
      var st = React.useState(0);
      var force = st[1];
      React.useEffect(function () { return localeCtx.subscribe(function () { force(function (n) { return n + 1; }); }); }, []);
    }

    // ---- plugin css (prototype component styles, dsm- prefixed) ----
    var CSS = ".dsm-dot{position:relative;display:inline-block;flex:none;width:10px;height:10px}.dsm-dot::before{content:\"\";position:absolute;inset:0;border-radius:50%;background:currentColor;opacity:.1}.dsm-dot::after{content:\"\";position:absolute;inset:20%;border-radius:50%;background:currentColor}.dsm-dot.idle{color:var(--dsw-static-neutral-bluish-400, #adb2b8)}.dsm-dot.ready{color:var(--dsw-static-neutral-bluish-300, #cfd3d6)}.dsm-dot.ready::after{opacity:0}" +
      ".dsm-iconbtn{display:inline-flex;align-items:center;justify-content:center;width:26px;height:26px;border:none;border-radius:8px;background:transparent;color:var(--dsw-alias-label-secondary, #b8b8b8);cursor:pointer;flex:none;padding:0}.dsm-iconbtn:hover{background:var(--dsw-alias-interactive-bg-hover, rgba(255,255,255,0.08))}.dsm-iconbtn.danger:hover{background:var(--dsw-alias-interactive-bg-hover-danger, rgba(242,90,90,0.15));color:var(--dsw-alias-state-error-primary, #ef4444)}" +
      ".dsm-popup{display:flex;flex-direction:column;background:color-mix(in srgb, var(--dsw-alias-bg-layer-2, #232529) 65%, transparent);backdrop-filter:blur(8px);border:1px solid var(--dsw-alias-border-l2, #36373b);border-radius:14px;box-shadow:var(--dsw-shadow-lv3, 0 12px 32px rgba(0,0,0,0.5));overflow:hidden}" +
      ".dsm-popup-header{display:flex;align-items:center;gap:3px;padding:2px 4px 2px 6px;border-bottom:1px solid var(--dsw-alias-border-l1, #2c2d31);cursor:grab;user-select:none}.dsm-popup-header:active{cursor:grabbing}" +
      ".dsm-switcher{position:relative;display:inline-flex;align-items:center;gap:3px;min-width:0;border:none;background:transparent;border-radius:6px;padding:2px 4px;cursor:pointer;color:var(--dsw-alias-label-primary, #e6e6e6);max-width:100%}.dsm-switcher:hover{background:var(--dsw-alias-interactive-bg-hover, rgba(255,255,255,0.08))}.dsm-switcher .name{font-size:12px;line-height:18px;font-weight:500;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.dsm-switcher .chev{color:var(--dsw-alias-label-tertiary, #9a9a9a);transition:transform .12s;flex:none}.dsm-switcher.open .chev{transform:rotate(180deg)}" +
      ".dsm-menu{position:absolute;top:calc(100% + 4px);left:0;z-index:100;min-width:250px;max-width:320px;background:var(--dsw-specific-menu, #232529);border:1px solid var(--dsw-alias-border-inverted, #2c2d31);border-radius:12px;box-shadow:var(--dsw-shadow-lv3, 0 12px 32px rgba(0,0,0,0.5));padding:4px;text-align:left}.dsm-menu .mi{display:flex;align-items:center;gap:8px;width:100%;min-height:34px;padding:6px 10px;border:none;border-radius:8px;background:transparent;cursor:pointer;text-align:left;font-size:13px;line-height:18px;color:var(--dsw-alias-label-primary, #e6e6e6)}.dsm-menu .mi:hover{background:var(--dsw-alias-interactive-bg-hover, rgba(255,255,255,0.08))}.dsm-menu .mi .t{flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.dsm-menu .mi .time{color:var(--dsw-alias-label-tertiary, #9a9a9a);font-size:12px;line-height:18px;flex:none}" +
      ".dsm-popup-body{flex:1;min-height:120px;padding:10px 12px 14px;overflow-y:auto;display:flex;flex-direction:column;gap:8px}.dsm-popup-body>*{flex-shrink:0}" +
      ".dsm-msg{font-size:13px;line-height:18px;color:var(--dsw-alias-label-primary, #e6e6e6);word-break:break-word}" +
      ".dsm-tool{min-width:0}" +
      ".dsm-tool-row{position:relative;overflow:hidden}.dsm-tool-title{font-weight:400}.dsm-tool-sep{background:var(--dsw-alias-label-caption, #7a7a7a);border-radius:1px;flex:none;width:2px;height:2px;margin:0 8px}.dsm-tool-summary{text-overflow:ellipsis;white-space:nowrap;min-width:0;color:var(--dsw-alias-label-tertiary, #9a9a9a);flex:auto;font-size:14px;line-height:24px;overflow:hidden}" +
      ".dsm-tool[data-state=running] .dsm-tool-row:after{content:\"\";position:absolute;top:0;bottom:0;left:0;width:300px;pointer-events:none;background:linear-gradient(90deg,transparent 0%,color-mix(in srgb,var(--dsw-alias-bg-base, #17181a) 60%,transparent) 55%,transparent 100%);animation:dsm-sweep 2.6s ease-out infinite}@keyframes dsm-sweep{0%{left:-300px}90%,100%{left:100%}}" +
      ".dsm-toolbody{max-height:280px;overflow-y:auto}.dsm-io{display:grid;grid-template-columns:max-content 1fr;gap:0 12px;align-items:baseline;padding:4px 0 0;margin-left:20px}.dsm-io .lab{align-self:start;font-size:11px;line-height:18px;color:var(--dsw-alias-label-caption, #7a7a7a)}.dsm-io .val{min-width:0;white-space:pre-wrap;word-break:break-word;font-family:var(--ds-font-family-code, Consolas, Menlo, monospace);font-size:12px;line-height:18px;color:var(--dsw-alias-label-secondary, #b8b8b8)}" +
      ".dsm-scard{display:flex;flex-direction:column;gap:4px;padding:7px 12px;border:1px solid var(--dsw-alias-border-l2, #36373b);border-radius:12px;background:var(--dsw-alias-bg-layer-1, #1c1d21);transition:background .12s,box-shadow .12s,opacity .12s;cursor:grab}.dsm-scard:active{cursor:grabbing}.dsm-scard:hover{background:var(--dsw-alias-interactive-bg-hover, rgba(255,255,255,0.08));box-shadow:var(--dsw-shadow-lv2, 0 4px 12px rgba(0,0,0,0.3))}" +
      ".dsm-scard.dsm-dragging{opacity:.45}.dsm-drag-ghost{position:fixed;z-index:3000;pointer-events:none;border-radius:12px;border:1px dashed var(--dsw-alias-state-business-primary,#5686fe);background:color-mix(in srgb,var(--dsw-alias-bg-layer-2,#232529) 40%,transparent);box-shadow:0 12px 48px rgba(0,0,0,0.4);display:flex;align-items:center;justify-content:center;animation:dsm-ghost-in .12s ease}.dsm-drag-ghost .dsm-ghost-hint{display:inline-flex;align-items:center;gap:6px;padding:7px 14px;border-radius:999px;background:color-mix(in srgb,var(--dsw-alias-state-business-primary,#5686fe) 18%,transparent);border:1px solid var(--dsw-alias-state-business-tertiary,#34415b);color:var(--dsw-alias-state-business-primary,#5686fe);font-size:12px;line-height:18px;font-weight:500;white-space:nowrap}@keyframes dsm-ghost-in{from{opacity:0}to{opacity:1}}" +
      ".dsm-scard-top{display:flex;align-items:center;gap:8px;min-width:0}.dsm-scard-top .title{flex:1;min-width:0;font-size:13px;line-height:18px;font-weight:500;color:var(--dsw-alias-label-primary, #e6e6e6);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}" +
      ".dsm-scard-stats{display:flex;align-items:center;gap:14px;flex-wrap:wrap}.dsm-ctx{display:inline-flex;align-items:center;gap:6px}.dsm-ctx .bar{position:relative;width:96px;height:4px;border-radius:2px;background:var(--dsw-alias-border-l2, #36373b);overflow:hidden}.dsm-ctx .fill{position:absolute;left:0;top:0;bottom:0;border-radius:2px;background:var(--dsw-alias-state-business-primary, #5686fe)}.dsm-ctx.warn .fill{background:var(--dsw-alias-state-warn-primary, #f59e0b)}.dsm-ctx .pct{font-size:11px;line-height:14px;color:var(--dsw-alias-label-tertiary, #9a9a9a)}.dsm-stat{display:inline-flex;align-items:center;gap:4px;font-size:11px;line-height:14px;color:var(--dsw-alias-label-tertiary, #9a9a9a)}.dsm-stat b{font-weight:500;color:var(--dsw-alias-label-secondary, #b8b8b8)}" +
      ".dsm-scard-meta{display:flex;align-items:center;gap:6px;min-width:0;font-size:11px;line-height:14px;color:var(--dsw-alias-label-tertiary, #9a9a9a)}.dsm-scard-meta .sep{flex:none;width:2px;height:2px;border-radius:50%;background:var(--dsw-alias-label-caption, #7a7a7a)}.dsm-scard-meta .model{font-family:var(--ds-font-family-code, Consolas, Menlo, monospace);color:var(--dsw-alias-label-secondary, #b8b8b8)}.dsm-scard-meta .sid{flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-family:var(--ds-font-family-code, Consolas, Menlo, monospace);font-size:10px;line-height:14px;color:var(--dsw-alias-label-caption, #7a7a7a)}" +
      ".dsm-pill{display:inline-flex;align-items:center;gap:3px;height:18px;padding:0 6px;border-radius:9px;font-size:10px;line-height:14px;color:var(--dsw-alias-label-secondary, #b8b8b8);background:var(--dsw-alias-bg-layer-2, #232529);border:1px solid var(--dsw-alias-border-l1, #2c2d31)}.dsm-pill.cont{color:var(--dsw-alias-state-business-primary, #5686fe);border-color:var(--dsw-alias-state-business-tertiary, #34415b);background:var(--dsw-alias-state-business-tertiary, #34415b)}.dsm-grip{display:grid;grid-template-columns:repeat(2,2px);gap:1px;padding:1px 1px;color:var(--dsw-alias-label-dimmed, #7a7a7a);flex:none}.dsm-grip i{width:2px;height:2px;border-radius:50%;background:currentColor}.dsm-msg [class*='_markdown_']{font-size:13px!important;line-height:18px!important;color:var(--dsw-alias-label-primary,#e6e6e6)!important}.dsm-msg [class*='_markdown_'] h1{font-size:24px!important;line-height:32px!important;margin:20px 0 10px!important}.dsm-msg [class*='_markdown_'] h2{font-size:22px!important;line-height:30px!important;margin:16px 0 8px!important}.dsm-msg [class*='_markdown_'] h3{font-size:20px!important;line-height:28px!important;margin:14px 0 6px!important}.dsm-msg [class*='_markdown_'] h4{font-size:16px!important;line-height:24px!important;margin:12px 0 4px!important}.dsm-msg [class*='_markdown_'] h5,.dsm-msg [class*='_markdown_'] h6{font-size:14px!important;line-height:22px!important;margin:12px 0 4px!important}.dsm-msg [class*='_markdown_'] p{margin:4px 0!important}.dsm-msg [class*='_markdown_'] ul,.dsm-msg [class*='_markdown_'] ol{margin:4px 0!important;padding-left:16px!important}.dsm-msg [class*='_markdown_'] li{margin:2px 0!important}.dsm-msg [class*='_markdown_'] blockquote{margin:4px 0!important}.dsm-msg [class*='_markdown_'] hr{margin:4px 0!important}.dsm-msg [class*='_markdown_'] li::marker{line-height:18px!important}.dsm-msg [class*='_markdown_'] pre{margin:4px 0!important}.dsm-msg [class*='_markdown_'] .md-code-block{margin:4px 0!important}.dsm-msg [class*='_markdown_'] li>*:first-child{margin-top:0!important}.dsm-msg [class*='_markdown_'] li>*:last-child{margin-bottom:0!important}.dsm-msg [class*='_markdown_'] li>p:first-child{display:inline!important}.dsm-turnstatus{height:26px;font-weight:500;font-size:14px;line-height:22px;white-space:nowrap;background:linear-gradient(90deg,var(--dsw-static-deepseek-500,#4176e6) 0%,var(--dsw-static-deepseek-500,#4176e6) 40%,var(--dsw-static-deepseek-200,#d3e2ff) 50%,var(--dsw-static-deepseek-500,#4176e6) 60%,var(--dsw-static-deepseek-500,#4176e6) 100%);color:transparent;-webkit-text-fill-color:transparent;background-position:100% 0;background-size:250% 100%;-webkit-background-clip:text;background-clip:text;animation:dsm-shimmer 1.8s linear infinite;display:inline-flex;align-self:flex-start}@keyframes dsm-shimmer{to{background-position:0 0}}.dsm-toolgroup-sub{border-left:1px solid var(--dsw-alias-border-l2,#36373b);margin:4px 0 2px 22px;padding-left:8px;display:flex;flex-direction:column;gap:4px}.dsm-prompt{border-left:2px solid var(--dsw-alias-state-business-primary,#5686fe);background:color-mix(in srgb, var(--dsw-alias-bg-layer-1, #1c1d21) 65%, transparent);border-radius:0 8px 8px 0;padding:8px 12px;display:flex;flex-direction:column;gap:4px}.dsm-prompt-label{font-size:11px;line-height:14px;font-weight:500;color:var(--dsw-alias-state-business-primary,#5686fe)}.dsm-prompt-text{font-size:13px;line-height:20px;color:var(--dsw-alias-label-primary,#e6e6e6);white-space:pre-wrap;word-break:break-word;max-height:220px;overflow-y:auto}.dsm-think-summary{min-width:0;color:var(--dsw-alias-label-tertiary,#9a9a9a);text-overflow:ellipsis;white-space:nowrap;flex:auto;font-size:14px;line-height:24px;overflow:hidden}.dsm-think-body{color:var(--dsw-alias-label-tertiary,#9a9a9a);white-space:pre-wrap;word-break:break-word;padding:4px 0 4px 22px;font-size:14px;line-height:24px}.dsm-resize-hint{opacity:0;transition:opacity .15s}.dsm-popup:hover .dsm-resize-hint{opacity:1}.dsm-notice-stack{position:fixed;top:12px;right:12px;z-index:2000;display:flex;flex-direction:column;align-items:flex-end;gap:8px;pointer-events:none}.dsm-notice{pointer-events:auto;display:flex;align-items:center;gap:8px;max-width:380px;background:var(--dsw-alias-bg-layer-2,#232529);border:1px solid var(--dsw-alias-border-l2,#36373b);border-radius:12px;box-shadow:var(--dsw-shadow-lv3,0 12px 32px rgba(0,0,0,0.5));padding:10px 12px;animation:dsm-notice-in .18s ease}.dsm-notice.leaving{animation:dsm-notice-out .2s ease forwards}.dsm-notice-text{flex:1;min-width:0;font-size:13px;line-height:20px;color:var(--dsw-alias-label-primary,#e6e6e6);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.dsm-notice-btn{display:inline-flex;align-items:center;justify-content:center;width:24px;height:24px;border:none;border-radius:6px;background:transparent;color:var(--dsw-alias-label-secondary,#b8b8b8);cursor:pointer;flex:none;padding:0}.dsm-notice-btn:hover{background:var(--dsw-alias-interactive-bg-hover,rgba(255,255,255,0.08));color:var(--dsw-alias-label-primary,#e6e6e6)}@keyframes dsm-notice-in{from{transform:translateX(24px);opacity:0}to{transform:translateX(0);opacity:1}}@keyframes dsm-notice-out{from{transform:translateX(0);opacity:1}to{transform:translateX(24px);opacity:0}}";
    if (typeof document !== "undefined") {
      var cssId = "@aaravarr/dsh-subagent-max/client.css";
      if (!document.querySelector("style[data-plugin-css='" + cssId + "']")) {
        var styleTag = document.createElement("style");
        styleTag.dataset.plugin = "@aaravarr/dsh-subagent-max";
        styleTag.dataset.pluginCss = cssId;
        styleTag.textContent = CSS;
        document.head.appendChild(styleTag);
      }
    }

    // ---- small pure helpers ----
    function flattenBlocks(content) {
      if (!Array.isArray(content)) return "";
      var parts = [];
      for (var i = 0; i < content.length; i++) {
        var b = content[i];
        if (!b) continue;
        if (b.type === "text" && typeof b.text === "string") parts.push(b.text);
        else { try { parts.push(JSON.stringify(b, null, 2)); } catch (e) { try { parts.push(String(b)); } catch (e2) {} } }
      }
      return parts.join("\n");
    }
    function extractText(message) {
      if (!message) return "";
      var block = message.content && message.content[0];
      return flattenBlocks(block && block.content);
    }
    function prettyArgs(argsRaw) {
      if (argsRaw == null || argsRaw === "") return null;
      try {
        var parsed = JSON.parse(argsRaw);
        if (parsed && typeof parsed === "object") return JSON.stringify(parsed, null, 2);
      } catch (e) {}
      return String(argsRaw);
    }
    function formatTokens(value) {
      if (value === undefined || value === null) return null;
      if (value < 1000) return String(value);
      if (value < 1e6) return (value >= 100 ? String(Math.round(value / 1e3)) : String(Math.round(value / 100) / 10)) + "K";
      return (value >= 1e8 ? String(Math.round(value / 1e6)) : String(Math.round(value / 1e5) / 10)) + "M";
    }
    function formatDuration(ms) {
      if (ms === undefined || ms === null || !isFinite(ms)) return null;
      if (ms < 0) ms = 0;
      var s = Math.floor(ms / 1000);
      var m = Math.floor(s / 60);
      var h = Math.floor(m / 60);
      if (h > 0) return h + "h" + String(m % 60).padStart(2, "0") + "m";
      if (m > 0) return m + "m" + String(s % 60).padStart(2, "0") + "s";
      return s + "s";
    }
    function formatClock(ts) {
      if (!ts) return "";
      var d = new Date(ts);
      var p = function (n) { return String(n).padStart(2, "0"); };
      return p(d.getHours()) + ":" + p(d.getMinutes());
    }
    function ageOf(ts, now) {
      if (!ts) return Infinity;
      var t = (typeof ts === "number") ? ts : new Date(ts).getTime();
      if (isNaN(t)) return Infinity;
      return now - t;
    }
    function formatAgo(ts, now) {
      var age = ageOf(ts, now);
      if (!isFinite(age)) return "";
      if (age < 60000) return T("time.justNow");
      if (age < 3600000) return Math.floor(age / 60000) + T("time.minAgo");
      if (age < 86400000) return Math.floor(age / 3600000) + T("time.hourAgo");
      return Math.floor(age / 86400000) + T("time.dayAgo");
    }
    function tokenTotal(usage) {
      if (!usage) return null;
      return (usage.uncachedInputTokens || 0) + (usage.outputTokens || 0) + (usage.cacheReadTokens || 0) + (usage.cacheWriteTokens || 0);
    }
    function activeMs(timing, now) {
      if (!timing) return null;
      if (timing.active) return timing.settledMs + Math.max(0, now - timing.active.since);
      return timing.settledMs;
    }
    function tpsOf(stats) {
      if (!stats || !stats.decodeMs) return null;
      return stats.decodeTokens / (stats.decodeMs / 1000);
    }
    function contextPercent(pressure) {
      if (!pressure || !pressure.contextWindow) return null;
      var num = pressure.projectedTokens !== undefined ? pressure.projectedTokens : pressure.pressureTokens;
      if (num === undefined) return null;
      return Math.min(100, (num / pressure.contextWindow) * 100);
    }

    // ---- dot (state semantics from design) ----
    function Dot(props) {
      var st = props.state;
      if (st === "ongoing" || st === "done" || st === "warning" || st === "error") {
        return React.createElement(StateDot, { state: st, size: props.size });
      }
      return React.createElement("span", { className: "dsm-dot " + (st === "ready" ? "ready" : "idle"), style: props.size ? { width: props.size, height: props.size } : null });
    }
    function subagentDot(s, errored) {
      if (!s) return "ready";
      if (errored || s.error) return "error";
      var timing = s.projections && s.projections.subagentTiming;
      if (s.activity === "running" || s.running || (timing && timing.active)) return "ongoing";
      if (s.completed || (timing && timing.settledMs > 0)) return "done";
      if (s.loaded) return "idle";
      return "ready";
    }

    // ---- event -> item fold ----
    function eventToItem(event, view) {
      if (!event) return null;
      if (event.type === "assistant/chunk") {
        var c = event.data && event.data.chunk;
        if (c && c.type === "text-delta") return { seq: event.seq, kind: "text", text: c.text };
        if (c && c.type === "reasoning-delta") return { seq: event.seq, kind: "think", text: c.text };
        return null;
      }
      if (event.type === "user/message") {
        var um = event.data;
        return { seq: event.seq, kind: "prompt", text: flattenBlocks(um && um.content) };
      }
      var tv = view && view.view;
      if (event.type === "tool/call") {
        var d = event.data;
        return { seq: event.seq, kind: "tool", callId: String(d.callId), name: d.name, args: d.arguments, callView: view && view.for === "call" ? tv : null };
      }
      if (event.type === "tool/result") {
        var r = event.data;
        var msg = r.message;
        var resultBlock = msg && msg.content && msg.content[0];
        var callId = (msg && msg.source && msg.source.callId) ? String(msg.source.callId) : (resultBlock ? String(resultBlock.toolCallId) : null);
        var text = extractText(msg);
        if (!text && r.error) text = (r.error.name ? r.error.name : "") + ": " + (r.error.code ? r.error.code : "");
        return { seq: event.seq, kind: "result", callId: callId, text: text, error: r.error, isError: !!(resultBlock && resultBlock.isError), resultView: view && view.for === "result" ? tv : null };
      }
      return null;
    }

    function applyItem(panel, item) {
      if (item.kind === "text") {
        var last = panel.blocks.length ? panel.blocks[panel.blocks.length - 1] : null;
        if (last && last.kind === "text") last.text += item.text;
        else panel.blocks.push({ kind: "text", text: item.text });
      } else if (item.kind === "think") {
        var last2 = panel.blocks.length ? panel.blocks[panel.blocks.length - 1] : null;
        if (last2 && last2.kind === "think") last2.text += item.text;
        else panel.blocks.push({ kind: "think", text: item.text });
      } else if (item.kind === "prompt") {
        panel.blocks.push({ kind: "prompt", text: item.text });
      } else if (item.kind === "tool") {
        panel.blocks.push({ kind: "tool", callId: item.callId, name: item.name, args: item.args, status: "running", callView: item.callView || null, resultText: "", resultView: null, error: null });
      } else if (item.kind === "result") {
        var matched = false;
        for (var i = panel.blocks.length - 1; i >= 0; i--) {
          var b = panel.blocks[i];
          if (b.kind === "tool" && b.callId === item.callId) {
            matched = true;
            b.status = "done";
            b.resultText = item.text;
            b.resultView = item.resultView || null;
            b.error = item.error || null;
            break;
          }
        }
        if (!matched) {
          panel.blocks.push({ kind: "tool", callId: item.callId, name: "?", args: "", status: "done", callView: null, resultText: item.text, resultView: item.resultView || null, error: item.error || null });
        }
      }
    }

    // ---- store ----
    function createStore(api, sessions) {
      loadLastActivity();
      var panels = new Map();
      var liveEvents = new Map();
      var listeners = new Set();
      var nextId = 1;
      var zTop = 0;
      var disposed = false;
      var notifications = [];
      var modelCache = {};
      var knownSubagents = {};
      var runningState = {};
      var suppressStart = {};
      var erroredSubagents = {};
      function onBeforeUnload() { if (laDirty) persistLastActivity(); }
      var persistTimer = setInterval(function () { if (laDirty) { persistLastActivity(); laDirty = false; } }, 10000);
      window.addEventListener("beforeunload", onBeforeUnload);

      function commit() { listeners.forEach(function (fn) { try { fn(); } catch (e) {} }); }
      function bufferEvent(sessionId, item) {
        var arr = liveEvents.get(sessionId);
        if (!arr) { arr = []; liveEvents.set(sessionId, arr); }
        arr.push(item);
        if (arr.length > MAX_EVENTS) arr.splice(0, arr.length - MAX_EVENTS);
      }
      function mergeLive(sessionId, afterSeq, panel) {
        var arr = liveEvents.get(sessionId) || [];
        for (var i = 0; i < arr.length; i++) {
          if (arr[i].seq > afterSeq) { applyItem(panel, arr[i]); afterSeq = arr[i].seq; }
        }
        return afterSeq;
      }
      function handleEvent(sessionId, event, view) {
        if (!event) return;
        lastActivity[sessionId] = (typeof event.time === "number" && event.time > 0) ? event.time : Date.now();
        laDirty = true;
        if (event.type === "turn/start" || event.type === "turn/end") {
          var st = event.type === "turn/start" ? "streaming" : "done";
          panels.forEach(function (panel) {
            if (panel.sessionId === sessionId && panel.ready && panel.status !== "error") panel.status = st;
          });
          commit();
        }
        var item = eventToItem(event, view);
        if (!item) return;
        bufferEvent(sessionId, item);
        panels.forEach(function (panel) {
          if (panel.sessionId !== sessionId || !panel.ready) return;
          if (item.seq > panel.seq) {
            applyItem(panel, item);
            panel.seq = item.seq;
            if (item.kind === "text" && panel.status !== "error") panel.status = "streaming";
          }
        });
        commit();
      }
      function resolveHistory(sessionId, payload, signal) {
        var address = sessions && typeof sessions.subagentAddress === "function" ? sessions.subagentAddress(sessionId) : undefined;
        if (address) return api.subagents.history(Object.assign({}, address, payload), signal);
        return api.sessions.history(Object.assign({ sessionId: sessionId }, payload), signal);
      }
      function foldEvents(events) {
        var blocks = [];
        var tmp = { blocks: blocks };
        var seq = -1;
        var openTurn = false;
        var endedError = false;
        for (var i = 0; i < events.length; i++) {
          var ev = events[i].event;
          if (ev.type === "turn/start") { openTurn = true; endedError = false; }
          else if (ev.type === "turn/end") { openTurn = false; endedError = !!(ev.data && ev.data.reason && ev.data.reason.kind === "error"); }
          var item = eventToItem(ev, events[i].view);
          if (item) { applyItem(tmp, item); if (item.seq > seq) seq = item.seq; }
        }
        return { blocks: blocks, seq: seq, openTurn: openTurn, endedError: endedError };
      }

      function backfill(panel, signal) {
        resolveHistory(panel.sessionId, { maxMessages: 20 }, signal).then(function (resp) {
          if (disposed || !panels.has(panel.id)) return;
          if (resp && resp.result && resp.result.ok) {
            var events = resp.result.value.events || [];
            var folded = foldEvents(events);
            panel.blocks = folded.blocks;
            panel.hasMore = resp.result.value.hasMore;
            panel.baseSeq = events.length > 0 ? events[0].event.seq : null;
            panel.seq = mergeLive(panel.sessionId, folded.seq, panel);
            panel.status = folded.endedError ? "error" : (folded.openTurn ? "streaming" : "done");
            if (folded.endedError) panel.error = "@ended";
          } else {
            panel.blocks = [];
            panel.seq = -1;
            panel.status = "done";
          }
          panel.ready = true;
          commit();
        }).catch(function (err) {
          if (disposed || !panels.has(panel.id)) return;
          panel.ready = true;
          panel.status = "error";
          panel.error = String(err && err.message ? err.message : err);
          commit();
        });
      }
      function computeInitialPosition() {
        var vw = window.innerWidth, vh = window.innerHeight;
        var w = Math.min(384, vw - 28);
        var h = Math.min(460, vh - 28);
        var existing = [];
        panels.forEach(function (p) { if (p.position) existing.push(p.position); });
        function overlapAny(cand) {
          for (var j = 0; j < existing.length; j++) {
            var e = existing[j];
            if (!(cand.x + cand.w <= e.x || e.x + e.w <= cand.x || cand.y + cand.h <= e.y || e.y + e.h <= cand.y)) return e;
          }
          return null;
        }
        var MARGIN = 14, GAP = 16;
        var maxX = Math.max(MARGIN, vw - w - MARGIN);
        var maxY = Math.max(MARGIN, vh - h - MARGIN);
        var best = null, bestArea = Infinity;
        for (var cx = maxX; cx >= MARGIN; cx -= (w + GAP)) {
          for (var cy = MARGIN; cy <= maxY; cy += (h + GAP)) {
            var g = { x: cx, y: cy, w: w, h: h };
            var e = overlapAny(g);
            if (!e) return g;
            var ox = Math.max(0, Math.min(g.x + w, e.x + e.w) - Math.max(g.x, e.x));
            var oy = Math.max(0, Math.min(g.y + h, e.y + e.h) - Math.max(g.y, e.y));
            var area = ox * oy;
            if (area < bestArea) { bestArea = area; best = g; }
          }
        }
        return best || { x: MARGIN, y: MARGIN, w: w, h: h };
      }

      function openPanel(sessionId, initPos) {
        var existing = null;
        panels.forEach(function (p) { if (p.sessionId === sessionId) existing = p; });
        if (existing) return existing.id;
        var id = "panel-" + (nextId++);
        var position;
        if (initPos && typeof initPos.x === "number" && typeof initPos.y === "number" && (initPos.x > 0 || initPos.y > 0)) {
          var vw = window.innerWidth, vh = window.innerHeight;
          var w = Math.min(384, vw - 28);
          var h = Math.min(460, vh - 28);
          position = clampPos({ x: initPos.x - w / 2, y: initPos.y - h / 2, w: w, h: h });
        } else {
          position = computeInitialPosition();
        }
        var panel = { id: id, sessionId: sessionId, blocks: [], seq: -1, status: "loading", error: null, ready: false, position: position, zIndex: ++zTop, hasMore: false, baseSeq: null, loadingOlder: false };
        panels.set(id, panel);
        commit();
        backfill(panel, new AbortController().signal);
        return id;
      }
      function rebindPanel(id, sessionId) {
        var panel = panels.get(id);
        if (!panel || panel.sessionId === sessionId) return;
        panel.sessionId = sessionId;
        panel.blocks = [];
        panel.seq = -1;
        panel.status = "loading";
        panel.error = null;
        panel.ready = false;
        commit();
        backfill(panel, new AbortController().signal);
      }
      function closePanel(id) { panels.delete(id); commit(); }
      function setPosition(id, position) {
        var panel = panels.get(id);
        if (panel) { panel.position = position; commit(); }
      }
      function bringToFront(id) {
        var panel = panels.get(id);
        if (panel) { panel.zIndex = ++zTop; commit(); }
      }
      function loadOlder(id) {
        var panel = panels.get(id);
        if (!panel || panel.loadingOlder || !panel.hasMore) return;
        panel.loadingOlder = true;
        commit();
        resolveHistory(panel.sessionId, { beforeSeq: panel.baseSeq, maxMessages: 20 }, new AbortController().signal).then(function (resp) {
          if (disposed || !panels.has(id)) return;
          if (resp && resp.result && resp.result.ok) {
            var events = resp.result.value.events || [];
            if (events.length > 0) {
              var folded = foldEvents(events);
              panel.blocks = folded.blocks.concat(panel.blocks);
              panel.baseSeq = events[0].event.seq;
            }
            panel.hasMore = resp.result.value.hasMore;
          }
          panel.loadingOlder = false;
          commit();
        }).catch(function () {
          if (disposed) return;
          if (panels.has(id)) { panels.get(id).loadingOlder = false; commit(); }
        });
      }

      function pushNotification(sessionId, parentSessionId, kind) {
        var n = { id: "n" + (nextId++), sessionId: sessionId, parentSessionId: parentSessionId || null, kind: kind || "started" };
        notifications.push(n);
        if (notifications.length > 3) notifications.shift();
        commit();
        return n.id;
      }
      function dismissNotification(id) {
        notifications = notifications.filter(function (n) { return n.id !== id; });
        commit();
      }
      function handleHostFrame(frame) {
        if (!frame) return;
        if (frame.type === "host/session-added" && frame.origin === "subagent") {
          knownSubagents[frame.sessionId] = true;
          suppressStart[frame.sessionId] = true;
          runningState[frame.sessionId] = false;
          pushNotification(frame.sessionId, frame.parentSessionId, "started");
        } else if (frame.type === "host/agent-error") {
          erroredSubagents[frame.sessionId] = true;
          commit();
        } else if (frame.type === "host/session-status") {
          var sid = frame.sessionId;
          var isSubagent = knownSubagents[sid] === true;
          if (!isSubagent) {
            var list = sessions && sessions.list;
            if (list && typeof list.getSnapshot === "function") {
              var snap = list.getSnapshot();
              var row = snap && snap.byId ? snap.byId[sid] : null;
              if (row && (row.parentId || row.origin === "subagent")) { isSubagent = true; knownSubagents[sid] = true; }
            }
          }
          if (!isSubagent) return;
          var prev = runningState[sid];
          var suppressed = suppressStart[sid] === true;
          suppressStart[sid] = false;
          runningState[sid] = frame.running;
          if (frame.running === true && !suppressed && prev === false) {
            pushNotification(sid, null, "message");
          }
        }
      }
      function getErrored(sessionId) { return erroredSubagents[sessionId] === true; }
      function seedLastActivity(id) {
        if (lastActivity[id] > 0 || seedingActivity[id]) return;
        seedingActivity[id] = true;
        resolveHistory(id, { maxMessages: 1 }, new AbortController().signal).then(function (resp) {
          if (resp && resp.result && resp.result.ok) {
            var events = resp.result.value.events || [];
            var last = events[events.length - 1];
            if (last && last.event && typeof last.event.time === "number" && last.event.time > 0) {
              lastActivity[id] = last.event.time;
              laDirty = true;
            }
          }
          delete seedingActivity[id];
          commit();
        }).catch(function () {
          delete seedingActivity[id];
          commit();
        });
      }


      var modelCache = {};
      function getModel(sessionId) { return modelCache[sessionId] || null; }
      function ensureModel(sessionId) {
        if (modelCache[sessionId]) return modelCache[sessionId];
        var entry = { loaded: false };
        modelCache[sessionId] = entry;
        api.sessions.history({ sessionId: sessionId, maxMessages: 5 }, new AbortController().signal).then(function (resp) {
          if (disposed) return;
          if (resp && resp.result && resp.result.ok) {
            var events = resp.result.value.events || [];
            for (var i = events.length - 1; i >= 0; i--) {
              var ev = events[i].event;
              if (ev.type === "request/header" && ev.data && ev.data.header && ev.data.header.config) {
                entry.provider = ev.data.header.config.provider || null;
                entry.model = ev.data.header.config.model || null;
                break;
              }
            }
          }
          entry.loaded = true;
          commit();
        }).catch(function () {
          if (disposed) return;
          entry.loaded = true;
          commit();
        });
        return entry;
      }

      return {
        getPanels: function () { return Array.from(panels.values()); },
        getNotifications: function () { return notifications; },
        subscribe: function (fn) { listeners.add(fn); return function () { listeners.delete(fn); }; },
        openPanel: openPanel,
        rebindPanel: rebindPanel,
        closePanel: closePanel,
        setPosition: setPosition,
        bringToFront: bringToFront,
        loadOlder: loadOlder,
        getModel: getModel,
        ensureModel: ensureModel,
        pushNotification: pushNotification,
        dismissNotification: dismissNotification,
        handleEvent: handleEvent,
        handleHostFrame: handleHostFrame,
        getErrored: getErrored,
        seedLastActivity: seedLastActivity,
        dispose: function () { disposed = true; clearInterval(persistTimer); window.removeEventListener("beforeunload", onBeforeUnload); if (laDirty) persistLastActivity(); panels.clear(); liveEvents.clear(); notifications = []; modelCache = {}; knownSubagents = {}; runningState = {}; suppressStart = {}; erroredSubagents = {}; }
      };
    }

    // ---- collect subagent cards ----
    function isActiveSubagent(s) {
      var t = s.projections && s.projections.subagentTiming;
      return s.activity === "running" || s.running || (t && t.active);
    }
    function collectSubagents(snap) {
      var out = [];
      var byId = snap.byId || {};
      var entries = (snap.catalog && snap.catalog.entries) ? snap.catalog.entries : [];
      for (var i = 0; i < entries.length; i++) {
        var e = entries[i];
        if (e.kind !== "child") continue;
        var sum = byId[e.id];
        out.push({
          id: e.id,
          mode: e.mode,
          label: e.label,
          hasChildren: e.hasChildren,
          activity: e.activity,
          loaded: !!sum,
          title: (sum && sum.projectionValues && sum.projectionValues.subagent && sum.projectionValues.subagent.label) || e.label || (sum && (sum.title || sum.displayTitle)) || e.id,
          running: !!(sum && sum.running),
          completed: !!(sum && sum.completed),
          updatedAt: (lastActivity[e.id] !== undefined ? lastActivity[e.id] : (sum ? sum.updatedAt : undefined)),
          projections: sum ? sum.projectionValues : undefined
        });
      }
      return out;
    }
    function titleOf(byId, id) {
      var s = byId && byId[id];
      if (s) {
        var sub = s.projectionValues && s.projectionValues.subagent;
        return (sub && sub.label) || s.title || s.displayTitle || id;
      }
      return id;
    }

    // ---- tool card (official ToolRow shape) ----
    function classifyTool(name) {
      var map = { bash: "bash", pwsh: "bash", read: "read", web_fetch: "read", web_search: "search", grep: "search", glob: "search", write: "write", edit: "edit", run_code: "code" };
      return map[name] || "others";
    }
    function toolTitle(name) {
      var titles = { pwsh: "Pwsh", cordis_package_inspect: "Inspect", cordis_runtime_inspect: "Inspect", cordis_run: "Run Cordis Plugin", cordis_stop: "Stop Cordis Plugin", cordis_undefine: "Remove Cordis Plugin" };
      if (titles[name]) return titles[name];
      var vt = { search: "Search", read: "Read", bash: "Bash", write: "Write", edit: "Edit", code: "Code", others: "Tool call" };
      return vt[classifyTool(name)];
    }
    function toolIcon(name) {
      switch (classifyTool(name)) {
        case "search": return React.createElement(IconSearchOutline16, { size: 14 });
        case "read": return React.createElement(IconBrowseOutline16, { size: 14 });
        case "bash": return React.createElement(IconApiOutline14, { size: 14 });
        case "write": case "edit": return React.createElement(IconEditOutline16, { size: 14 });
        case "code": return React.createElement(IconCodeOutline16, { size: 14 });
        default: return React.createElement(IconSparkle16, { size: 14 });
      }
    }
    function toolLeading(block) {
      if (block.error) return React.createElement(StateDot, { state: "error" });
      return toolIcon(block.name);
    }
    function toolSummary(block) {
      var rv = block.resultView, cv = block.callView;
      if (cv && cv.card === "terminal") return cv.description || cv.title || block.name;
      if (cv && cv.title) return cv.title;
      if (rv && rv.title) return rv.title;
      if (rv && rv.card === "read") return rv.path || block.name;
      if (cv && cv.card === "diff") return (cv.diffs && cv.diffs.length ? cv.diffs.length + " file" : "");
      if (rv && rv.card === "search") return rv.shape === "paths" ? (rv.total + " paths") : (rv.total + " matches");
      if (rv && rv.card === "web" && rv.kind === "fetch") return rv.url;
      if (block.args) return String(block.args).slice(0, 140);
      return "";
    }
    function ioRow(label, value) {
      if (value === null || value === undefined || value === "") return null;
      return React.createElement("div", { className: "dsm-io" },
        React.createElement("span", { className: "lab" }, label),
        React.createElement("span", { className: "val" }, String(value))
      );
    }
    function genericRows(block) {
      var rows = [];
      var input = null;
      if (block.callView && block.callView.rawInput != null) {
        input = typeof block.callView.rawInput === "string" ? block.callView.rawInput : JSON.stringify(block.callView.rawInput, null, 2);
      } else {
        input = prettyArgs(block.args);
      }
      if (input) rows.push(ioRow(T("tool.input"), input));
      if (block.resultText) rows.push(ioRow(T("tool.result"), block.resultText));
      if (rows.length === 0) rows.push(React.createElement("div", { className: "dsm-io" }, React.createElement("span", { className: "val" }, T("tool.noOutput"))));
      return rows;
    }
    function toolBody(block) {
      var rv = block.resultView;
      var cv = block.callView;
      if (rv) {
        if (rv.card === "terminal") return React.createElement(TerminalBlock, { command: (cv && cv.title) || block.name, cwd: cv && cv.cwd, output: rv.output, exitCode: rv.exitCode, signal: rv.signal });
        if (rv.card === "diff") return React.createElement(DiffBlock, { diffs: rv.diffs });
        if (rv.card === "read") return React.createElement(ReadBlock, { label: rv.path, lines: rv.lines, totalLines: rv.totalLines, lang: rv.lang });
        if (rv.card === "search") {
          return rv.shape === "paths"
            ? React.createElement(SearchBlock, { kind: "paths", paths: rv.paths, truncated: rv.truncated, total: rv.total })
            : React.createElement(SearchBlock, { kind: "matches", files: rv.files, truncated: rv.truncated, total: rv.total });
        }
        if (rv.card === "web") {
          return rv.kind === "fetch"
            ? React.createElement(WebBlock, { kind: "fetch", url: rv.url, statusCode: rv.statusCode, truncated: rv.truncated })
            : React.createElement(WebBlock, { kind: "search", sources: rv.sources, answer: rv.answer, truncated: rv.truncated });
        }
        var t = flattenBlocks(rv.content);
        if (t) return React.createElement("div", { className: "dsm-msg" }, React.createElement(MarkdownText, { text: t }));
        return genericRows(block);
      }
      if (cv) {
        if (cv.card === "terminal") return React.createElement(TerminalBlock, { command: cv.title, cwd: cv.cwd, running: block.status === "running" });
        if (cv.card === "diff") return React.createElement(DiffBlock, { diffs: cv.diffs });
        return genericRows(block);
      }
      return genericRows(block);
    }
    function ToolBlockView(props) {
      var block = props.block;
      var running = block.status === "running";
      var failed = !!block.error;
      var state = running ? "running" : (failed ? "error" : "ok");
      var exp = React.useState(null);
      var expanded = exp[0];
      var setExpanded = exp[1];
      var summary = toolSummary(block);
      var body = toolBody(block);
      var expandable = body != null;
      var open = (expanded !== null ? expanded : running) && expandable;
      return React.createElement("div", { className: "dsm-tool", "data-state": state },
        React.createElement(DisclosureRow, {
          icon: toolLeading(block),
          title: toolTitle(block.name),
          open: open,
          expandable: expandable,
          onToggle: function () { setExpanded(!open); },
          expandOnRowClick: true,
          keepContentWhenOpen: true,
          rowClassName: "dsm-tool-row",
          titleClassName: "dsm-tool-title",
          collapsedContent: React.createElement(React.Fragment, null,
            summary ? React.createElement("span", { className: "dsm-tool-sep", "aria-hidden": true }) : null,
            summary ? React.createElement("span", { className: "dsm-tool-summary" }, summary) : null),
          children: body ? React.createElement("div", { className: "dsm-toolbody" }, body) : null
        })
      );
    }

    function ThinkBlock(props) {
      var text = props.text;
      var running = props.running;
      var exp = React.useState(false);
      var expanded = exp[0];
      var setExpanded = exp[1];
      var lines = text.split("\n");
      var summary = running ? (lines.length ? lines[lines.length - 1] : "") : (lines.length ? lines[0] : "");
      return React.createElement(DisclosureRow, {
        icon: React.createElement(IconThinkOutline14, { size: 14 }),
        title: T("think.title"),
        open: expanded,
        expandable: true,
        onToggle: function () { setExpanded(!expanded); },
        expandOnRowClick: true,
        rowClassName: "dsm-tool-row",
        titleClassName: "dsm-tool-title",
        collapsedContent: React.createElement(React.Fragment, null,
          React.createElement("span", { className: "dsm-tool-sep", "aria-hidden": true }),
          React.createElement("span", { className: "dsm-think-summary" }, summary)),
        children: React.createElement("div", { className: "dsm-think-body" }, text)
      });
    }

    function PromptBlock(props) {
      return React.createElement("div", { className: "dsm-prompt" },
        React.createElement("div", { className: "dsm-prompt-label" }, T("prompt.label")),
        React.createElement("div", { className: "dsm-prompt-text" }, props.text)
      );
    }

    function ToolGroup(props) {
      var blocks = props.blocks;
      var active = props.active;
      var exp = React.useState(null);
      var expanded = exp[0];
      var setExpanded = exp[1];
      var open = expanded !== null ? expanded : active;
      var last = blocks[blocks.length - 1];
      var summary = toolSummary(last) || toolTitle(last.name);
      return React.createElement(DisclosureRow, {
        icon: React.createElement(IconChecklistOutline14, { size: 14 }),
        title: T("tool.groupTitle"),
        open: open,
        expandable: true,
        onToggle: function () { setExpanded(!open); },
        expandOnRowClick: true,
        keepContentWhenOpen: false,
        rowClassName: "dsm-tool-row",
        titleClassName: "dsm-tool-title",
        collapsedContent: summary ? React.createElement(React.Fragment, null,
          React.createElement("span", { className: "dsm-tool-sep", "aria-hidden": true }),
          React.createElement("span", { className: "dsm-tool-summary" }, summary)) : null,
        children: React.createElement("div", { className: "dsm-toolgroup-sub" },
          blocks.map(function (b, i) {
            if (b.kind === "think") return React.createElement(ThinkBlock, { key: i, text: b.text, running: false });
            return React.createElement(ToolBlockView, { key: i, block: b });
          })
        )
      });
    }

    function collectToolGroup(blocks, startIdx) {
      var group = [];
      var i = startIdx;
      while (i < blocks.length) {
        var b = blocks[i];
        if (b.kind === "tool") { group.push(b); i++; }
        else if (b.kind === "think" && i + 1 < blocks.length && blocks[i + 1].kind === "tool") { group.push(b); group.push(blocks[i + 1]); i += 2; }
        else break;
      }
      return { group: group, next: i };
    }

    // ---- window geometry (mobile-safe) ----
    var PAD = 8;
    function clampPos(p) {
      var vw = window.innerWidth, vh = window.innerHeight;
      var w = Math.min(Math.max(p.w, 320), Math.max(320, vw - PAD * 2));
      var h = Math.min(Math.max(p.h, 220), Math.max(220, vh - PAD * 2));
      var x = Math.min(Math.max(p.x, PAD), Math.max(PAD, vw - w - PAD));
      var y = Math.min(Math.max(p.y, PAD), Math.max(PAD, vh - h - PAD));
      return { x: x, y: y, w: w, h: h };
    }
    function resizeClamp(x, y, w, h) {
      var vw = window.innerWidth, vh = window.innerHeight;
      return { x: x, y: y, w: Math.min(Math.max(w, 320), vw - x - PAD), h: Math.min(Math.max(h, 220), vh - y - PAD) };
    }
    function resizeClampLeft(rightEdge, y, w, h) {
      var vh = window.innerHeight;
      var nw = Math.min(Math.max(w, 320), rightEdge - PAD);
      var nh = Math.min(Math.max(h, 220), vh - y - PAD);
      return { x: rightEdge - nw, y: y, w: nw, h: nh };
    }

    // ---- panel window ----
    function PanelWindow(props) {
      var panel = props.panel;
      var subagents = props.subagents;
      var byId = props.byId;
      var index = props.index;
      var onRebind = props.onRebind;
      var onClose = props.onClose;
      var onPosition = props.onPosition;
      var onLoadOlder = props.onLoadOlder;
      var onBringToFront = props.onBringToFront;

      var pos = panel.position || { x: 14, y: 14, w: 384, h: 460 };
      var posRef = React.useRef(pos);
      posRef.current = pos;
      var swState = React.useState(false);
      var swOpen = swState[0];
      var setSwOpen = swState[1];
      var rootRef = React.useRef(null);
      var pendingAnchor = React.useRef(null);

      React.useEffect(function () {
        function onResize() { onPosition(panel.id, clampPos(posRef.current)); }
        window.addEventListener("resize", onResize);
        return function () { window.removeEventListener("resize", onResize); };
      }, []);
      React.useEffect(function () {
        if (!swOpen) return;
        function outside(e) { if (rootRef.current && !rootRef.current.contains(e.target)) setSwOpen(false); }
        document.addEventListener("pointerdown", outside);
        return function () { document.removeEventListener("pointerdown", outside); };
      }, [swOpen]);

      function startDrag(e) {
        if (e.target.closest("[data-no-drag]")) return;
        e.preventDefault();
        var sx = e.clientX, sy = e.clientY, ox = pos.x, oy = pos.y, ow = pos.w, oh = pos.h;
        function move(ev) { onPosition(panel.id, clampPos({ x: ox + ev.clientX - sx, y: oy + ev.clientY - sy, w: ow, h: oh })); }
        function up() { document.removeEventListener("pointermove", move); document.removeEventListener("pointerup", up); }
        document.addEventListener("pointermove", move);
        document.addEventListener("pointerup", up);
      }
      function startResize(e) {
        e.preventDefault(); e.stopPropagation();
        var sx = e.clientX, sy = e.clientY, ow = pos.w, oh = pos.h, ox = pos.x, oy = pos.y;
        function move(ev) { onPosition(panel.id, resizeClamp(ox, oy, ow + ev.clientX - sx, oh + ev.clientY - sy)); }
        function up() { document.removeEventListener("pointermove", move); document.removeEventListener("pointerup", up); }
        document.addEventListener("pointermove", move);
        document.addEventListener("pointerup", up);
      }
      function startResizeLeft(e) {
        e.preventDefault(); e.stopPropagation();
        var sx = e.clientX, sy = e.clientY, ow = pos.w, oh = pos.h, ox = pos.x, oy = pos.y;
        var rightEdge = ox + ow;
        function move(ev) {
          var dx = ev.clientX - sx;
          var dy = ev.clientY - sy;
          onPosition(panel.id, resizeClampLeft(rightEdge, oy, ow - dx, oh + dy));
        }
        function up() { document.removeEventListener("pointermove", move); document.removeEventListener("pointerup", up); }
        document.addEventListener("pointermove", move);
        document.addEventListener("pointerup", up);
      }

      var title = titleOf(byId, panel.sessionId);
      var pst = panel.status === "streaming" ? { dot: "ongoing", text: T("status.running") } : panel.status === "error" ? { dot: "error", text: T("status.error") } : panel.status === "loading" ? { dot: "idle", text: T("status.loading") } : { dot: "done", text: T("status.done") };

      var bodyRef = React.useRef(null);
      var atBottomRef = React.useRef(true);
      function onScroll() {
        var el = bodyRef.current;
        if (!el) return;
        atBottomRef.current = el.scrollTop + el.clientHeight >= el.scrollHeight - 4;
        if (el.scrollTop < 40 && panel.hasMore && !panel.loadingOlder) {
          pendingAnchor.current = { top: el.scrollTop, height: el.scrollHeight };
          onLoadOlder(panel.id);
        }
      }
      React.useLayoutEffect(function () {
        if (pendingAnchor.current && bodyRef.current) {
          var el = bodyRef.current;
          var a = pendingAnchor.current;
          el.scrollTop = a.top + (el.scrollHeight - a.height);
          pendingAnchor.current = null;
        }
      }, [panel.blocks]);
      React.useEffect(function () {
        var el = bodyRef.current;
        if (el && atBottomRef.current) el.scrollTop = el.scrollHeight;
      }, [panel.seq]);

      var blockViews = [];
      var bi = 0;
      while (bi < panel.blocks.length) {
        var bl = panel.blocks[bi];
        if (bl.kind === "text") {
          blockViews.push(React.createElement("div", { key: "b" + bi, className: "dsm-msg" }, React.createElement(MarkdownText, { text: bl.text, streaming: panel.status === "streaming" })));
          bi++;
        } else if (bl.kind === "prompt") {
          blockViews.push(React.createElement(PromptBlock, { key: "p" + bi, text: bl.text }));
          bi++;
        } else if (bl.kind === "think" && !(bi + 1 < panel.blocks.length && panel.blocks[bi + 1].kind === "tool")) {
          blockViews.push(React.createElement(ThinkBlock, { key: "t" + bi, text: bl.text, running: panel.status === "streaming" }));
          bi++;
        } else {
          var gstart = bi;
          var grouped = collectToolGroup(panel.blocks, bi);
          bi = grouped.next;
          var grp = grouped.group;
          var grpRunning = grp.some(function (b) { return b.kind === "tool" && b.status === "running"; });
          var grpHasTextAfter = false;
          for (var j = grouped.next; j < panel.blocks.length; j++) { var kb = panel.blocks[j].kind; if (kb === "text" || kb === "prompt") { grpHasTextAfter = true; break; } }
          var grpActive = grpRunning || (panel.status === "streaming" && !grpHasTextAfter);
          blockViews.push(React.createElement(ToolGroup, { key: "g" + gstart, blocks: grp, active: grpActive }));
        }
      }

      return React.createElement("div", { ref: rootRef, className: "dsm-popup", style: { position: "fixed", left: pos.x, top: pos.y, width: pos.w, height: pos.h, zIndex: panel.zIndex }, onPointerDown: function () { onBringToFront(panel.id); } },
        React.createElement("div", { className: "dsm-popup-header", onPointerDown: startDrag },
          React.createElement("span", { className: "dsm-grip" },
            React.createElement("i", null), React.createElement("i", null), React.createElement("i", null),
            React.createElement("i", null), React.createElement("i", null), React.createElement("i", null)
          ),
          React.createElement("span", { "data-no-drag": "1", className: "dsm-switcher" + (swOpen ? " open" : ""), onClick: function () { setSwOpen(!swOpen); } },
            React.createElement("span", { className: "name" }, title),
            React.createElement("span", { className: "chev" }, React.createElement(IconChevronDownOutline14, {})),
            swOpen ? React.createElement("span", { className: "dsm-menu", onClick: function (e) { e.stopPropagation(); } },
              subagents.length === 0
                ? React.createElement("div", { className: "mi" }, React.createElement("span", { className: "t" }, T("switcher.empty")))
                : subagents.map(function (s) {
                    return React.createElement("button", { key: s.id, className: "mi", onClick: function () { onRebind(panel.id, s.id); setSwOpen(false); } },
                      React.createElement("span", { style: { display: "inline-flex", width: 10, height: 10, alignItems: "center", justifyContent: "center" } }, React.createElement(Dot, { state: subagentDot(s) })),
                      React.createElement("span", { className: "t" }, s.title),
                      React.createElement("span", { className: "time" }, s.running ? formatDuration(s.projections && s.projections.subagentTiming ? activeMs(s.projections.subagentTiming, Date.now()) : null) : formatClock(s.updatedAt))
                    );
                  })
            ) : null
          ),
          React.createElement("span", { className: "dsm-scard-meta", style: { marginLeft: 2, gap: 5, flex: "none" } },
            React.createElement(Dot, { state: pst.dot }),
            React.createElement("span", null, pst.text)
          ),
          React.createElement("span", { style: { flex: 1 } }),
          React.createElement("button", { "data-no-drag": "1", className: "dsm-iconbtn danger", onClick: function () { onClose(panel.id); }, title: T("close") }, "×")
        ),
        React.createElement("div", { ref: bodyRef, onScroll: onScroll, className: "dsm-popup-body" },
          panel.loadingOlder ? React.createElement("div", { style: { color: "var(--dsw-alias-label-tertiary, #9a9a9a)", fontSize: 12, textAlign: "center", padding: "4px 0" } }, T("panel.loadingOlder")) : null,
          panel.status === "loading" ? React.createElement("div", { style: { color: "var(--dsw-alias-label-tertiary, #9a9a9a)", fontSize: 13 } }, T("panel.loadingHistory")) :
          panel.status === "error" ? React.createElement("div", { style: { color: "var(--dsw-alias-state-error-primary, #ef4444)", fontSize: 13 } }, String(panel.error === "@ended" ? T("panel.error.ended") : (panel.error || "error"))) :
          blockViews.length === 0 ? React.createElement("div", { style: { display: "flex", flexDirection: "column", alignItems: "center", justifyContent: "center", gap: 6, padding: "30px 16px", color: "var(--dsw-alias-label-tertiary, #9a9a9a)", fontSize: 13, textAlign: "center" } },
            React.createElement("span", { style: { color: "var(--dsw-alias-label-dimmed, #7a7a7a)", display: "inline-flex" } }, React.createElement(IconCodeOutline16, {})),
            React.createElement("span", null, T("panel.waiting"))
          ) :
          blockViews,
          panel.status === "streaming" ? React.createElement("div", { className: "dsm-turnstatus" }, T("panel.streaming")) : null
        ),
        React.createElement("div", { onPointerDown: startResize, style: { position: "absolute", right: 0, bottom: 0, width: 18, height: 18, cursor: "nwse-resize" } },
          React.createElement("div", { className: "dsm-resize-hint", style: { position: "absolute", right: 4, bottom: 4, width: 9, height: 9, borderRight: "1.5px solid var(--dsw-alias-label-tertiary, #9a9a9a)", borderBottom: "1.5px solid var(--dsw-alias-label-tertiary, #9a9a9a)", borderBottomRightRadius: 5 } })
        ),
        React.createElement("div", { onPointerDown: startResizeLeft, style: { position: "absolute", left: 0, bottom: 0, width: 18, height: 18, cursor: "nesw-resize" } },
          React.createElement("div", { className: "dsm-resize-hint", style: { position: "absolute", left: 4, bottom: 4, width: 9, height: 9, borderLeft: "1.5px solid var(--dsw-alias-label-tertiary, #9a9a9a)", borderBottom: "1.5px solid var(--dsw-alias-label-tertiary, #9a9a9a)", borderBottomLeftRadius: 5 } })
        )
      );
    }

    // ---- side notification stack (custom) ----
    function SideNotification(props) {
      var n = props.notification;
      var byId = props.byId;
      var store = props.store;
      var parentTitle = n.parentSessionId ? titleOf(byId, n.parentSessionId) : "";
      var subTitle = titleOf(byId, n.sessionId);
      var text = n.kind === "message"
        ? subTitle + T("notice.receivedMessage")
        : (parentTitle && parentTitle !== n.parentSessionId ? parentTitle + " · " : "") + subTitle + T("notice.started");
      var leaveState = React.useState(false);
      var leaving = leaveState[0];
      var setLeaving = leaveState[1];
      React.useEffect(function () {
        var t1 = setTimeout(function () { setLeaving(true); }, 59800);
        var t2 = setTimeout(function () { store.dismissNotification(n.id); }, 60000);
        return function () { clearTimeout(t1); clearTimeout(t2); };
      }, [n.id, store]);
      return React.createElement("div", { className: "dsm-notice" + (leaving ? " leaving" : "") },
        React.createElement("span", { className: "dsm-notice-text" }, text),
        React.createElement("button", { className: "dsm-notice-btn", title: T("open.popup"), onClick: function () { store.openPanel(n.sessionId); store.dismissNotification(n.id); } }, React.createElement(IconRightUpOutline16, {})),
        React.createElement("button", { className: "dsm-notice-btn", title: T("close"), onClick: function () { store.dismissNotification(n.id); } }, React.createElement(IconCloseOutline16, {}))
      );
    }

    // ---- shell.overlay root ----
    function ShellOverlay(props) {
      var store = props.store;
      useLocale();
      var useSessions = props.useSessions || (function () { return {}; });
      var force = React.useReducer(function (x) { return x + 1; }, 0)[1];
      React.useEffect(function () { return store.subscribe(function () { force(); }); }, [store]);

      var snap = useSessions(function (s) {
        return { current: s.current, byId: s.byId, catalog: s.current === undefined ? undefined : s.subagentsByParent[s.current] };
      });

      var subagents = collectSubagents(snap);
      var panels = store.getPanels();
      var notifications = store.getNotifications();

      return React.createElement("div", { style: { position: "fixed", inset: 0, pointerEvents: "none", zIndex: 1000 } },
        panels.map(function (panel, i) {
          return React.createElement("div", { key: panel.id, style: { pointerEvents: "auto" } },
            React.createElement(PanelWindow, {
              panel: panel, subagents: subagents, byId: snap.byId || {}, index: i,
              onRebind: function (id, sid) { store.rebindPanel(id, sid); },
              onClose: function (id) { store.closePanel(id); },
              onPosition: function (id, pos) { store.setPosition(id, pos); },
              onLoadOlder: function (id) { store.loadOlder(id); },
              onBringToFront: function (id) { store.bringToFront(id); }
            })
          );
        }),
        notifications.length
          ? React.createElement("div", { className: "dsm-notice-stack" },
              notifications.map(function (n) {
                return React.createElement(SideNotification, { key: n.id, notification: n, byId: snap.byId || {}, store: store });
              })
            )
          : null
      );
    }

    // ---- conversation.view tab: subagent cards ----
    function SubagentsTab(props) {
      var sessionId = props.sessionId;
      useLocale();
      var useSessions = props.useSessions;
      var openPanel = props.openPanel;
      var getModel = props.getModel;
      var ensureModel = props.ensureModel;
      var getErrored = props.getErrored;
      var seedLastActivity = props.seedLastActivity;
      var subscribe = props.subscribe;
      var snap = useSessions(function (s) { return { byId: s.byId, catalog: s.subagentsByParent[sessionId] }; });
      var subagents = collectSubagents(snap);
      var subagentIds = subagents.map(function (s) { return s.id; }).join(",");
      React.useEffect(function () {
        subagents.forEach(function (s) { ensureModel(s.id); seedLastActivity(s.id); });
      }, [subagentIds]);
      var now = Date.now();
      var tickState = React.useState(0);
      var setTick = tickState[1];
      React.useEffect(function () { return subscribe(function () { setTick(function (n) { return n + 1; }); }); }, [subscribe]);
      React.useEffect(function () {
        var id = setInterval(function () { setTick(function (n) { return n + 1; }); }, 60000);
        return function () { clearInterval(id); };
      }, []);

      var activeList = [], inactiveList = [];
      subagents.forEach(function (s) { (isActiveSubagent(s) ? activeList : inactiveList).push(s); });
      function byNewest(a, b) { return (b.updatedAt || 0) - (a.updatedAt || 0); }
      activeList.sort(byNewest);
      inactiveList.sort(byNewest);
      function groupHead(key, count) {
        return React.createElement("div", { className: "dsm-scard-grouphead", style: { gridColumn: "1 / -1", fontSize: 11, lineHeight: "16px", fontWeight: 500, color: "var(--dsw-alias-label-tertiary, #9a9a9a)", marginTop: 2 } }, T(key) + " (" + count + ")");
      }
      function renderCard(s) {
        var proj = s.projections || {};
        var timing = proj.subagentTiming;
        var usage = proj.tokenUsage;
        var stats = proj.sessionStats;
        var pressure = proj.contextPressure;
        var ident = proj.subagent;
        var total = tokenTotal(usage);
        var pct = contextPercent(pressure);
        var tps = tpsOf(stats);
        var dur = timing ? activeMs(timing, now) : null;
        var mode = (ident && ident.mode) || s.mode || null;
        var model = getModel(s.id);
        return React.createElement("div", { key: s.id, className: "dsm-scard",
          onPointerDown: function (e) {
            if (e.button !== 0) return;
            if (e.target.closest("button") || e.target.closest("[data-no-drag]")) return;
            e.preventDefault();
            e.currentTarget.setPointerCapture(e.pointerId);
            var card = e.currentTarget;
            var sx = e.clientX, sy = e.clientY, dragging = false, ghost = null;
            function move(ev) {
              if (!dragging && (Math.abs(ev.clientX - sx) >= 5 || Math.abs(ev.clientY - sy) >= 5)) {
                dragging = true;
                document.body.style.cursor = "grabbing";
                card.classList.add("dsm-dragging");
                ghost = document.createElement("div");
                ghost.className = "dsm-drag-ghost";
                ghost.innerHTML = '<span class="dsm-ghost-hint"><svg width="14" height="14" viewBox="0 0 16 16" fill="none"><path d="M6 3h7v7M13 3 4.5 11.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg>' + T("drag.release") + '</span>';
                document.body.appendChild(ghost);
              }
              if (dragging && ghost) {
                var vw = window.innerWidth, vh = window.innerHeight;
                var gw = Math.min(384, vw - 28), gh = Math.min(460, vh - 28);
                var gp = clampPos({ x: ev.clientX - gw / 2, y: ev.clientY - gh / 2, w: gw, h: gh });
                ghost.style.left = gp.x + "px";
                ghost.style.top = gp.y + "px";
                ghost.style.width = gp.w + "px";
                ghost.style.height = gp.h + "px";
              }
            }
            function cleanup() {
              document.removeEventListener("pointermove", move);
              document.removeEventListener("pointerup", up);
              document.removeEventListener("pointercancel", cancel);
              window.removeEventListener("blur", cleanup);
              document.body.style.cursor = "";
              if (ghost) { ghost.remove(); ghost = null; }
              card.classList.remove("dsm-dragging");
            }
            function up(ev) { cleanup(); if (dragging) openPanel(s.id, { x: ev.clientX, y: ev.clientY }); }
            function cancel() { cleanup(); }
            document.addEventListener("pointermove", move);
            document.addEventListener("pointerup", up);
            document.addEventListener("pointercancel", cancel);
            window.addEventListener("blur", cleanup);
          } },
          React.createElement("div", { className: "dsm-scard-top" },
            React.createElement(Dot, { state: subagentDot(s, getErrored(s.id)) }),
            React.createElement("span", { className: "title" }, s.title),
            mode ? React.createElement("span", { className: "dsm-pill" + (mode === "continuable" ? " cont" : "") }, mode === "continuable" ? T("mode.continuable") : T("mode.oneshot")) : null,
            s.hasChildren ? React.createElement("span", { style: { display: "inline-flex", color: "var(--dsw-alias-label-tertiary, #9a9a9a)", flex: "none" } }, React.createElement(IconBranchOutline16, { size: 14 })) : null,
            React.createElement("button", { className: "dsm-iconbtn", onClick: function () { openPanel(s.id); }, title: T("open.popup") }, React.createElement(IconRightUpOutline16, { size: 14 }))
          ),
          React.createElement("div", { className: "dsm-scard-stats" },
            pct !== null ? React.createElement("span", { className: "dsm-ctx" + (pct > 85 ? " warn" : "") },
              React.createElement("span", { className: "bar" }, React.createElement("span", { className: "fill", style: { width: Math.round(pct) + "%" } })),
              React.createElement("span", { className: "pct" }, Math.round(pct) + "%")
            ) : null,
            total !== null ? React.createElement("span", { className: "dsm-stat" }, React.createElement("b", null, formatTokens(total)), " tok") : null,
            tps !== null ? React.createElement("span", { className: "dsm-stat" }, React.createElement("b", null, Math.round(tps * 10) / 10), " t/s") : null,
            stats ? React.createElement("span", { className: "dsm-stat" }, React.createElement("b", null, String(stats.steps)), " steps") : null
          ),
          React.createElement("div", { className: "dsm-scard-meta" },
            model && model.model ? React.createElement("span", { className: "model" }, model.model) : null,
            model && model.model ? React.createElement("span", { className: "sep" }) : null,
            dur !== null ? React.createElement("span", null, formatDuration(dur)) : null,
            dur !== null ? React.createElement("span", { className: "sep" }) : null,
            React.createElement("span", { title: formatClock(s.updatedAt) }, formatAgo(s.updatedAt, now)),
            React.createElement("span", { className: "sep" }),
            React.createElement("span", { className: "sid" }, s.id)
          )
        );
      }

      if (subagents.length === 0) {
        return React.createElement("div", { style: { padding: 16, display: "flex", alignItems: "center", justifyContent: "center", height: "100%", overflow: "auto" } },
          React.createElement("div", { style: { display: "flex", flexDirection: "column", alignItems: "center", gap: 12, textAlign: "center" } },
            React.createElement("div", { style: { width: 64, height: 64, borderRadius: "50%", display: "flex", alignItems: "center", justifyContent: "center", color: "var(--dsw-alias-label-caption, #7a7a7a)", background: "var(--dsw-alias-bg-layer-1, #1c1d21)", border: "1px solid var(--dsw-alias-border-l2, #36373b)" } }, React.createElement(IconBranchOutline16, { size: 28 })),
            React.createElement("div", { style: { fontSize: 14, lineHeight: "22px", fontWeight: 500, color: "var(--dsw-alias-label-secondary, #b8b8b8)" } }, T("empty.title")),
            React.createElement("div", { style: { fontSize: 13, lineHeight: "18px", color: "var(--dsw-alias-label-tertiary, #9a9a9a)" } }, T("empty.subtitle"))
          )
        );
      }
      return React.createElement("div", { style: { padding: 16, display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(250px, 1fr))", gap: 8, overflow: "auto", height: "100%", alignContent: "start" } },
        React.createElement(React.Fragment, null,
              activeList.length ? groupHead("group.active", activeList.length) : null,
              activeList.map(renderCard),
              inactiveList.length ? groupHead("group.inactive", inactiveList.length) : null,
              inactiveList.map(renderCard)
            )
      );
    }

    var inject = ["sessions", "connection", "slots", "locale"];

    function apply(ctx) {
      var api = ctx.connection.api;
      localeCtx = ctx.locale;
      ctx.locale.register("subagent-max", "zh", ZH);
      ctx.locale.register("subagent-max", "en", EN);
      T = ctx.locale.bind("subagent-max");
      var store = createStore(api, ctx.sessions);

      var controller = null;
      var retryTimer = null;
      var token = 0;
      var backoffMs = BASE_BACKOFF_MS;

      function scheduleRetry(myToken) {
        if (myToken !== token) return;
        backoffMs = Math.min(backoffMs * 2, MAX_BACKOFF_MS);
        retryTimer = setTimeout(function () { if (myToken === token) pump(); }, backoffMs);
      }

      async function runStream(myToken, controller) {
        var signal = controller.signal;
        var muxStream, hostStream;
        try {
          muxStream = api.events.mux({}, signal);
          hostStream = api.events.host({}, signal);
        } catch (e) { scheduleRetry(myToken); return; }
        var ended = false;
        function finish() { if (!ended) { ended = true; try { controller.abort(); } catch (e) {} } }
        async function pumpMux() {
          try {
            for await (var envelope of muxStream) {
              var frame = envelope && envelope.payload;
              if (frame && frame.type === "session/event") store.handleEvent(frame.sessionId, frame.event, frame.view);
            }
          } catch (e) {}
          finish();
        }
        async function pumpHost() {
          try {
            for await (var envelope of hostStream) {
              var frame = envelope && envelope.payload;
              if (frame) store.handleHostFrame(frame);
            }
          } catch (e) {}
          finish();
        }
        await Promise.all([pumpMux(), pumpHost()]);
        scheduleRetry(myToken);
      }

      function pump() {
        var myToken = ++token;
        if (retryTimer !== null) { clearTimeout(retryTimer); retryTimer = null; }
        if (controller !== null) { try { controller.abort(); } catch (e) {} }
        controller = new AbortController();
        runStream(myToken, controller);
      }

      ctx.on("connection/reset", function () { backoffMs = BASE_BACKOFF_MS; pump(); });

      ctx.effect(function () {
        pump();
        return function () {
          token++;
          if (retryTimer !== null) clearTimeout(retryTimer);
          if (controller !== null) { try { controller.abort(); } catch (e) {} }
          store.dispose();
        };
      }, "dsh-subagent-max: mux stream");

      ctx.slots.inject("shell.overlay", function () {
        return ctx.slots.register({
          name: "shell.overlay",
          id: "subagent-max",
          order: 1000
        }, function (slotProps) {
          return React.createElement(ShellOverlay, { store: store, useSessions: slotProps.useSessions });
        });
      });

      ctx.slots.inject("conversation.view", function () {
        return ctx.slots.register({
          name: "conversation.view",
          id: "subagent-max",
          order: 20,
          label: function () { return T("tab.title"); }
        }, function (slotProps) {
          return React.createElement(SubagentsTab, {
            sessionId: slotProps.sessionId,
            useSessions: slotProps.useSessions,
            openPanel: function (id, pos) { store.openPanel(id, pos); },
            getModel: function (id) { return store.getModel(id); },
            ensureModel: function (id) { store.ensureModel(id); },
            getErrored: function (id) { return store.getErrored(id); },
            seedLastActivity: function (id) { store.seedLastActivity(id); },
            subscribe: function (fn) { return store.subscribe(fn); }
          });
        });
      });
    }

    return { apply: apply, inject: inject };
  }
});