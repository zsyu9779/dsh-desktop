package main

import (
	"net/http"
	"testing"
)

func authedReq(t *testing.T, base, jwt, path string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("GET", base+path, nil)
	req.AddCookie(&http.Cookie{Name: remoteCookieName, Value: jwt})
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatalf("request %s: %v", path, err)
	}
	defer resp.Body.Close()
	return resp
}

func TestPrivilegedMethodDefaultForbidden(t *testing.T) {
	m, base := newTestRemote(t)
	jwt := pairJWT(t, m, base)

	resp := authedReq(t, base, jwt, "/api/settings.update")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("privileged default status = %d, want 403", resp.StatusCode)
	}

	resp2 := authedReq(t, base, jwt, "/api/session.list")
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("non-privileged status = %d, want 200", resp2.StatusCode)
	}
}

func TestPrivilegedMethodAllowedAfterGrant(t *testing.T) {
	m, base := newTestRemote(t)
	jwt := pairJWT(t, m, base)
	m.setAllowPrivileged(true)

	resp := authedReq(t, base, jwt, "/api/settings.update")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("privileged after grant status = %d, want 200", resp.StatusCode)
	}
}
