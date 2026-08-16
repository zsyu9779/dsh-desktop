package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/skip2/go-qrcode"
)

const (
	remotePreferredPort = 8787
	remoteCookieName    = "dsh_remote"
	pairingCodeTTL      = 60 * time.Second
	deviceJWTTTL        = 12 * time.Hour
)

//go:embed remote_polyfill.js
var polyfillScript []byte

type remoteStatus struct {
	Enabled       bool   `json:"enabled"`
	URL           string `json:"url"`
	PairingCode   string `json:"pairingCode"`
	HostPublicKey string `json:"hostPublicKey"`
	QR            string `json:"qr"`
	Port          int    `json:"port"`
	Message       string `json:"message"`
}

type remoteManager struct {
	app *App

	mu            sync.Mutex
	enabled       bool
	pairingCode   string
	pairingExpiry time.Time
	cred          *hostCredential
	devices       *deviceRegistry
	port          int
	target        string
	server        *http.Server
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
	s := remoteStatus{Enabled: r.enabled, PairingCode: r.pairingCode, Port: r.port}
	if r.cred != nil {
		s.HostPublicKey = r.cred.publicKeyB64()
	}
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
	if r.pairingCode == "" || time.Now().After(r.pairingExpiry) {
		return ""
	}
	ip := firstLANIP()
	if ip == "" {
		return ""
	}
	pairingURL := fmt.Sprintf("http://%s:%d/?pair=%s", ip, r.port, r.pairingCode)
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

	cred, err := loadOrCreateCredential(stateDir())
	if err != nil {
		return r.buildStatusLocked(), err
	}
	r.cred = cred
	r.devices = newDeviceRegistry(filepath.Join(stateDir(), "devices.json"))

	code, err := randomToken(12)
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
	targetOrigin := targetURL.Scheme + "://" + targetURL.Host
	baseDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		baseDirector(req)
		// NewSingleHostReverseProxy rewrites req.URL.Host but leaves req.Host
		// (the wire Host header) untouched; force it to loopback so dsh's
		// /api trust fence accepts the request.
		req.Host = targetURL.Host
		// Prefer uncompressed HTML so our polyfill injection never lands on
		// compressed bytes; we still handle gzip defensively in ModifyResponse.
		req.Header.Set("Accept-Encoding", "identity")
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

		if resp.Header.Get("Content-Encoding") == "gzip" {
			raw, err := gunzip(body)
			if err != nil {
				// Can't decode; pass through unchanged rather than corrupting.
				resp.Body = io.NopCloser(bytes.NewReader(body))
				return nil
			}
			modified := injectPolyfill(raw)
			recompressed, err := gzipBytes(modified)
			if err != nil {
				return err
			}
			resp.Body = io.NopCloser(bytes.NewReader(recompressed))
			resp.ContentLength = int64(len(recompressed))
			resp.Header.Set("Content-Length", strconv.Itoa(len(recompressed)))
			resp.Header.Set("Content-Encoding", "gzip")
			return nil
		}

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
	r.pairingCode = code
	r.pairingExpiry = time.Now().Add(pairingCodeTTL)
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
	r.pairingCode = ""
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
	code, err := randomToken(12)
	if err != nil {
		r.logf("rotate pairing code failed: %v", err)
		return r.buildStatusLocked()
	}
	r.pairingCode = code
	r.pairingExpiry = time.Now().Add(pairingCodeTTL)
	r.logf("pairing code rotated")
	return r.buildStatusLocked()
}

func (r *remoteManager) listDevices() []deviceIdentity {
	r.mu.Lock()
	devices := r.devices
	r.mu.Unlock()
	if devices == nil {
		return nil
	}
	return devices.list()
}

func (r *remoteManager) revokeDevice(deviceID string) bool {
	r.mu.Lock()
	devices := r.devices
	r.mu.Unlock()
	if devices == nil {
		return false
	}
	return devices.revoke(deviceID)
}

func (r *remoteManager) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		enabled := r.enabled
		pairingCode := r.pairingCode
		pairingExpiry := r.pairingExpiry
		cred := r.cred
		devices := r.devices
		r.mu.Unlock()

		if !enabled || cred == nil {
			http.Error(w, "remote disabled", http.StatusServiceUnavailable)
			return
		}

		// One-time pairing code presented on the root path.
		if req.URL.Path == "/" && req.URL.Query().Get("pair") != "" {
			if r.consumePairingCode(w, req, pairingCode, pairingExpiry, cred, devices) {
				http.Redirect(w, req, "/", http.StatusFound)
			}
			return
		}

		// Authenticated requests carry the device JWT in a cookie.
		c, err := req.Cookie(remoteCookieName)
		if err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		claims, err := cred.verifyJWT(c.Value)
		if err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if !devices.exists(claims.DeviceID) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, req)
	})
}

// consumePairingCode validates the one-time pairing code, registers the device,
// and issues a JWT cookie. It returns true on success (after writing the cookie).
func (r *remoteManager) consumePairingCode(w http.ResponseWriter, req *http.Request, code string, expiry time.Time, cred *hostCredential, devices *deviceRegistry) bool {
	if code == "" || time.Now().After(expiry) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	if subtle.ConstantTimeCompare([]byte(req.URL.Query().Get("pair")), []byte(code)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}

	// Single-use: burn the code on success so it can't be replayed.
	r.mu.Lock()
	r.pairingCode = ""
	r.pairingExpiry = time.Time{}
	r.mu.Unlock()

	deviceID := newDeviceID()
	devices.register(deviceID, "device", deviceFingerprint(deviceID))
	token, err := cred.signJWT(deviceID, "full", deviceJWTTTL)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return false
	}
	http.SetCookie(w, &http.Cookie{
		Name:     remoteCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	return true
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
// physical interfaces (en/eth/wl) over virtual ones (utun/vmnet).
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

func isPhysicalIface(name string) bool {
	for _, p := range []string{"en", "eth", "wl", "wlan", "wwan"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

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

func prependPolyfill(html []byte) []byte {
	out := make([]byte, 0, len(polyfillScript)+len(html))
	out = append(out, polyfillScript...)
	out = append(out, html...)
	return out
}

func gunzip(data []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return io.ReadAll(zr)
}

func gzipBytes(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(data); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
