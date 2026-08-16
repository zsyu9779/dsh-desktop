package main

import (
	"bytes"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/skip2/go-qrcode"
)

const (
	remotePreferredPort = 8787
	remoteCookieName    = "dsh_remote"
)

//go:embed remote_polyfill.js
var polyfillScript []byte

type remoteStatus struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
	Token   string `json:"token"`
	QR      string `json:"qr"`
	Port    int    `json:"port"`
	Message string `json:"message"`
}

type remoteManager struct {
	app *App

	mu      sync.Mutex
	enabled bool
	token   string
	port    int
	target  string
	server  *http.Server
}

func newRemoteManager(app *App) *remoteManager {
	return &remoteManager{app: app}
}

func (r *remoteManager) status() remoteStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buildStatusLocked()
}

func (r *remoteManager) buildStatusLocked() remoteStatus {
	s := remoteStatus{Enabled: r.enabled, Token: r.token, Port: r.port}
	if r.enabled {
		if ip := firstLANIP(); ip != "" {
			s.URL = fmt.Sprintf("http://%s:%d", ip, r.port)
		}
		s.QR = r.qrLocked()
		s.Message = "remote enabled"
	} else {
		s.Message = "remote disabled"
	}
	return s
}

func (r *remoteManager) qrLocked() string {
	if r.token == "" {
		return ""
	}
	ip := firstLANIP()
	if ip == "" {
		return ""
	}
	pairingURL := fmt.Sprintf("http://%s:%d/?t=%s", ip, r.port, r.token)
	png, err := qrcode.Encode(pairingURL, qrcode.Medium, 256)
	if err != nil {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
}

func (r *remoteManager) enable(target string) (remoteStatus, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.enabled {
		return r.buildStatusLocked(), nil
	}
	if target == "" {
		return r.buildStatusLocked(), fmt.Errorf("harness not ready")
	}

	token, err := randomToken(24)
	if err != nil {
		return r.buildStatusLocked(), err
	}
	port, err := pickRemotePort()
	if err != nil {
		return r.buildStatusLocked(), err
	}
	targetURL, err := url.Parse(target)
	if err != nil {
		return r.buildStatusLocked(), err
	}

	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	// dsh's /api trust fence rejects a request when its Origin header does not
	// match the Host it was served from. The phone talks to the proxy origin
	// (e.g. http://192.168.1.5:8787), so we rewrite browser markers back to the
	// loopback target or every POST and WebSocket upgrade would 403.
	targetOrigin := targetURL.Scheme + "://" + targetURL.Host
	baseDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		baseDirector(req)
		// NewSingleHostReverseProxy rewrites req.URL.Host but leaves req.Host
		// (the wire Host header) untouched, so the phone's Host would reach dsh
		// as 192.168.x.x and trip the /api trust fence. Force it to loopback.
		req.Host = targetURL.Host
		if req.Header.Get("Origin") != "" {
			req.Header.Set("Origin", targetOrigin)
		}
		if req.Header.Get("Referer") != "" {
			req.Header.Set("Referer", targetOrigin+"/")
		}
	}
	proxy.ModifyResponse = func(resp *http.Response) error {
		ct := resp.Header.Get("Content-Type")
		if !strings.HasPrefix(ct, "text/html") {
			return nil
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		_ = resp.Body.Close()
		modified := injectPolyfill(body)
		resp.Body = io.NopCloser(bytes.NewReader(modified))
		resp.ContentLength = int64(len(modified))
		resp.Header.Set("Content-Length", strconv.Itoa(len(modified)))
		resp.Header.Del("Content-Encoding")
		return nil
	}
	server := &http.Server{
		Addr:              fmt.Sprintf("0.0.0.0:%d", port),
		Handler:           r.authMiddleware(proxy),
		ReadHeaderTimeout: 10 * time.Second,
	}

	r.enabled = true
	r.token = token
	r.port = port
	r.target = target
	r.server = server

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			r.logf("remote server error: %v", err)
			r.mu.Lock()
			if r.server == server {
				r.enabled = false
				r.server = nil
			}
			r.mu.Unlock()
		}
	}()

	r.logf("remote enabled on 0.0.0.0:%d -> %s", port, target)
	return r.buildStatusLocked(), nil
}

func (r *remoteManager) disable() {
	r.mu.Lock()
	server := r.server
	r.enabled = false
	r.server = nil
	r.token = ""
	r.mu.Unlock()

	if server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
		_ = server.Close()
	}
	r.logf("remote disabled")
}

func (r *remoteManager) regenerateToken() remoteStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.enabled {
		return r.buildStatusLocked()
	}
	token, err := randomToken(24)
	if err != nil {
		r.logf("rotate token failed: %v", err)
		return r.buildStatusLocked()
	}
	r.token = token
	r.logf("remote token rotated")
	return r.buildStatusLocked()
}

func (r *remoteManager) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		token := r.token
		enabled := r.enabled
		r.mu.Unlock()

		if !enabled || token == "" {
			http.Error(w, "remote disabled", http.StatusServiceUnavailable)
			return
		}

		if (req.URL.Path == "/" || req.URL.Path == "") && req.URL.Query().Get("t") != "" {
			if req.URL.Query().Get("t") != token {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			http.SetCookie(w, &http.Cookie{
				Name:     remoteCookieName,
				Value:    token,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
			})
			http.Redirect(w, req, "/", http.StatusFound)
			return
		}

		c, err := req.Cookie(remoteCookieName)
		if err != nil || c.Value != token {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, req)
	})
}

func (r *remoteManager) logf(format string, args ...interface{}) {
	if r.app != nil && r.app.dsh != nil {
		r.app.dsh.logf("[remote] "+format, args...)
	}
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func pickRemotePort() (int, error) {
	if l, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", remotePreferredPort)); err == nil {
		_ = l.Close()
		return remotePreferredPort, nil
	}
	l, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// firstLANIP returns the best-guess LAN IPv4 for the pairing URL, preferring
// physical interfaces (en/eth/wl) over virtual ones (utun/vmnet) so a VPN or
// VM adapter does not shadow the address the phone can actually reach.
func firstLANIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	var fallback string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		ip4 := ipv4For(iface)
		if ip4 == "" || strings.HasPrefix(ip4, "169.254.") {
			continue
		}
		if isPhysicalIface(iface.Name) {
			return ip4
		}
		if fallback == "" {
			fallback = ip4
		}
	}
	return fallback
}

// ipv4For returns the first IPv4 address assigned to the interface.
func ipv4For(iface net.Interface) string {
	addrs, err := iface.Addrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok {
			if ip4 := ipnet.IP.To4(); ip4 != nil {
				return ip4.String()
			}
		}
	}
	return ""
}

// isPhysicalIface reports whether the interface name looks like a physical
// NIC (Ethernet/WiFi/WWAN) across macOS, Linux, and Windows.
func isPhysicalIface(name string) bool {
	for _, p := range []string{"en", "eth", "wl", "wlan", "wwan"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// injectPolyfill inserts the polyfill right after the <head> opening tag so it
// runs before the app's deferred module scripts.
func injectPolyfill(html []byte) []byte {
	lower := bytes.ToLower(html)
	idx := bytes.Index(lower, []byte("<head"))
	if idx == -1 {
		return prependPolyfill(html)
	}
	gt := bytes.IndexByte(html[idx:], '>')
	if gt == -1 {
		return prependPolyfill(html)
	}
	insertAt := idx + gt + 1
	out := make([]byte, 0, len(html)+len(polyfillScript))
	out = append(out, html[:insertAt]...)
	out = append(out, polyfillScript...)
	out = append(out, html[insertAt:]...)
	return out
}

// prependPolyfill puts the polyfill before the document when no <head> exists.
func prependPolyfill(html []byte) []byte {
	out := make([]byte, 0, len(polyfillScript)+len(html))
	out = append(out, polyfillScript...)
	out = append(out, html...)
	return out
}
