package main

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newTestRemote points a remoteManager at a fake upstream (the single test seam)
// and returns a ready-to-use loopback base URL for driving requests like a phone.
func newTestRemote(t *testing.T) (*remoteManager, string) {
	t.Helper()
	t.Setenv(stateDirEnv, t.TempDir())

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><head></head><body>ok</body></html>"))
	}))
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

func noRedirectClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func TestPairingIssuesJWTCookie(t *testing.T) {
	m, base := newTestRemote(t)
	code := m.status().PairingCode
	if code == "" {
		t.Fatal("no pairing code")
	}

	client := noRedirectClient()
	resp, err := client.Get(base + "/?pair=" + code)
	if err != nil {
		t.Fatalf("pair request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("pair status = %d, want 302", resp.StatusCode)
	}

	var jwt string
	for _, c := range resp.Cookies() {
		if c.Name == remoteCookieName {
			jwt = c.Value
		}
	}
	if jwt == "" {
		t.Fatal("no dsh_remote cookie on pairing")
	}

	req, _ := http.NewRequest("GET", base+"/", nil)
	req.AddCookie(&http.Cookie{Name: remoteCookieName, Value: jwt})
	resp2, err := client.Do(req)
	if err != nil {
		t.Fatalf("authed request: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("authed status = %d, want 200", resp2.StatusCode)
	}
}

func TestPairingWrongCodeForbidden(t *testing.T) {
	m, base := newTestRemote(t)
	resp, err := noRedirectClient().Get(base + "/?pair=wrongcode")
	if err != nil {
		t.Fatalf("pair request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong-code status = %d, want 403", resp.StatusCode)
	}
	if m.status().PairingCode == "" {
		t.Fatal("wrong attempt should not burn the pairing code")
	}
}

func TestPairingCodeSingleUse(t *testing.T) {
	m, base := newTestRemote(t)
	code := m.status().PairingCode
	client := noRedirectClient()

	resp, err := client.Get(base + "/?pair=" + code)
	if err != nil {
		t.Fatalf("first pair: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("first pair status = %d, want 302", resp.StatusCode)
	}

	resp2, err := client.Get(base + "/?pair=" + code)
	if err != nil {
		t.Fatalf("second pair: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusForbidden {
		t.Fatalf("second pair status = %d, want 403 (single-use)", resp2.StatusCode)
	}
}

func TestPairingCodeExpiredForbidden(t *testing.T) {
	m, base := newTestRemote(t)
	code := m.status().PairingCode
	m.mu.Lock()
	m.pairingExpiry = time.Now().Add(-time.Minute)
	m.mu.Unlock()

	resp, err := noRedirectClient().Get(base + "/?pair=" + code)
	if err != nil {
		t.Fatalf("pair request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expired-code status = %d, want 403", resp.StatusCode)
	}
}

func TestMissingJWTCookieForbidden(t *testing.T) {
	_, base := newTestRemote(t)
	resp, err := noRedirectClient().Get(base + "/")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("no-cookie status = %d, want 403", resp.StatusCode)
	}
}

func TestInvalidJWTForbidden(t *testing.T) {
	_, base := newTestRemote(t)
	req, _ := http.NewRequest("GET", base+"/", nil)
	req.AddCookie(&http.Cookie{Name: remoteCookieName, Value: "garbage.token.here"})
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("invalid-JWT status = %d, want 403", resp.StatusCode)
	}
}
