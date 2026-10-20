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
 * So this plugin contributes one head script to the DSH page, which forwards two
 * things to the parent frame — the shell page — over `postMessage`:
 *
 *   1. `window.open(http(s)…)`. DSH >= 0.2 answers a Markdown link in its own
 *      `onClick` (`preventDefault()`, then `window.open` for the "browser"
 *      link-opening mode; the "sidebar" mode never leaves the app). Overriding
 *      `window.open` redirects exactly that broken call and leaves the sidebar
 *      mode — which never reaches it — untouched.
 *   2. an unclaimed `target="_blank"` http(s) anchor click, for clients that
 *      render links without an `onClick` of their own. A handler that already
 *      claimed the click (`defaultPrevented`) stays authoritative.
 *
 * The shell forwards the message to the `OpenExternalURL` binding and Go hands
 * it to `runtime.BrowserOpenURL`, the same path the shell's own “在浏览器打开”
 * button already uses.
 *
 * The script is inert wherever the page is not embedded. DSH opened directly in
 * a browser (the shell's browser fallback, or the phone remote surface) is a
 * top-level document, `window.parent === window`, and its native `_blank` and
 * `window.open` behavior is left untouched.
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
 * Head script installing the `window.open` override plus one document-level
 * click/auxclick fallback. It runs before the client module graph, so the
 * override is in place before any link can be clicked.
 */
const OPEN_EXTERNAL_SCRIPT = [
  "(function () {",
  "  if (window.parent === window) return;", // top-level page: native behavior works
  "  function forward(url) {",
  "    window.parent.postMessage({ type: " + JSON.stringify(OPEN_EXTERNAL_MESSAGE) + ", url: url }, '*');",
  "  }",
  "  function absolute(raw) {",
  "    try { return new URL(String(raw), window.location.href); } catch (e) { return null; }",
  "  }",
  "  // DSH >= 0.2 claims external-link clicks itself and opens the 'browser'",
  "  // link-opening mode through window.open, which this WebView cannot answer.",
  "  // Redirect that call here; the 'sidebar' mode never calls it, so an in-app",
  "  // browser tab keeps working.",
  "  var nativeOpen = window.open;",
  "  window.open = function (url) {",
  "    var target = absolute(url);",
  "    if (target && /^https?:$/i.test(target.protocol)) { forward(target.href); return null; }",
  "    return nativeOpen.apply(window, arguments);",
  "  };",
  "  // Fallback for a _blank http(s) anchor nothing else claims: older clients",
  "  // render links without an onClick, and their default _blank navigation is",
  "  // just as dead in this WebView. An in-app handler that already claimed the",
  "  // click (defaultPrevented) is left alone, so the sidebar mode is untouched.",
  "  function onActivate(event) {",
  "    if (event.defaultPrevented) return;",
  "    if (event.button !== 0 && event.button !== 1) return;", // left or middle click
  "    var node = event.target;",
  "    var anchor = node && node.closest ? node.closest('a[target]') : null;",
  "    if (!anchor) return;",
  "    if ((anchor.getAttribute('target') || '').toLowerCase() !== '_blank') return;",
  "    var href = anchor.href || '';",
  "    if (!/^https?:/i.test(href)) return;",
  "    event.preventDefault();",
  "    forward(href);",
  "  }",
  "  document.addEventListener('click', onActivate);",
  "  document.addEventListener('auxclick', onActivate);",
  "})();",
].join("\n");

/** Contribute the forwarder as a head script on every index render. */
export function apply(ctx) {
  ctx.on("webserver/index-inject", (table) => {
    table.push({ kind: "script", placement: "head", text: OPEN_EXTERNAL_SCRIPT });
  });
}
