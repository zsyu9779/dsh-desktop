package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"

	"github.com/skip2/go-qrcode"
)

const (
	remotePreferredPort = 8787
	remoteCookieName    = "dsh_remote"
)

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

func firstLANIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() {
			continue
		}
		if ip4 := ipnet.IP.To4(); ip4 != nil {
			return ip4.String()
		}
	}
	return ""
}
