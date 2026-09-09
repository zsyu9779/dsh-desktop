package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEmbeddedProxyBridgesStrictUpstreamAuthentication(t *testing.T) {
	const cookieName = "dsh-auth-test"
	var mu sync.Mutex
	var gotHost, gotOrigin string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") == "one-time" {
			http.SetCookie(w, &http.Cookie{
				Name:     cookieName,
				Value:    "session",
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
			})
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		cookie, err := r.Cookie(cookieName)
		if err != nil || cookie.Value != "session" {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		mu.Lock()
		gotHost = r.Host
		gotOrigin = r.Header.Get("Origin")
		mu.Unlock()
		_, _ = io.WriteString(w, "authenticated DSH")
	}))
	t.Cleanup(upstream.Close)

	authenticatedURL, err := url.Parse(upstream.URL + "/?token=one-time")
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := newEmbeddedProxy(context.Background(), authenticatedURL)
	if err != nil {
		t.Fatalf("newEmbeddedProxy: %v", err)
	}
	t.Cleanup(proxy.Close)

	if strings.Contains(proxy.URL, "one-time") || strings.Contains(proxy.URL, "?") {
		t.Fatalf("public proxy URL leaked upstream credentials: %s", proxy.URL)
	}
	publicURL, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	if publicURL.Hostname() != "127.0.0.1" || publicURL.User == nil {
		t.Fatalf("proxy URL = %q, want credentialed loopback URL", proxy.URL)
	}
	proxyJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	directClient := &http.Client{Transport: &http.Transport{Proxy: nil}, Jar: proxyJar}
	unauthenticatedURL := *publicURL
	unauthenticatedURL.User = nil
	preClaimResp, err := directClient.Get(unauthenticatedURL.String())
	if err != nil {
		t.Fatal(err)
	}
	_ = preClaimResp.Body.Close()
	if preClaimResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unclaimed proxy status = %d, want 401", preClaimResp.StatusCode)
	}
	directResp, err := directClient.Get(proxy.URL)
	if err != nil {
		t.Fatalf("randomized loopback URL is not directly reachable: %v", err)
	}
	_ = directResp.Body.Close()
	if directResp.StatusCode != http.StatusOK {
		t.Fatalf("direct proxy status = %d, want 200", directResp.StatusCode)
	}
	unauthenticatedResp, err := directClient.Get(unauthenticatedURL.String())
	if err != nil {
		t.Fatal(err)
	}
	_ = unauthenticatedResp.Body.Close()
	if unauthenticatedResp.StatusCode != http.StatusOK {
		t.Fatalf("cookie-authenticated proxy status = %d, want 200", unauthenticatedResp.StatusCode)
	}

	cleanResp, err := (&http.Client{Transport: &http.Transport{Proxy: nil}}).Get(unauthenticatedURL.String())
	if err != nil {
		t.Fatal(err)
	}
	_ = cleanResp.Body.Close()
	if cleanResp.StatusCode != http.StatusOK {
		t.Fatalf("claimed no-referrer proxy status = %d, want 200", cleanResp.StatusCode)
	}

	sameOriginRequest, _ := http.NewRequest(http.MethodGet, unauthenticatedURL.String(), nil)
	sameOriginRequest.Header.Set("Origin", "http://"+publicURL.Host)
	sameOriginResp, err := (&http.Client{Transport: &http.Transport{Proxy: nil}}).Do(sameOriginRequest)
	if err != nil {
		t.Fatal(err)
	}
	_ = sameOriginResp.Body.Close()
	if sameOriginResp.StatusCode != http.StatusOK {
		t.Fatalf("claimed same-origin proxy status = %d, want 200", sameOriginResp.StatusCode)
	}

	crossOriginRequest, _ := http.NewRequest(http.MethodGet, unauthenticatedURL.String(), nil)
	crossOriginRequest.Header.Set("Origin", "https://attacker.invalid")
	crossOriginResp, err := directClient.Do(crossOriginRequest)
	if err != nil {
		t.Fatal(err)
	}
	_ = crossOriginResp.Body.Close()
	if crossOriginResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("claimed cross-origin proxy status = %d, want 401", crossOriginResp.StatusCode)
	}

	client := proxyTestClient(proxy)
	req, _ := http.NewRequest(http.MethodGet, proxy.URL+"api/ping", nil)
	req.Header.Set("Origin", strings.TrimSuffix(proxy.URL, "/"))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "authenticated DSH" {
		t.Fatalf("proxy response = %d %q", resp.StatusCode, body)
	}
	for _, cookie := range resp.Cookies() {
		if cookie.Name == cookieName {
			t.Fatalf("proxy leaked upstream cookie to WebView: %v", resp.Cookies())
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if gotHost != authenticatedURL.Host {
		t.Fatalf("upstream Host = %q, want %q", gotHost, authenticatedURL.Host)
	}
	if gotOrigin != upstream.URL {
		t.Fatalf("upstream Origin = %q, want %q", gotOrigin, upstream.URL)
	}
}

func TestEmbeddedProxyRejectsRequestsWithoutCapabilityHost(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") == "one-time" {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(upstream.Close)

	authenticatedURL, _ := url.Parse(upstream.URL + "/?token=one-time")
	proxy, err := newEmbeddedProxy(context.Background(), authenticatedURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(proxy.Close)

	badURL := "http://localhost:" + strconv.Itoa(proxy.listener.Addr().(*net.TCPAddr).Port) + "/"
	resp, err := proxyTestClient(proxy).Get(badURL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMisdirectedRequest {
		t.Fatalf("wrong-host status = %d, want %d", resp.StatusCode, http.StatusMisdirectedRequest)
	}
}

// A browser never forwards userinfo from a subresource URL (Chromium blocks
// the request outright, WebKit strips the header), so the WebView must be able
// to authenticate with a capability the URL can actually carry: a query
// parameter. This is the regression test for the shipped 0.1.2 adaptation,
// which published a credentialed URL the WebView silently refused.
func TestEmbeddedProxyAcceptsBrowserCapabilityWithoutUserinfo(t *testing.T) {
	const cookieName = "dsh-auth-test"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") == "one-time" {
			http.SetCookie(w, &http.Cookie{
				Name: cookieName, Value: "session", Path: "/",
				HttpOnly: true, SameSite: http.SameSiteStrictMode,
			})
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		if _, err := r.Cookie(cookieName); err != nil {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, "authenticated DSH")
	}))
	t.Cleanup(upstream.Close)

	authenticatedURL, err := url.Parse(upstream.URL + "/?token=one-time")
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := newEmbeddedProxy(context.Background(), authenticatedURL)
	if err != nil {
		t.Fatalf("newEmbeddedProxy: %v", err)
	}
	t.Cleanup(proxy.Close)

	browserURL, err := url.Parse(proxy.BrowserURL)
	if err != nil {
		t.Fatalf("browser URL %q: %v", proxy.BrowserURL, err)
	}
	if browserURL.User != nil {
		t.Fatalf("browser URL must not carry userinfo: %q", proxy.BrowserURL)
	}
	if got := browserURL.Query().Get(embeddedProxyCapabilityQuery); got == "" {
		t.Fatalf("browser URL must carry the capability query: %q", proxy.BrowserURL)
	}

	plain := &http.Client{Transport: &http.Transport{Proxy: nil}}

	// Pre-claim, a request without the capability is refused.
	uncredentialed := *browserURL
	uncredentialed.RawQuery = ""
	denied, err := plain.Get(uncredentialed.String())
	if err != nil {
		t.Fatal(err)
	}
	_ = denied.Body.Close()
	if denied.StatusCode != http.StatusUnauthorized {
		t.Fatalf("capability-less pre-claim status = %d, want 401", denied.StatusCode)
	}

	// The browser-shaped request carries no Authorization header at all.
	req, err := http.NewRequest(http.MethodGet, proxy.BrowserURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if req.Header.Get("Authorization") != "" {
		t.Fatal("browser-shaped request must not carry Authorization")
	}
	resp, err := plain.Do(req)
	if err != nil {
		t.Fatalf("browser-shaped request: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "authenticated DSH" {
		t.Fatalf("browser-shaped response = %d %q", resp.StatusCode, body)
	}
}

func TestDSHManagerPublishesEmbeddedProxyURL(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") == "one-time" {
			http.SetCookie(w, &http.Cookie{Name: "dsh-auth-test", Value: "session", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		if _, err := r.Cookie("dsh-auth-test"); err != nil {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, "authenticated DSH")
	}))
	t.Cleanup(upstream.Close)
	upstreamURL, _ := url.Parse(upstream.URL)
	port, _ := strconv.Atoi(upstreamURL.Port())

	m := newDSHManager(&App{})
	m.port = port
	line := "dsh web: " + upstream.URL + "/?token=one-time"
	if !m.captureAdvertisedURL(context.Background(), line) {
		t.Fatal("captureAdvertisedURL failed")
	}
	t.Cleanup(m.stopEmbeddedProxy)

	status := m.current()
	if status.URL == "" || strings.Contains(status.URL, "one-time") || status.URL == upstream.URL+"/?token=one-time" {
		t.Fatalf("published unsafe embedded URL: %q", status.URL)
	}
	m.mu.Lock()
	proxy := m.embedProxy
	m.mu.Unlock()
	resp, err := proxyTestClient(proxy).Get(status.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "authenticated DSH" {
		t.Fatalf("embedded response = %d %q", resp.StatusCode, body)
	}
}

func TestRemoteProxyChainsThroughAuthenticatedEmbeddedProxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") == "one-time" {
			http.SetCookie(w, &http.Cookie{Name: "dsh-auth-test", Value: "session", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		if _, err := r.Cookie("dsh-auth-test"); err != nil {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html><head></head><body>authenticated DSH</body></html>")
	}))
	t.Cleanup(upstream.Close)
	authenticatedURL, _ := url.Parse(upstream.URL + "/?token=one-time")
	embedded, err := newEmbeddedProxy(context.Background(), authenticatedURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(embedded.Close)

	t.Setenv(stateDirEnv, t.TempDir())
	remote := newRemoteManager(nil)
	if _, err := remote.enable(embedded.URL); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(remote.disable)
	addr := fmt.Sprintf("127.0.0.1:%d", remote.status().Port)
	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, dialErr := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("remote server did not start: %v", dialErr)
		}
	}
	base := "https://" + addr
	jwt := pairJWT(t, remote, base)
	resp, body := authedGet(t, base, jwt)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(body, []byte("authenticated DSH")) {
		t.Fatalf("remote chained response = %d %q", resp.StatusCode, body)
	}
}

func proxyTestClient(proxy *embeddedProxy) *http.Client {
	return &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, proxy.listener.Addr().String())
		},
	}}
}
