package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/zalando/go-keyring"
)

const accountKeyringService = "dsh-desktop"

type keyringAccountSecretStore struct{}

func (keyringAccountSecretStore) get(key string) (string, error) {
	value, err := keyring.Get(accountKeyringService, key)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", errAccountSecretNotFound
	}
	return value, err
}

func (keyringAccountSecretStore) set(key, value string) error {
	return keyring.Set(accountKeyringService, key, value)
}

func (keyringAccountSecretStore) delete(key string) error {
	err := keyring.Delete(accountKeyringService, key)
	if errors.Is(err, keyring.ErrNotFound) {
		return errAccountSecretNotFound
	}
	return err
}

type httpAccountServer struct {
	baseURL string
	client  *http.Client
}

func newHTTPAccountServer(baseURL string) *httpAccountServer {
	return &httpAccountServer{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}

func (s *httpAccountServer) authenticate(ctx context.Context, identityToken string) (accountCredential, error) {
	var credential accountCredential
	if err := s.request(ctx, http.MethodPost, "/v1/account/authenticate", "", map[string]string{
		"identityToken": identityToken,
	}, &credential); err != nil {
		return accountCredential{}, err
	}
	return credential, nil
}

func (s *httpAccountServer) registerHost(ctx context.Context, accessToken string, registration hostAccountRegistration) error {
	return s.request(ctx, http.MethodPost, "/v1/account/hosts", accessToken, registration, nil)
}

func (s *httpAccountServer) migrateHost(ctx context.Context, accessToken string, migration hostAccountMigration) error {
	return s.request(ctx, http.MethodPost, "/v1/account/hosts/"+url.PathEscape(migration.HostID)+"/identity", accessToken, migration, nil)
}

func (s *httpAccountServer) request(ctx context.Context, method, path, accessToken string, body, result any) error {
	if s.baseURL == "" {
		return errors.New("Account 服务地址未配置")
	}
	var encoded []byte
	var err error
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	request, err := http.NewRequestWithContext(ctx, method, s.baseURL+path, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	if accessToken != "" {
		request.Header.Set("Authorization", "Bearer "+accessToken)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusConflict && path == "/v1/account/hosts" {
		return nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if response.StatusCode >= 400 && response.StatusCode < 500 {
			return fmt.Errorf("%w: HTTP %d", errAccountRejected, response.StatusCode)
		}
		return fmt.Errorf("Account service: HTTP %d", response.StatusCode)
	}
	if result == nil || response.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(result); err != nil {
		return fmt.Errorf("decode Account response: %w", err)
	}
	return nil
}

func accountServerURL() string {
	return strings.TrimSpace(os.Getenv("DSH_ACCOUNT_SERVER_URL"))
}
