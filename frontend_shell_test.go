package main

import (
	"os"
	"strings"
	"testing"
)

func TestRemoteControlMovesIntoPersistentPanel(t *testing.T) {
	markup, err := os.ReadFile("frontend/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(markup)
	splashAt := strings.Index(html, `id="splash"`)
	triggerAt := strings.Index(html, `id="btn-remote-panel"`)
	if splashAt < 0 || triggerAt < 0 || triggerAt < splashAt {
		t.Fatal("remote panel trigger must remain outside the hidden splash")
	}

	script, err := os.ReadFile("frontend/src/main.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(script), "remotePanelContent.append(accountRemote, remoteEl)") {
		t.Fatal("remote controls must be moved into the persistent panel before the splash is hidden")
	}
}

// The link bridge is a protocol between two files that cannot import each
// other: the script injected into DSH posts a message type, and the shell
// frontend has to listen for exactly that type and forward it to the Go
// binding. A drift between the halves silently re-breaks every external link,
// so pin both ends here.
func TestWebviewLinksProtocolMatchesShell(t *testing.T) {
	const message = "dsh-desktop:open-external"

	plugin, err := os.ReadFile("plugins/webview-links/lib/index.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(plugin), message) {
		t.Fatalf("webview-links does not post the %q message", message)
	}
	// DSH >= 0.2 opens an external link from its own onClick: preventDefault()
	// then window.open(). Wails' WKWebView has no new-window delegate, so that
	// call lands nowhere; the plugin has to redirect window.open itself or the
	// bridge silently degrades to dead links again.
	if !strings.Contains(string(plugin), "window.open = function") {
		t.Fatal("webview-links must override window.open for the DSH >= 0.2 link path")
	}
	// The bubble-phase fallback still covers clients that render a plain
	// target=_blank anchor with no onClick of their own.
	for _, hook := range []string{"'click'", "'auxclick'"} {
		if !strings.Contains(string(plugin), hook) {
			t.Fatalf("webview-links lost its %s fallback listener", hook)
		}
	}

	script, err := os.ReadFile("frontend/src/main.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(script), message) {
		t.Fatalf("frontend/src/main.js does not listen for the %q message", message)
	}
	if !strings.Contains(string(script), "App.OpenExternalURL(") {
		t.Fatal("frontend/src/main.js must forward the message to App.OpenExternalURL")
	}
}
