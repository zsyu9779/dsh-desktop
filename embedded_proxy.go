package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// embeddedProxy turns DSH's one-time URL into a stable, cookie-free origin for
// the desktop WebView. The upstream session cookie stays in the Go process, so
// WKWebView never needs to accept a SameSite=Strict cookie inside an iframe.
type embeddedProxy struct {
	URL      string
	listener net.Listener
	server   *http.Server
	close    sync.Once
	first    sync.Once
	firstHit chan struct{}
}

const embeddedProxyCookieName = "dsh_desktop_embed"

func newEmbeddedProxy(ctx context.Context, authenticatedURL *url.URL) (*embeddedProxy, error) {
	if err := validateUpstreamURL(authenticatedURL); err != nil {
		return nil, err
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("创建 DSH 会话存储失败: %w", err)
	}
	transport := &http.Transport{Proxy: nil}
	bootstrapClient := &http.Client{
		Transport: transport,
		Jar:       jar,
		Timeout:   10 * time.Second,
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, authenticatedURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("创建 DSH 鉴权请求失败: %w", err)
	}
	response, err := bootstrapClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("建立 DSH 鉴权会话失败: %w", err)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	_ = response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("建立 DSH 鉴权会话失败: HTTP %d", response.StatusCode)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("启动桌面嵌入代理失败: %w", err)
	}
	capability, err := randomCapability()
	if err != nil {
		_ = listener.Close()
		return nil, err
	}
	publicHost := listener.Addr().String()
	target := &url.URL{Scheme: authenticatedURL.Scheme, Host: authenticatedURL.Host}
	publicOrigin := "http://" + publicHost
	publicURL := &url.URL{Scheme: "http", Host: publicHost, Path: "/", User: url.UserPassword("dsh", capability)}
	targetOrigin := target.Scheme + "://" + target.Host
	var claimed atomic.Bool

	p := &embeddedProxy{
		URL:      publicURL.String(),
		listener: listener,
		firstHit: make(chan struct{}),
	}

	reverseProxy := httputil.NewSingleHostReverseProxy(target)
	baseDirector := reverseProxy.Director
	reverseProxy.Director = func(req *http.Request) {
		baseDirector(req)
		req.Host = target.Host
		req.Header.Del("Cookie")
		for _, cookie := range jar.Cookies(req.URL) {
			req.AddCookie(cookie)
		}
		if req.Header.Get("Origin") != "" {
			req.Header.Set("Origin", targetOrigin)
		}
		if req.Header.Get("Referer") != "" {
			req.Header.Set("Referer", targetOrigin+"/")
		}
	}
	reverseProxy.ModifyResponse = func(resp *http.Response) error {
		if resp.Request != nil && resp.Request.URL != nil {
			jar.SetCookies(resp.Request.URL, resp.Cookies())
		}
		resp.Header.Del("Set-Cookie")
		if location := resp.Header.Get("Location"); location != "" {
			if parsed, parseErr := url.Parse(location); parseErr == nil && parsed.IsAbs() && parsed.Scheme == target.Scheme && parsed.Host == target.Host {
				parsed.Scheme = "http"
				parsed.Host = publicHost
				resp.Header.Set("Location", parsed.String())
			}
		}
		if resp.Header.Get("Access-Control-Allow-Origin") == targetOrigin {
			resp.Header.Set("Access-Control-Allow-Origin", publicOrigin)
		}
		return nil
	}
	reverseProxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, proxyErr error) {
		http.Error(w, "DSH upstream unavailable: "+proxyErr.Error(), http.StatusBadGateway)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !strings.EqualFold(req.Host, publicHost) {
			http.Error(w, "misdirected request", http.StatusMisdirectedRequest)
			return
		}
		username, password, ok := req.BasicAuth()
		validUser := subtle.ConstantTimeCompare([]byte(username), []byte("dsh")) == 1
		validPassword := subtle.ConstantTimeCompare([]byte(password), []byte(capability)) == 1
		validBasicAuth := ok && validUser && validPassword
		cookie, cookieErr := req.Cookie(embeddedProxyCookieName)
		validCookie := cookieErr == nil && subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(capability)) == 1
		crossOrigin := isCrossOriginProxyRequest(req, publicOrigin)
		// WKWebView may omit both Origin and Referer for fetches made by DSH
		// plugins. Once the credentialed root document has claimed this random
		// loopback listener, allow requests without an explicit foreign origin.
		// An explicit cross-origin request is still rejected, even if the browser
		// happens to attach our local cookie.
		if !validBasicAuth && (crossOrigin || !validCookie && !claimed.Load()) {
			w.Header().Set("WWW-Authenticate", `Basic realm="dsh-desktop", charset="UTF-8"`)
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		if validBasicAuth {
			claimed.Store(true)
		}
		if validBasicAuth && !validCookie {
			http.SetCookie(w, &http.Cookie{
				Name:     embeddedProxyCookieName,
				Value:    capability,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
			})
		}
		req.Header.Del("Authorization")
		p.first.Do(func() { close(p.firstHit) })
		reverseProxy.ServeHTTP(w, req)
	})
	p.server = &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		_ = p.server.Serve(listener)
	}()
	return p, nil
}

// isCrossOriginProxyRequest rejects requests that explicitly identify another
// origin. Missing Origin and Referer are common for no-referrer plugin fetches.
func isCrossOriginProxyRequest(req *http.Request, publicOrigin string) bool {
	if origin := req.Header.Get("Origin"); origin != "" {
		return origin != publicOrigin
	}
	referer := req.Referer()
	if referer == "" {
		return false
	}
	parsed, err := url.Parse(referer)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return true
	}
	origin := parsed.Scheme + "://" + parsed.Host
	return origin != publicOrigin
}

func (p *embeddedProxy) FirstRequest() <-chan struct{} {
	return p.firstHit
}

func validateUpstreamURL(upstream *url.URL) error {
	if upstream == nil || upstream.Scheme != "http" || upstream.Hostname() != "127.0.0.1" || upstream.Port() == "" || upstream.User != nil {
		return fmt.Errorf("DSH 鉴权地址不是受信任的本机 HTTP 地址")
	}
	return nil
}

func randomCapability() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("生成桌面嵌入地址失败: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

func (p *embeddedProxy) Close() {
	if p == nil {
		return
	}
	p.close.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = p.server.Shutdown(ctx)
		_ = p.listener.Close()
	})
}
