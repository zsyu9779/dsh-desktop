package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
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

// remoteAllowedEndpoints is the exact /api/<namespace>/<method> surface a
// paired Device may call. DSH 0.1.2 removed the upstream privileged-method
// list — the /api fence now authenticates the browser session instead — so the
// shell owns the policy and denies everything not listed here. A namespace a
// future DSH release adds therefore stays unreachable until it is listed.
var remoteAllowedEndpoints = map[string]bool{
	// Session main line.
	"session/list": true, "session/search": true, "session/create": true,
	"session/selectModel": true, "session/modelCatalog": true,
	"session/canOpenWorkspacePath": true, "session/rename": true, "session/fork": true,
	"session/prompt": true, "session/attachment": true, "session/updateQueue": true,
	"session/cancel": true, "session/page": true, "session/follow": true,
	"session/control": true, "session/uploadFileBinary": true,
	// Workspace organisation.
	"workspace/create": true, "workspace/rename": true, "workspace/delete": true,
	"workspace/insertBefore": true, "workspace/insertSessionBefore": true,
	"workspace/archiveSession": true, "workspace/follow": true,
	// The browse picker's primitives; the native chooser (pick) stays desktop-only.
	"directoryPicker/list": true, "directoryPicker/createDirectory": true,
	// Goals, commands, skills, feedback and references.
	"goals/get": true, "goals/edit": true, "goals/pause": true, "goals/resume": true,
	"goals/complete": true, "goals/clear": true, "goals/create": true,
	"commands/list": true, "commands/execute": true,
	"skills/list":                         true,
	"messageFeedback/list":                true,
	"messageFeedback/put":                 true,
	"messageFeedback/delete":              true,
	"sessionFeedback/record":              true,
	"fileReferences/list":                 true,
	"sessionReferenceResolver/candidates": true,
	"agentTeams/view":                     true,
	"agentTeams/createTask":               true,
	"agentTeams/updateTask":               true,
	// Read-only metadata the composer needs to render its pickers.
	"agentPresets/list": true,
	"llm/listProviders": true, "llm/listConfigurableProviders": true,
}

// remoteAllowedExactPaths are /api routes that are not namespace/method pairs.
var remoteAllowedExactPaths = map[string]bool{
	// Multiplexed Remote streams (WebSocket); the phone's live updates ride it.
	"/api/remote.mux": true,
	// Result channel for forwarded waterfall events: without it a paired Device
	// can receive an approval or question but never answer it.
	"/api/$events/result": true,
}

// isRemoteAllowedPath reports whether a paired Device may reach path. Every
// /api path outside the allowlist is refused, so a namespace a future DSH
// release adds fails closed; non-/api/ paths (static assets and the shell's own
// routes) are governed by isPreinstalledPluginRoute instead.
func isRemoteAllowedPath(path string) bool {
	if !strings.HasPrefix(path, "/api/") {
		return true // static assets and the shell's own routes
	}
	if remoteAllowedExactPaths[path] {
		return true
	}
	return remoteAllowedEndpoints[strings.TrimPrefix(path, "/api/")]
}

// preinstalledPluginRoutePrefixes are URL path prefixes registered by the
// preinstalled DSH plugins (diff-review's git/file routes, file-changes'
// reveal route). They sit outside the remote endpoint allowlist, so they must
// be blocked outright over the LAN proxy: safe on the desktop loopback, but
// never reachable from a paired Device.
var preinstalledPluginRoutePrefixes = []string{
	"/diff-review/",
	"/api/file-changes/",
}

// isPreinstalledPluginRoute reports whether a request path targets a
// preinstalled-plugin HTTP route that must be blocked over the LAN proxy.
func isPreinstalledPluginRoute(path string) bool {
	for _, prefix := range preinstalledPluginRoutePrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

type remoteStatus struct {
	Enabled         bool   `json:"enabled"`
	AllowPrivileged bool   `json:"allowPrivileged"`
	URL             string `json:"url"`
	PairingCode     string `json:"pairingCode"`
	HostPublicKey   string `json:"hostPublicKey"`
	CertFingerprint string `json:"certFingerprint"`
	QR              string `json:"qr"`
	Port            int    `json:"port"`
	Message         string `json:"message"`
}

type remoteManager struct {
	app *App

	mu              sync.Mutex
	enabled         bool
	allowPrivileged bool
	pairingCode     string
	pairingExpiry   time.Time
	cred            *hostCredential
	certFingerprint string
	devices         *deviceRegistry
	transport       *deviceTransportTracker
	port            int
	target          string
	server          *http.Server
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
	s := remoteStatus{Enabled: r.enabled, PairingCode: r.pairingCode, Port: r.port, AllowPrivileged: r.allowPrivileged, CertFingerprint: r.certFingerprint}
	if r.cred != nil {
		s.HostPublicKey = r.cred.publicKeyB64()
	}
	if r.enabled {
		if ip := firstLANIP(); ip != "" {
			s.URL = fmt.Sprintf("https://%s:%d", ip, r.port)
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
	pairingURL := fmt.Sprintf("https://%s:%d/?pair=%s", ip, r.port, r.pairingCode)
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

	var certIP net.IP
	if ipStr := firstLANIP(); ipStr != "" {
		certIP = net.ParseIP(ipStr)
	}
	leafCertPEM, leafKeyPEM, fingerprint, err := cred.issueLeafCert(certIP)
	if err != nil {
		return r.buildStatusLocked(), err
	}
	// Persist the stable leaf so the fingerprint survives restarts (ticket 04).
	if err := cred.save(stateDir()); err != nil {
		return r.buildStatusLocked(), fmt.Errorf("persist leaf cert: %w", err)
	}
	tlsCert, err := tls.X509KeyPair(leafCertPEM, leafKeyPEM)
	if err != nil {
		return r.buildStatusLocked(), err
	}

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
	targetUsername := ""
	targetPassword := ""
	if targetURL.User != nil {
		targetUsername = targetURL.User.Username()
		targetPassword, _ = targetURL.User.Password()
	}
	baseDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		baseDirector(req)
		// NewSingleHostReverseProxy rewrites req.URL.Host but leaves req.Host
		// (the wire Host header) untouched; force it to loopback so dsh's
		// /api trust fence accepts the request.
		req.Host = targetURL.Host
		if targetUsername != "" {
			req.SetBasicAuth(targetUsername, targetPassword)
		}
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
		TLSConfig:         &tls.Config{Certificates: []tls.Certificate{tlsCert}},
	}

	r.enabled = true
	r.pairingCode = code
	r.pairingExpiry = time.Now().Add(pairingCodeTTL)
	r.port = port
	safeTarget := *targetURL
	safeTarget.User = nil
	r.target = safeTarget.String()
	r.certFingerprint = fingerprint
	r.server = server

	go func() {
		if err := server.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			r.logf("remote server error: %v", err)
			r.mu.Lock()
			if r.server == server {
				r.enabled = false
				r.server = nil
			}
			r.mu.Unlock()
		}
	}()

	r.logf("remote enabled: https://0.0.0.0:%d -> %s (cert=%s, pairing=%s)", port, safeTarget.String(), fingerprint, code)
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

func (r *remoteManager) lanCredentialFor(deviceID string) (*hostCredential, error) {
	r.mu.Lock()
	cred := r.cred
	devices := r.devices
	r.mu.Unlock()
	if devices == nil {
		devices = newDeviceRegistry(filepath.Join(stateDir(), "devices.json"))
	}
	if !devices.exists(deviceID) {
		return nil, errors.New("Device 没有既有 LAN Pairing")
	}
	if cred == nil {
		var err error
		cred, err = loadExistingHostCredential(stateDir())
		if err != nil {
			return nil, err
		}
	}
	return cred, nil
}

func (r *remoteManager) lanPublicKey(deviceID string) (string, error) {
	cred, err := r.lanCredentialFor(deviceID)
	if err != nil {
		return "", err
	}
	return cred.publicKeyB64(), nil
}

// signLANPairing signs an Account registration intent only for a Device
// already authorized by this Host's persisted LAN credential.
func (r *remoteManager) signLANPairing(deviceID string, message []byte) ([]byte, error) {
	cred, err := r.lanCredentialFor(deviceID)
	if err != nil {
		return nil, err
	}
	return ed25519.Sign(cred.privateKey(), message), nil
}

func (r *remoteManager) revokeDevice(deviceID string) bool {
	r.mu.Lock()
	devices := r.devices
	r.mu.Unlock()
	if devices == nil {
		return false
	}
	if devices.revoke(deviceID) {
		r.logf("device revoked: %s", deviceID)
		return true
	}
	return false
}

func (r *remoteManager) setAllowPrivileged(v bool) {
	r.mu.Lock()
	r.allowPrivileged = v
	r.mu.Unlock()
	r.logf("remote scope: allow-all = %v", v)
}

func (r *remoteManager) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		enabled := r.enabled
		pairingCode := r.pairingCode
		pairingExpiry := r.pairingExpiry
		cred := r.cred
		devices := r.devices
		allowPrivileged := r.allowPrivileged
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
			r.logf("auth rejected: missing cookie (from %s)", req.RemoteAddr)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		claims, err := cred.verifyJWT(c.Value)
		if err != nil {
			r.logf("auth rejected: invalid token (from %s)", req.RemoteAddr)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if !devices.exists(claims.DeviceID) {
			r.logf("auth rejected: device %s revoked (from %s)", claims.DeviceID, req.RemoteAddr)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		devices.touch(claims.DeviceID)
		if r.transport != nil {
			r.transport.mark(deviceActivity{DeviceID: claims.DeviceID, Name: devices.name(claims.DeviceID), Transport: deviceTransportLAN})
		}
		if req.Method == http.MethodPost && req.URL.Path == "/api/remote/account-pairing-proof" {
			var body struct {
				DeviceChallenge string `json:"deviceChallenge"`
			}
			if json.NewDecoder(req.Body).Decode(&body) != nil {
				http.Error(w, "invalid request", http.StatusBadRequest)
				return
			}
			challenge, err := base64.StdEncoding.DecodeString(body.DeviceChallenge)
			if err != nil || len(challenge) < 16 || r.app == nil || r.app.remoteSetup == nil {
				http.Error(w, "invalid challenge", http.StatusBadRequest)
				return
			}
			identity, hostProof, err := r.app.remoteSetup.stageLANProof(req.Context(), claims.DeviceID, challenge)
			if err != nil {
				http.Error(w, "pairing proof unavailable", http.StatusServiceUnavailable)
				return
			}
			hostKey, err := identity.x25519PublicKey()
			if err != nil {
				http.Error(w, "Host identity unavailable", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"hostID": identity.HostID, "hostIdentityPublicKey": base64.StdEncoding.EncodeToString(hostKey),
				"deviceChallenge": body.DeviceChallenge, "hostProof": base64.StdEncoding.EncodeToString(hostProof),
			})
			return
		}
		if req.Method == http.MethodPost && req.URL.Path == "/api/remote/account-pairing-complete" {
			var body struct {
				PairingID               string `json:"pairingID"`
				DeviceID                string `json:"deviceID"`
				DeviceIdentityPublicKey string `json:"deviceIdentityPublicKey"`
			}
			if json.NewDecoder(req.Body).Decode(&body) != nil || body.PairingID == "" || body.DeviceID == "" || r.app == nil || r.app.remoteSetup == nil {
				http.Error(w, "invalid request", http.StatusBadRequest)
				return
			}
			deviceKey, err := base64.StdEncoding.DecodeString(body.DeviceIdentityPublicKey)
			if err != nil || len(deviceKey) == 0 {
				http.Error(w, "invalid device identity", http.StatusBadRequest)
				return
			}
			if err := r.app.remoteSetup.completeLANPairing(req.Context(), claims.DeviceID, pairedDevice{
				PairingID: body.PairingID, DeviceID: body.DeviceID, DeviceIdentityPublicKey: deviceKey,
			}); err != nil {
				http.Error(w, "pairing confirmation failed", http.StatusForbidden)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if !allowPrivileged && !isRemoteAllowedPath(req.URL.Path) {
			r.logf("auth rejected: %s is not in the remote allowlist (from %s)", req.URL.Path, req.RemoteAddr)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if isPreinstalledPluginRoute(req.URL.Path) {
			r.logf("auth rejected: preinstalled-plugin route %s denied (from %s)", req.URL.Path, req.RemoteAddr)
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
		r.logf("pairing rejected: no code or expired (from %s)", req.RemoteAddr)
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	if subtle.ConstantTimeCompare([]byte(req.URL.Query().Get("pair")), []byte(code)) != 1 {
		r.logf("pairing rejected: bad code (from %s)", req.RemoteAddr)
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
		r.logf("pairing failed: sign JWT: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return false
	}
	r.logf("device paired: %s (from %s)", deviceID, req.RemoteAddr)
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
