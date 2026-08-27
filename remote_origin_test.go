package main

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestReverseProxyWSWithBrowserOrigin mimics the BROWSER's WebSocket handshake:
// it sends an Origin header (which the reverse proxy's Director rewrites to the
// loopback target) plus the JWT cookie. If this fails, the phone's real-time
// stream is broken by the Origin rewrite.
func TestReverseProxyWSWithBrowserOrigin(t *testing.T) {
	t.Setenv(stateDirEnv, t.TempDir())
	m := newRemoteManager(nil)
	if _, err := m.enable("http://127.0.0.1:49873"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	t.Cleanup(m.disable)

	base := fmt.Sprintf("https://127.0.0.1:%d", m.status().Port)
	client := noRedirectClient()
	code := m.status().PairingCode
	resp, err := client.Get(base + "/?pair=" + code)
	if err != nil {
		t.Fatalf("pair request: %v", err)
	}
	resp.Body.Close()
	var jwt string
	for _, c := range resp.Cookies() {
		if c.Name == remoteCookieName {
			jwt = c.Value
		}
	}
	if jwt == "" {
		t.Fatal("no dsh_remote cookie on pairing")
	}

	wsURL := "wss" + base[len("https"):] + "/api/events.mux"
	dialer := websocket.Dialer{
		TLSClientConfig: client.Transport.(*http.Transport).TLSClientConfig,
	}
	header := http.Header{}
	header.Set("Cookie", remoteCookieName+"="+jwt)
	header.Set("Origin", base) // mimic the browser: Origin = proxy origin

	c, resp2, err := dialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("dial WS with Origin: %v (resp=%v)", err, resp2)
	}
	defer c.Close()

	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, msg, err := c.ReadMessage()
	if err != nil {
		t.Fatalf("no frame through proxy with Origin: %v", err)
	}
	t.Logf("received with Origin: %.200s", string(msg))
}
