package main

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestWaitReadyDoesNotAcceptUnauthorizedBareURL(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "dsh web authentication required", http.StatusUnauthorized)
	})}
	t.Cleanup(func() { _ = server.Close() })
	go func() { _ = server.Serve(listener) }()

	port := listener.Addr().(*net.TCPAddr).Port
	m := newDSHManager(&App{})
	m.mu.Lock()
	m.port = port
	m.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ready := m.waitReady(ctx, port)

	select {
	case got := <-ready:
		t.Fatalf("waitReady returned %v before DSH advertised its authenticated URL", got)
	case <-time.After(750 * time.Millisecond):
	}

	m.mu.Lock()
	m.proxyURL = "http://capability.localhost:" + strconv.Itoa(port) + "/"
	m.browserURL = "http://127.0.0.1:" + strconv.Itoa(port) + "/?dshcap=capability"
	m.mu.Unlock()

	select {
	case got := <-ready:
		if !got {
			t.Fatal("waitReady returned false after DSH advertised its URL")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waitReady did not report ready after DSH advertised its URL")
	}
}

func TestParseAdvertisedURL(t *testing.T) {
	line := "dsh web: http://127.0.0.1:3080/?token=secret (LAN: http://example.invalid)"
	parsed, ok := parseAdvertisedURL(line, 3080)
	if !ok {
		t.Fatal("parseAdvertisedURL rejected the local DSH URL")
	}
	if got := parsed.String(); got != "http://127.0.0.1:3080/?token=secret" {
		t.Fatalf("captured URL = %q", got)
	}
	if got := redactAdvertisedURL(line); got != "dsh web: http://127.0.0.1:3080/?<REDACTED> (LAN: http://example.invalid)" {
		t.Fatalf("redacted log line = %q", got)
	}
}

func TestParseAdvertisedURLRejectsUnexpectedOrigin(t *testing.T) {
	for _, line := range []string{
		"dsh web: https://127.0.0.1:3080/?token=secret",
		"dsh web: http://localhost:3080/?token=secret",
		"dsh web: http://127.0.0.1:9999/?token=secret",
		"dsh web: http://user@127.0.0.1:3080/?token=secret",
	} {
		if _, ok := parseAdvertisedURL(line, 3080); ok {
			t.Fatalf("parseAdvertisedURL accepted %q", line)
		}
	}
}

// The frontend loads status.URL in an iframe, so every reader of the status
// snapshot must publish the browser-facing URL. A credentialed URL would be
// refused by the WebView (Chromium blocks it outright, WebKit strips the
// header), which is exactly how the shipped 0.1.2 adaptation failed.
func TestStatusSnapshotPublishesBrowserURL(t *testing.T) {
	const (
		internal = "http://dsh:cap@127.0.0.1:1234/"
		browser  = "http://127.0.0.1:1234/?dshcap=cap"
	)
	m := newDSHManager(&App{})
	m.mu.Lock()
	m.proxyURL = internal
	m.browserURL = browser
	m.port = 1234
	m.mu.Unlock()

	if got := m.current().URL; got != browser {
		t.Fatalf("current().URL = %q, want the browser URL", got)
	}
	if got := m.internalURL(); got != internal {
		t.Fatalf("internalURL() = %q, want the credentialed Go-facing URL", got)
	}

	m.setStatus("ready", "已就绪")
	after := m.current()
	if after.URL != browser || after.State != "ready" {
		t.Fatalf("after setStatus status = %+v, want the browser URL and ready state", after)
	}
}
