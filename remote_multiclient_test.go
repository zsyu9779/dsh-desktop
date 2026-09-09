package main

import (
	"bufio"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// sseUpstream mimics dsh's /api/remote.mux: every connected client is
// registered and receives every broadcast event in real time.
type sseUpstream struct {
	mu      sync.Mutex
	clients map[chan string]struct{}
}

func (u *sseUpstream) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/remote.mux" {
			u.serveSSE(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><head></head><body>ok</body></html>"))
	}
}

func (u *sseUpstream) serveSSE(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "no flusher", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ch := make(chan string, 64)
	u.mu.Lock()
	if u.clients == nil {
		u.clients = map[chan string]struct{}{}
	}
	u.clients[ch] = struct{}{}
	u.mu.Unlock()
	defer func() {
		u.mu.Lock()
		delete(u.clients, ch)
		u.mu.Unlock()
	}()

	w.WriteHeader(http.StatusOK)
	fl.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			if _, err := fmt.Fprintf(w, "data: %s\n\n", msg); err != nil {
				return
			}
			fl.Flush()
		}
	}
}

func (u *sseUpstream) broadcast(msg string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	for ch := range u.clients {
		select {
		case ch <- msg:
		default:
		}
	}
}

func (u *sseUpstream) count() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.clients)
}

// sseReader consumes an SSE stream and reports data payloads.
type sseReader struct {
	sc    *bufio.Scanner
	ch    chan string
	start chan struct{}
}

func newSSEReader(t *testing.T, resp *http.Response) *sseReader {
	t.Helper()
	rd := &sseReader{
		sc:    bufio.NewScanner(resp.Body),
		ch:    make(chan string, 16),
		start: make(chan struct{}),
	}
	go func() {
		close(rd.start)
		for rd.sc.Scan() {
			line := rd.sc.Text()
			if strings.HasPrefix(line, "data: ") {
				rd.ch <- strings.TrimPrefix(line, "data: ")
			}
		}
		close(rd.ch)
	}()
	return rd
}

// TestReverseProxySSEBroadcastToMultipleClients asserts that two independent
// SSE consumers connected THROUGH the reverse proxy each receive the same
// upstream broadcast (the cross-device real-time sync invariant).
func TestReverseProxySSEBroadcastToMultipleClients(t *testing.T) {
	up := &sseUpstream{}
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

	// Pair once to obtain a valid device JWT cookie.
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

	open := func() *sseReader {
		req, _ := http.NewRequest("GET", base+"/api/remote.mux", nil)
		req.AddCookie(&http.Cookie{Name: remoteCookieName, Value: jwt})
		r, err := client.Do(req)
		if err != nil {
			t.Fatalf("open SSE: %v", err)
		}
		if r.StatusCode != http.StatusOK {
			r.Body.Close()
			t.Fatalf("open SSE status = %d, want 200", r.StatusCode)
		}
		return newSSEReader(t, r)
	}

	a := open()
	b := open()

	// Wait until both connections are registered on the upstream.
	deadline := time.Now().Add(5 * time.Second)
	for up.count() < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if up.count() < 2 {
		t.Fatalf("only %d upstream SSE clients registered, want 2", up.count())
	}

	up.broadcast("hello-both")

	got := func(rd *sseReader) bool {
		select {
		case m := <-rd.ch:
			return m == "hello-both"
		case <-time.After(2 * time.Second):
			return false
		}
	}
	if !got(a) {
		t.Fatal("first SSE client did not receive broadcast")
	}
	if !got(b) {
		t.Fatal("second SSE client did not receive broadcast")
	}
}
