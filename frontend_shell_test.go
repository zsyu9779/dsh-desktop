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
