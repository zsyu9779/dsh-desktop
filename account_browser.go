package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type localDeveloperAccountAuthorizer struct{ token string }

func (a localDeveloperAccountAuthorizer) authorize(context.Context) (string, error) {
	if a.token == "" {
		return "", errAccountRejected
	}
	return a.token, nil
}

func loadLocalDeveloperAccountAuthorizer() (accountAuthorizer, bool) {
	path := strings.TrimSpace(os.Getenv("DSH_DEV_IDENTITY_TOKEN_FILE"))
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return nil, false
		}
		path = filepath.Join(home, ".dsh-desktop", "dev-identity-token")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return nil, false
	}
	return localDeveloperAccountAuthorizer{token: token}, true
}

type browserAccountAuthorizer struct {
	accountBaseURL string
	openURL        func(string) error
}

func newBrowserAccountAuthorizer(accountBaseURL string, openURL func(string) error) *browserAccountAuthorizer {
	return &browserAccountAuthorizer{accountBaseURL: strings.TrimRight(accountBaseURL, "/"), openURL: openURL}
}

type browserAuthorizationResult struct {
	token string
	err   error
}

func (a *browserAccountAuthorizer) authorize(ctx context.Context) (string, error) {
	accountURL, err := url.Parse(a.accountBaseURL)
	if err != nil || accountURL.Scheme == "" || accountURL.Host == "" {
		return "", errors.New("Account 服务地址未配置")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("listen for Account callback: %w", err)
	}
	defer listener.Close()

	state, err := browserAuthorizationState()
	if err != nil {
		return "", err
	}
	callbackURL := "http://" + listener.Addr().String() + "/account/callback"
	result := make(chan browserAuthorizationResult, 1)
	var complete sync.Once

	mux := http.NewServeMux()
	mux.HandleFunc("/account/callback", func(response http.ResponseWriter, request *http.Request) {
		_ = request.ParseForm()
		callbackResult := browserAuthorizationResult{}
		switch {
		case request.Form.Get("state") != state:
			callbackResult.err = fmt.Errorf("%w: callback state mismatch", errAccountRejected)
		case request.Form.Get("error") == "access_denied":
			callbackResult.err = errAccountAuthorizationCanceled
		case request.Form.Get("error") == "expired":
			callbackResult.err = errAccountAuthorizationExpired
		case request.Form.Get("error") != "":
			callbackResult.err = fmt.Errorf("%w: %s", errAccountRejected, request.Form.Get("error"))
		default:
			callbackResult.token = request.Form.Get("identity_token")
			if callbackResult.token == "" {
				callbackResult.err = fmt.Errorf("%w: callback has no identity token", errAccountRejected)
			}
		}
		complete.Do(func() { result <- callbackResult })
		response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = response.Write([]byte("Account 登录已完成，可以关闭此页面。"))
	})

	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	defer server.Shutdown(context.Background())
	go func() { _ = server.Serve(listener) }()

	authorizationURL, err := url.Parse(a.accountBaseURL + "/v1/account/apple/authorize")
	if err != nil {
		return "", err
	}
	query := authorizationURL.Query()
	query.Set("redirect_uri", callbackURL)
	query.Set("state", state)
	query.Set("endpoint", "host")
	authorizationURL.RawQuery = query.Encode()
	if err := a.openURL(authorizationURL.String()); err != nil {
		return "", err
	}

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case callback := <-result:
		return callback.token, callback.err
	}
}

func browserAuthorizationState() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("create Account callback state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
