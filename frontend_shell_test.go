package main

import (
	"os"
	"strings"
	"testing"
)

func TestRemoteControlLivesOutsideHiddenSplash(t *testing.T) {
	markup, err := os.ReadFile("frontend/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(markup)
	remoteAt := strings.Index(html, `id="remote"`)
	splashAt := strings.Index(html, `id="splash"`)
	if remoteAt < 0 || splashAt < 0 || remoteAt > splashAt {
		t.Fatal("remote control must be a sibling before the splash so it remains visible over DSH")
	}

	styles, err := os.ReadFile("frontend/src/style.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(styles)
	if !strings.Contains(css, ".remote {") || !strings.Contains(css, "z-index: 20") {
		t.Fatal("remote control must render above the z-index 10 harness iframe")
	}
}
