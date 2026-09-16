/**
 * dsh-webview-compat — let upstream client bundles that assume a newer WebKit
 * run inside this app's system WebView.
 *
 * The right sidebar's document-preview bundle bundles a parser that
 * feature-detects the iterator-helpers global at module scope:
 *
 *   if (typeof Iterator.prototype.join !== "function") Iterator.prototype.join = ...
 *
 * Where `Iterator` does not exist — WebKit before Safari 18.4, which is macOS
 * before 15.4 — evaluating that line throws, and because it sits at module
 * scope the whole client module fails to import on every launch. This plugin
 * contributes one head script that installs the missing global before the
 * module graph evaluates, so those bundles load unchanged and the surface they
 * own stays available.
 *
 * The shim resolves the real `%IteratorPrototype%` rather than inventing one:
 * the method the library installs has to land on the prototype every iterator
 * already inherits from, or its own `join` fallback would attach to an object
 * no iterator uses. On an engine that already provides `Iterator`, the script
 * is a no-op.
 */

/** Stable Cordis plugin name. */
export const name = "webview-compat";

/** The index-injection seam this plugin contributes to. */
export const inject = ["webServer"];

/**
 * Head script defining `Iterator` when the WebView lacks it. It runs ahead of
 * every module-loader script, and never changes an engine that has the global.
 */
const ITERATOR_SHIM = [
  'if (typeof globalThis.Iterator === "undefined") {',
  '  var proto = Object.getPrototypeOf(Object.getPrototypeOf([][Symbol.iterator]()));',
  '  globalThis.Iterator = { prototype: proto || {} };',
  '}',
].join("\n");

/** Contribute the shim as a head script on every index render. */
export function apply(ctx) {
  ctx.on("webserver/index-inject", (table) => {
    table.push({ kind: "script", placement: "head", text: ITERATOR_SHIM });
  });
}
