package main

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newTestRemoteWith points a remoteManager at a custom fake upstream (the seam).
func newTestRemoteWith(t *testing.T, handler http.HandlerFunc) (*remoteManager, string) {
	t.Helper()
	t.Setenv(stateDirEnv, t.TempDir())

	upstream := httptest.NewServer(handler)
	t.Cleanup(upstream.Close)

	m := newRemoteManager(nil)
	if _, err := m.enable(upstream.URL); err != nil {
		t.Fatalf("enable: %v", err)
	}
	t.Cleanup(m.disable)

	addr := fmt.Sprintf("127.0.0.1:%d", m.status().Port)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return m, "http://" + addr
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("remote server did not come up on %s", addr)
	return m, ""
}

// pairJWT completes pairing and returns the device JWT cookie value.
func pairJWT(t *testing.T, m *remoteManager, base string) string {
	t.Helper()
	code := m.status().PairingCode
	if code == "" {
		t.Fatal("no pairing code")
	}
	resp, err := noRedirectClient().Get(base + "/?pair=" + code)
	if err != nil {
		t.Fatalf("pair: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("pair status = %d, want 302", resp.StatusCode)
	}
	for _, c := range resp.Cookies() {
		if c.Name == remoteCookieName {
			return c.Value
		}
	}
	t.Fatal("no dsh_remote cookie")
	return ""
}

func gzipHTML(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte("<html><head></head><body>compressed</body></html>")); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

func gunzipAll(t *testing.T, data []byte) []byte {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	defer zr.Close()
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("gunzip read: %v", err)
	}
	return out
}

func authedGet(t *testing.T, base, jwt string) (*http.Response, []byte) {
	t.Helper()
	req, _ := http.NewRequest("GET", base+"/", nil)
	req.AddCookie(&http.Cookie{Name: remoteCookieName, Value: jwt})
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatalf("authed get: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp, body
}

func TestModifyResponseIdentityHTML(t *testing.T) {
	m, base := newTestRemoteWith(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><head></head><body>hello</body></html>"))
	})
	jwt := pairJWT(t, m, base)
	resp, body := authedGet(t, base, jwt)

	if enc := resp.Header.Get("Content-Encoding"); enc != "" {
		t.Fatalf("identity response should have no Content-Encoding, got %q", enc)
	}
	if !bytes.Contains(body, []byte("hello")) {
		t.Fatal("identity HTML missing original content")
	}
	if !bytes.Contains(body, polyfillScript) {
		t.Fatal("identity HTML missing injected polyfill")
	}
}

func TestModifyResponseGzipHTML(t *testing.T) {
	m, base := newTestRemoteWith(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(gzipHTML(t))
	})
	jwt := pairJWT(t, m, base)
	resp, body := authedGet(t, base, jwt)

	var raw []byte
	if resp.Header.Get("Content-Encoding") == "gzip" {
		raw = gunzipAll(t, body)
	} else {
		raw = body
	}
	if !bytes.Contains(raw, []byte("compressed")) {
		t.Fatal("gzip HTML missing original content after decode")
	}
	if !bytes.Contains(raw, polyfillScript) {
		t.Fatal("gzip HTML missing injected polyfill after decode")
	}
}
