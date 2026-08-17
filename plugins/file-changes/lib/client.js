/**
 * dsh-file-changes — browser half.
 *
 * Registers into the chat view's `conversation.chat.turnTail` chain and
 * renders a per-turn file-change panel under the closing message:
 *   - one row per file the turn created or modified (native mode: derived
 *     from the mutation tools' presentation views on the wire; Code Mode:
 *     fetched from the host half's nested-dispatch records),
 *   - a "view diff" button opening a modal with the applied hunks,
 *   - a "reveal" button locating the file in the OS file manager.
 */
window.__ModuleLoader__.load({
	id: "dsh-file-changes",
	factory: (require) => {
		var module = { exports: {} };
		var exports = module.exports;
		Object.defineProperty(exports, Symbol.toStringTag, { value: "Module" });
		let react = require("react");
		let react_jsx_runtime = require("react/jsx-runtime");
		let runtime_client = require("@deepseek-ai/dsh-client-runtime/client");
		let primitives = require("@deepseek-ai/dsh-client-ui-primitives");

		// ── locale ────────────────────────────────────────────────────────────

		const NS = "fileChanges";
		const zh = {
			"panel.label": "文件变更",
			"panel.created": "新增",
			"panel.modified": "修改",
			"panel.open": "打开 {name}",
			"panel.viewDiff": "查看修改",
			"panel.reveal": "定位",
			"panel.close": "关闭",
			"panel.diffTitle": "变更详情 · {name}",
			"panel.noDiff": "该工具没有提供可渲染的差异内容。"
		};
		const en = {
			"panel.label": "File changes",
			"panel.created": "Created",
			"panel.modified": "Modified",
			"panel.open": "Open {name}",
			"panel.viewDiff": "View diff",
			"panel.reveal": "Reveal",
			"panel.close": "Close",
			"panel.diffTitle": "Changes · {name}",
			"panel.noDiff": "This tool provided no renderable diff content."
		};

		// ── native-mode turn accumulator ─────────────────────────────────────
		// Mirrors the deliverables definition but keeps the applied diff hunks.

		function collectFromViews(resultView, callView, seq) {
			if (resultView !== null && resultView.card === "diff" && Array.isArray(resultView.diffs)) {
				return resultView.diffs.map((hunk) => ({
					seq,
					path: hunk.path,
					status: hunk.oldText === null || hunk.oldText === undefined ? "created" : "modified",
					hunks: [hunk]
				}));
			}
			if (callView !== null && callView.card === "diff" && Array.isArray(callView.diffs)) {
				return callView.diffs.map((hunk) => ({
					seq,
					path: hunk.path,
					status: hunk.oldText === null || hunk.oldText === undefined ? "created" : "modified",
					hunks: [hunk]
				}));
			}
			const isGenericEdit = (view) => view !== null && view.card === "generic" && view.kind === "edit";
			const generic = isGenericEdit(resultView) ? resultView : isGenericEdit(callView) ? callView : null;
			if (generic !== null) {
				return (generic.locations ?? []).map((location) => ({
					seq,
					path: location.path,
					status: "modified",
					hunks: []
				}));
			}
			return [];
		}

		function upsert(changes, addition) {
			const index = changes.findIndex((change) => change.path === addition.path);
			if (index === -1) return [...changes, addition];
			const existing = changes[index];
			const merged = {
				...existing,
				status: existing.status === "created" || addition.status === "created" ? "created" : "modified",
				hunks: [...existing.hunks, ...addition.hunks],
				seq: addition.seq
			};
			const next = changes.slice();
			next[index] = merged;
			return next;
		}

		/** Turn-local mutation accumulator publishing no view Node. */
		const fileChangesDefinition = {
			kind: "fileChanges",
			match: (event) => {
				if (event.type === "turn/start") return { id: String(event.data.turn), role: "start" };
				if (event.type === "tool/call") return { id: String(event.data.turn), role: "update" };
				if (event.type === "tool/result" && runtime_client.isAppendSurfaceEvent(event)) {
					return { id: String(event.data.turn), role: "update" };
				}
				return null;
			},
			start: (_context, match) => {
				if (match.event.type !== "turn/start") throw new Error("fileChanges start requires turn/start");
				return { turn: match.event.data.turn, calls: new Map(), changes: [] };
			},
			update: (context, match) => {
				if (match.event.type === "tool/call") {
					const calls = new Map(context.state.calls);
					calls.set(String(match.event.data.callId), match.view?.for === "call" ? match.view.view : null);
					return { ...context.state, calls };
				}
				if (match.event.type !== "tool/result") return context.state;
				if (match.event.data.message.content[0].isError === true) return context.state;
				const callId = String(match.event.data.message.source.callId);
				const callView = context.state.calls.get(callId) ?? null;
				const resultView = match.view?.for === "result" ? match.view.view : null;
				const additions = collectFromViews(resultView, callView, match.event.seq);
				if (additions.length === 0) return context.state;
				let changes = context.state.changes;
				for (const addition of additions) changes = upsert(changes, addition);
				return { ...context.state, changes };
			},
			buildLocationData: (context, scope) => scope !== "turn" || context.state === undefined ? null : {
				kind: "turn",
				turn: context.state.turn,
				key: "fileChanges",
				value: { changes: context.state.changes }
			}
		};

		/** Chain entry selector: the closing turn's own records, never declined. */
		function selectFileChanges(owner) {
			const data = owner.turn.data.get("fileChanges");
			const changes = data === undefined ? [] : data.changes.filter((change) => change.seq <= owner.seq);
			return changes;
		}

		// ── host-record loading (Code Mode) ──────────────────────────────────

		const sessionCache = new Map();
		function loadSessionChanges(sessionId) {
			const entry = sessionCache.get(sessionId);
			if (entry !== undefined && entry.inflight !== undefined) return entry.inflight;
			const promise = fetch("/api/file-changes/changes?sessionId=" + encodeURIComponent(sessionId))
				.then((response) => (response.ok ? response.json() : { changes: [] }))
				.then((payload) => {
					const fresh = { fetchedAt: Date.now(), changes: payload.changes ?? [], inflight: undefined };
					sessionCache.set(sessionId, fresh);
					return fresh;
				})
				.catch(() => {
					sessionCache.delete(sessionId);
					return { fetchedAt: Date.now(), changes: [] };
				});
			sessionCache.set(sessionId, { inflight: promise });
			return promise;
		}

		function revealFile(sessions, path) {
			const snapshot = sessions.list.getSnapshot();
			const current = snapshot.current;
			const cwd = current !== undefined ? snapshot.byId[current]?.cwd : undefined;
			const absolute = runtime_client.resolveWorkspacePath(cwd, path);
			fetch("/api/file-changes/reveal", {
				method: "POST",
				headers: { "content-type": "application/json" },
				body: JSON.stringify({ path: absolute })
			}).catch(() => {});
		}

		function basename(path) {
			const at = Math.max(path.lastIndexOf("/"), path.lastIndexOf("\\"));
			return at === -1 ? path : path.slice(at + 1);
		}

		// ── component ────────────────────────────────────────────────────────

		function FileChangesPanel({ matched: localChanges, turn, openFile, isLoopback, sessions, useHostDescription, t }) {
			const [hostChanges, setHostChanges] = react.useState(null);
			const [openPath, setOpenPath] = react.useState(null);
			const hostCanOpenPath = useHostDescription((description) => description?.canOpenPath === true);
			const canReveal = isLoopback && hostCanOpenPath;

			react.useEffect(() => {
				const snapshot = sessions.list.getSnapshot();
				const sessionId = snapshot.current;
				if (sessionId === undefined) {
					setHostChanges([]);
					return;
				}
				const until = turn.end?.time ?? Date.now();
				const cached = sessionCache.get(sessionId);
				if (cached !== undefined && cached.inflight === undefined && (cached.fetchedAt ?? 0) >= until) {
					setHostChanges(cached.changes);
					return;
				}
				let cancelled = false;
				loadSessionChanges(sessionId).then((entry) => {
					if (!cancelled) setHostChanges(entry.changes);
				});
				return () => {
					cancelled = true;
				};
			}, [turn, sessions]);

			if (hostChanges === null) {
				if (localChanges.length === 0) return null;
				return react_jsx_runtime.jsx(ChangesView, {
					changes: localChanges,
					openPath,
					setOpenPath,
					canReveal,
					openFile,
					sessions,
					t
				});
			}
			const since = turn.start?.time ?? 0;
			const until = turn.end?.time ?? Date.now();
			const fetched = hostChanges.filter((item) => item.time >= since && item.time <= until);
			const seen = new Set();
			const changes = [];
			for (const change of [...localChanges, ...fetched]) {
				if (seen.has(change.path)) continue;
				seen.add(change.path);
				changes.push(change);
			}
			if (changes.length === 0) return null;
			return react_jsx_runtime.jsx(ChangesView, {
				changes,
				openPath,
				setOpenPath,
				canReveal,
				openFile,
				sessions,
				t
			});
		}

		function ChangesView({ changes, openPath, setOpenPath, canReveal, openFile, sessions, t }) {
			const openChange = changes.find((change) => change.path === openPath) ?? null;
			const rows = changes.map((change) => {
				const hunks = change.hunks ?? [];
				return react_jsx_runtime.jsx("div", {
					className: css.item,
					children: [
						react_jsx_runtime.jsx("span", {
							className: change.status === "created" ? css.badgeCreated : css.badgeModified,
							children: change.status === "created" ? t("panel.created") : t("panel.modified")
						}, "badge"),
						react_jsx_runtime.jsx("button", {
							type: "button",
							className: css.file,
							title: change.path,
							onClick: () => openFile(change.path),
							children: basename(change.path)
						}, "file"),
						react_jsx_runtime.jsx("button", {
							type: "button",
							className: css.action,
							disabled: hunks.length === 0,
							onClick: () => setOpenPath(change.path),
							children: t("panel.viewDiff")
						}, "diff"),
						canReveal ? react_jsx_runtime.jsx("button", {
							type: "button",
							className: css.action,
							onClick: () => revealFile(sessions, change.path),
							children: t("panel.reveal")
						}, "reveal") : null
					]
				}, change.path);
			});
			return react_jsx_runtime.jsx("div", {
				className: css.root,
				children: [
					react_jsx_runtime.jsx("span", { className: css.label, children: t("panel.label") }, "label"),
					react_jsx_runtime.jsx("div", { className: css.list, children: rows }, "list"),
					openChange !== null ? react_jsx_runtime.jsx(primitives.Modal, {
						open: true,
						onClose: () => setOpenPath(null),
						title: t("panel.diffTitle", { name: openChange.path }),
						closeLabel: t("panel.close"),
						contentClassName: css.diffContent,
						children: (openChange.hunks ?? []).length > 0
							? react_jsx_runtime.jsx(primitives.DiffBlock, { diffs: openChange.hunks })
							: react_jsx_runtime.jsx("div", { className: css.noDiff, children: t("panel.noDiff") })
					}, "modal") : null
				]
			});
		}

		// ── styles ───────────────────────────────────────────────────────────

		const cssText = ".fc_root{grid-template-columns:max-content minmax(0,1fr);align-items:start;gap:6px 8px;margin-top:16px;font-size:13px;line-height:22px;display:grid}.fc_label{color:var(--dsw-alias-label-tertiary);grid-area:1/1}.fc_list{min-width:0;grid-area:1/2;display:flex;flex-wrap:wrap;gap:6px 8px;align-items:center}.fc_item{flex-wrap:nowrap;min-width:0;display:inline-flex;align-items:center;gap:6px;background:var(--dsw-alias-interactive-bg-hover);border-radius:6px;padding:2px 8px}.fc_badgeCreated,.fc_badgeModified{white-space:nowrap;font-size:11px;line-height:18px;border-radius:9px;padding:0 7px}.fc_badgeCreated{color:var(--dsw-alias-label-primary);background:var(--dsw-alias-interactive-bg-active)}.fc_badgeModified{color:var(--dsw-alias-label-secondary);background:transparent;box-shadow:inset 0 0 0 1px var(--dsw-alias-border-l2)}.fc_file{text-overflow:ellipsis;white-space:nowrap;max-width:280px;color:var(--dsw-alias-label-secondary);font:inherit;cursor:pointer;border:none;background:0 0;border-radius:4px;margin:0;padding:0 2px;overflow:hidden}.fc_file:hover{color:var(--dsw-alias-label-primary);text-decoration:underline}.fc_action{color:var(--dsw-alias-label-tertiary);font:inherit;font-size:12px;cursor:pointer;background:0 0;border:none;border-radius:4px;margin:0;padding:0 2px;line-height:20px}.fc_action:hover:not(:disabled){color:var(--dsw-alias-label-secondary);text-decoration:underline}.fc_action:disabled{opacity:.45;cursor:default}.fc_noDiff{color:var(--dsw-alias-label-secondary);padding:8px 0}.fc_diffContent{max-height:60vh;overflow:auto}";
		const tagId = "dsh-file-changes/FileChangesPanel.module.css";
		if (typeof document !== "undefined" && document.querySelector("style[data-plugin-css=" + JSON.stringify(tagId) + "]") === null) {
			const tag = document.createElement("style");
			tag.dataset.plugin = "dsh-file-changes";
			tag.dataset.pluginCss = tagId;
			tag.textContent = cssText;
			document.head.appendChild(tag);
		}
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
			diffContent: "fc_diffContent"
		};

		// ── plugin body ──────────────────────────────────────────────────────

		const inject = ["slots", "locale", "conversationEvents", "sessions", "connection"];
		function apply(ctx) {
			const connection = ctx.get("connection");
			const sessions = ctx.get("sessions");
			ctx.conversationEvents.register(fileChangesDefinition);
			ctx.effect(() => ctx.locale.register(NS, { zh, en }), "dsh-file-changes: dictionaries");
			ctx.slots.inject("conversation.chat.turnTail", () => ctx.slots.register({
				name: "conversation.chat.turnTail",
				select: selectFileChanges,
				locale: NS,
				inject: () => ({
					isLoopback: connection.isLoopback,
					sessions,
					hooks: { hostDescription: connection.hostDescription }
				})
			}, FileChangesPanel));
		}

		exports.FileChangesPanel = FileChangesPanel;
		exports.apply = apply;
		exports.inject = inject;
		exports.selectFileChanges = selectFileChanges;
		return module.exports;
	}
});
