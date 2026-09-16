# dsh-webview-compat

Host-only DSH plugin shipped by dsh-desktop. It contributes one `<head>` script
that defines the `Iterator` global when the host WebView does not provide it
— WebKit before Safari 18.4, that is macOS before 15.4 — ahead of the client
module graph.

Upstream client bundles (the right sidebar's document preview among them)
feature-detect `Iterator.prototype.join` at module scope. Without the global
that line throws and the whole module fails to import, so the desktop shell
used to disable the module outright. Defining the global — pinned to the real
`%IteratorPrototype%`, so a method installed on it reaches every iterator —
keeps those bundles loading on older WebKit and changes nothing on newer
WebKit.

No configuration, no file system access, and no effect on the host when the
WebView already provides `Iterator`.
