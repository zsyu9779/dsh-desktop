package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// wsUpstream mimics dsh's /api/remote.mux WebSocket downlink: every connected
// client is registered and receives every broadcast frame.
type wsUpstream struct {
	mu       sync.Mutex
	clients  map[*websocket.Conn]struct{}
	upgrader websocket.Upgrader
}

func newWSUpstream() *wsUpstream {
	return &wsUpstream{
		clients: map[*websocket.Conn]struct{}{},
		upgrader: websocket.Upgrader{
			CheckOrigin: func(*http.Request) bool { return true },
		},
	}
}

func (u *wsUpstream) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/remote.mux" {
			u.serveWS(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><head></head><body>ok</body></html>"))
	}
}

func (u *wsUpstream) serveWS(w http.ResponseWriter, r *http.Request) {
	c, err := u.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	u.mu.Lock()
	u.clients[c] = struct{}{}
	u.mu.Unlock()
	defer func() {
		u.mu.Lock()
		delete(u.clients, c)
		u.mu.Unlock()
		_ = c.Close()
	}()
	for {
		if _, _, err := c.ReadMessage(); err != nil {
			return
		}
	}
}

func (u *wsUpstream) broadcast(msg string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	for c := range u.clients {
		_ = c.WriteMessage(websocket.TextMessage, []byte(msg))
	}
}

func (u *wsUpstream) count() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.clients)
}

// TestReverseProxyWSBroadcastToMultipleClients asserts two WebSocket consumers
// connected THROUGH the reverse proxy each receive the same upstream broadcast.
func TestReverseProxyWSBroadcastToMultipleClients(t *testing.T) {
	up := newWSUpstream()
	upstream := httptest.NewServer(up.handler())
	t.Cleanup(upstream.Close)

	t.Setenv(stateDirEnv, t.TempDir())
	m := newRemoteManager(nil)
	if _, err := m.enable(upstream.URL); err != nil {
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

	dialer := websocket.Dialer{
		TLSClientConfig: client.Transport.(*http.Transport).TLSClientConfig,
	}
	wsURL := "wss" + base[len("https"):] + "/api/remote.mux"

	open := func() *websocket.Conn {
		header := http.Header{}
		header.Set("Cookie", remoteCookieName+"="+jwt)
		c, _, err := dialer.Dial(wsURL, header)
		if err != nil {
			t.Fatalf("dial WS: %v", err)
		}
		return c
	}

	a := open()
	b := open()
	defer a.Close()
	defer b.Close()

	deadline := time.Now().Add(5 * time.Second)
	for up.count() < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if up.count() < 2 {
		t.Fatalf("only %d upstream WS clients registered, want 2", up.count())
	}

	up.broadcast("hello-both")

	recv := func(c *websocket.Conn) bool {
		c.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, msg, err := c.ReadMessage()
		return err == nil && string(msg) == "hello-both"
	}
	if !recv(a) {
		t.Fatal("first WS client did not receive broadcast")
	}
	if !recv(b) {
		t.Fatal("second WS client did not receive broadcast")
	}
}
