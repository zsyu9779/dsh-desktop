# dsh-webview-links

Host-only DSH plugin shipped by dsh-desktop. It contributes one `<head>` script
that keeps the external links in a session clickable inside the desktop shell.

Upstream renders every external Markdown link as
`<a href="https://…" target="_blank" rel="noopener noreferrer">`. A real browser
answers `_blank` with a new tab. This app's WebView cannot: it is WKWebView
under Wails v2, whose macOS side declares a `WKUIDelegate` but implements no
`webView:createWebViewWithConfiguration:`, so the new-window request has no
delegate to answer it and the click disappears — no tab, no navigation, no
error. Wails exposes no shell-side navigation hook, and the DSH page runs on its
own loopback origin, out of reach of the shell's document.

The injected script bridges the two. It intercepts a `_blank` click on an
http(s) link, prevents the (dead) default action, and posts the URL to the
parent frame. That parent is the shell page; `frontend/src/main.js` validates
the message and calls the `OpenExternalURL` Go binding, which narrows the URL to
http(s) and calls `runtime.BrowserOpenURL` — the same path the shell's own
“在浏览器打开” button uses.

The script is inert wherever the page is not embedded. DSH served straight to a
browser (the shell's browser fallback, or the phone remote surface) is a
top-level document where `window.parent === window`, so its native `_blank`
behavior is left untouched.

No configuration, no file system access, and no effect outside the desktop
shell's WebView.
