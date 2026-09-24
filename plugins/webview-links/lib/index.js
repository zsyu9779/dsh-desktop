/**
 * dsh-webview-links — let the external links in a session reach the system
 * browser instead of dying on a WebView that cannot open a new window.
 *
 * Upstream renders every external Markdown link as
 *
 *   <a href="https://…" target="_blank" rel="noopener noreferrer">
 *
 * A real browser answers `_blank` with a new tab. This app's WebView does not:
 * it is WKWebView under Wails v2, whose macOS side declares a `WKUIDelegate`
 * but implements no `webView:createWebViewWithConfiguration:`, so the new-window
 * request has no delegate to answer it and the click is dropped — no tab, no
 * navigation, no error. Wails exposes no shell-side navigation hook, and the DSH
 * page sits on its own loopback origin that the shell document cannot script.
 *
 * So this plugin contributes one head script to the DSH page. It converts a
 * `_blank` click on an http(s) link into a `postMessage` to the parent frame —
 * the shell page — which forwards it to the `OpenExternalURL` binding and Go
 * hands it to `runtime.BrowserOpenURL`, the same path the shell's own
 * “在浏览器打开” button already uses.
 *
 * The script is inert wherever the page is not embedded. DSH opened directly in
 * a browser (the shell's browser fallback, or the phone remote surface) is a
 * top-level document, `window.parent === window`, and its native `_blank`
 * behavior is left untouched.
 */

/** Stable Cordis plugin name. */
export const name = "webview-links";

/** The index-injection seam this plugin contributes to. */
export const inject = ["webServer"];

/**
 * The window message the shell's frontend listens for. Kept in step with
 * `frontend/src/main.js` by `TestWebviewLinksProtocolMatchesShell`.
 */
export const OPEN_EXTERNAL_MESSAGE = "dsh-desktop:open-external";

/**
 * Head script installing one document-level click handler. It runs before the
 * client module graph, and the handler itself rides the bubble phase so a DSH
 * handler that already claimed the click (defaultPrevented) is left alone.
 */
const OPEN_EXTERNAL_SCRIPT = [
  "(function () {",
  "  if (window.parent === window) return;", // top-level page: native _blank works
  "  function forward(event) {",
  "    if (event.defaultPrevented) return;",
  "    if (event.button !== 0 && event.button !== 1) return;", // left or middle click
  "    var node = event.target;",
  "    var anchor = node && node.closest ? node.closest('a[target]') : null;",
  "    if (!anchor) return;",
  "    if ((anchor.getAttribute('target') || '').toLowerCase() !== '_blank') return;",
  "    var href = anchor.href || '';",
  "    if (!/^https?:/i.test(href)) return;",
  "    event.preventDefault();",
  "    window.parent.postMessage({ type: " + JSON.stringify(OPEN_EXTERNAL_MESSAGE) + ", url: href }, '*');",
  "  }",
  "  document.addEventListener('click', forward);",
  "  document.addEventListener('auxclick', forward);",
  "})();",
].join("\n");

/** Contribute the forwarder as a head script on every index render. */
export function apply(ctx) {
  ctx.on("webserver/index-inject", (table) => {
    table.push({ kind: "script", placement: "head", text: OPEN_EXTERNAL_SCRIPT });
  });
}
