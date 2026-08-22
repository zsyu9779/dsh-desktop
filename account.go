package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

const (
	accountIdentitySecret   = "host-account-identity"
	accountCredentialSecret = "host-account-credential"
)

var (
	errAccountSecretNotFound        = errors.New("Account secret not found")
	errAccountAuthorizationCanceled = errors.New("Account authorization canceled")
	errAccountAuthorizationExpired  = errors.New("Account authorization expired")
	errAccountRejected              = errors.New("Account request rejected")
)

type accountState string

const (
	accountStateSignedOut    accountState = "signed-out"
	accountStateAuthorizing  accountState = "authorizing"
	accountStateSignedIn     accountState = "signed-in"
	accountStateCanceled     accountState = "canceled"
	accountStateExpired      accountState = "expired"
	accountStateNetworkError accountState = "network-error"
	accountStateRejected     accountState = "rejected"
	accountStateFailed       accountState = "failed"
)

type accountStatus struct {
	State     accountState `json:"state"`
	AccountID string       `json:"accountID,omitempty"`
	HostID    string       `json:"hostID,omitempty"`
	Message   string       `json:"message"`
	Retryable bool         `json:"retryable"`
}

type accountCredential struct {
	AccountID   string    `json:"accountID"`
	AccessToken string    `json:"accessToken"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

type hostAccountRegistration struct {
	HostID            string `json:"hostID"`
	IdentityPublicKey string `json:"identityPublicKey"`
}

type accountAuthorizer interface {
	authorize(context.Context) (string, error)
}

type accountServer interface {
	authenticate(context.Context, string) (accountCredential, error)
	registerHost(context.Context, string, hostAccountRegistration) error
}

type accountSecretStore interface {
	get(string) (string, error)
	set(string, string) error
	delete(string) error
}

type hostAccountIdentity struct {
	HostID             string          `json:"hostID"`
	PrivateKey         string          `json:"privateKey"`
	RegisteredAccounts map[string]bool `json:"registeredAccounts"`
}

type accountManager struct {
	mu          sync.Mutex
	operationMu sync.Mutex
	authorizer  accountAuthorizer
	server      accountServer
	secrets     accountSecretStore
	status      accountStatus
}

func newAccountManager(authorizer accountAuthorizer, server accountServer, secrets accountSecretStore) *accountManager {
	m := &accountManager{
		authorizer: authorizer,
		server:     server,
		secrets:    secrets,
		status:     accountStatus{State: accountStateSignedOut, Message: "Account 未登录"},
	}
	m.restoreStatus()
	return m
}

func (m *accountManager) currentStatus() accountStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

func (m *accountManager) restoreStatus() {
	identity, identityErr := m.loadIdentity()
	if identityErr != nil && !errors.Is(identityErr, errAccountSecretNotFound) {
		m.status = accountStatus{State: accountStateFailed, Message: "无法读取 Host 身份", Retryable: true}
		return
	}
	credential, credentialErr := m.loadCredential()
	if errors.Is(credentialErr, errAccountSecretNotFound) {
		m.status.HostID = identity.HostID
		return
	}
	if credentialErr != nil {
		m.status = accountStatus{State: accountStateFailed, HostID: identity.HostID, Message: "无法读取 Account credential", Retryable: true}
		return
	}
	if !credential.ExpiresAt.IsZero() && time.Now().After(credential.ExpiresAt) {
		_ = m.secrets.delete(accountCredentialSecret)
		m.status = accountStatus{State: accountStateSignedOut, HostID: identity.HostID, Message: "Account credential 已过期", Retryable: true}
		return
	}
	m.status = accountStatus{State: accountStateSignedIn, AccountID: credential.AccountID, HostID: identity.HostID, Message: "Account 已登录"}
}

func (m *accountManager) signIn(ctx context.Context) accountStatus {
	m.operationMu.Lock()
	defer m.operationMu.Unlock()
	m.setStatus(accountStatus{State: accountStateAuthorizing, Message: "正在通过系统浏览器登录 Account"})

	identityToken, err := m.authorizer.authorize(ctx)
	if err != nil {
		return m.failFor(err, "Account 登录")
	}
	credential, err := m.server.authenticate(ctx, identityToken)
	if err != nil {
		return m.failFor(err, "Account 登录")
	}
	if credential.AccountID == "" || credential.AccessToken == "" {
		return m.fail("Account 服务返回了无效登录结果")
	}

	identity, err := m.loadOrCreateIdentity()
	if err != nil {
		return m.fail(fmt.Sprintf("无法使用 Host 身份：%v", err))
	}
	if !identity.RegisteredAccounts[credential.AccountID] {
		registration, err := identity.registration()
		if err != nil {
			return m.fail(fmt.Sprintf("无法读取 Host 身份：%v", err))
		}
		if err := m.server.registerHost(ctx, credential.AccessToken, registration); err != nil {
			return m.failFor(err, "Host 登记")
		}
		identity.RegisteredAccounts[credential.AccountID] = true
		if err := m.saveIdentity(identity); err != nil {
			return m.fail(fmt.Sprintf("无法保存 Host 身份：%v", err))
		}
	}
	if err := m.saveCredential(credential); err != nil {
		return m.fail(fmt.Sprintf("无法保存 Account credential：%v", err))
	}

	return m.setStatus(accountStatus{
		State:     accountStateSignedIn,
		AccountID: credential.AccountID,
		HostID:    identity.HostID,
		Message:   "Account 已登录",
	})
}

func (m *accountManager) fail(message string) accountStatus {
	return m.setStatus(accountStatus{State: accountStateFailed, Message: message, Retryable: true})
}

func (m *accountManager) failFor(err error, operation string) accountStatus {
	status := accountStatus{Retryable: true}
	var networkError net.Error
	switch {
	case errors.Is(err, errAccountAuthorizationCanceled), errors.Is(err, context.Canceled):
		status.State = accountStateCanceled
		status.Message = "Account 登录已取消，可重新尝试"
	case errors.Is(err, errAccountAuthorizationExpired), errors.Is(err, context.DeadlineExceeded):
		status.State = accountStateExpired
		status.Message = "Account 登录已过期，请重新尝试"
	case errors.As(err, &networkError):
		status.State = accountStateNetworkError
		status.Message = "网络不可用，" + operation + "失败，请重试"
	case errors.Is(err, errAccountRejected):
		status.State = accountStateRejected
		status.Message = "Account 服务拒绝了" + operation + "，请重试或重新登录"
	default:
		status.State = accountStateFailed
		status.Message = operation + "失败，请重试"
	}
	return m.setStatus(status)
}

func (m *accountManager) setStatus(status accountStatus) accountStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status = status
	return status
}

func (m *accountManager) signOut() accountStatus {
	m.operationMu.Lock()
	defer m.operationMu.Unlock()
	if err := m.secrets.delete(accountCredentialSecret); err != nil && !errors.Is(err, errAccountSecretNotFound) {
		return m.fail("无法清除 Account credential")
	}
	identity, _ := m.loadIdentity()
	return m.setStatus(accountStatus{State: accountStateSignedOut, HostID: identity.HostID, Message: "Account 已退出"})
}

func (m *accountManager) loadIdentity() (hostAccountIdentity, error) {
	encoded, err := m.secrets.get(accountIdentitySecret)
	if err != nil {
		return hostAccountIdentity{}, err
	}
	var identity hostAccountIdentity
	if err := json.Unmarshal([]byte(encoded), &identity); err != nil {
		return hostAccountIdentity{}, err
	}
	if identity.RegisteredAccounts == nil {
		identity.RegisteredAccounts = make(map[string]bool)
	}
	return identity, nil
}

func (m *accountManager) loadCredential() (accountCredential, error) {
	encoded, err := m.secrets.get(accountCredentialSecret)
	if err != nil {
		return accountCredential{}, err
	}
	var credential accountCredential
	if err := json.Unmarshal([]byte(encoded), &credential); err != nil {
		return accountCredential{}, err
	}
	return credential, nil
}

func (m *accountManager) loadOrCreateIdentity() (hostAccountIdentity, error) {
	identity, err := m.loadIdentity()
	if err == nil {
		return identity, nil
	}
	if !errors.Is(err, errAccountSecretNotFound) {
		return hostAccountIdentity{}, err
	}

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return hostAccountIdentity{}, err
	}
	digest := sha256.Sum256(publicKey)
	identity = hostAccountIdentity{
		HostID:             hex.EncodeToString(digest[:]),
		PrivateKey:         base64.RawStdEncoding.EncodeToString(privateKey),
		RegisteredAccounts: make(map[string]bool),
	}
	if err := m.saveIdentity(identity); err != nil {
		return hostAccountIdentity{}, err
	}
	return identity, nil
}

func (i hostAccountIdentity) registration() (hostAccountRegistration, error) {
	privateKey, err := base64.RawStdEncoding.DecodeString(i.PrivateKey)
	if err != nil || len(privateKey) != ed25519.PrivateKeySize {
		return hostAccountRegistration{}, fmt.Errorf("invalid identity private key")
	}
	publicKey := ed25519.PrivateKey(privateKey).Public().(ed25519.PublicKey)
	return hostAccountRegistration{
		HostID:            i.HostID,
		IdentityPublicKey: base64.RawStdEncoding.EncodeToString(publicKey),
	}, nil
}

func (m *accountManager) saveIdentity(identity hostAccountIdentity) error {
	encoded, err := json.Marshal(identity)
	if err != nil {
		return err
	}
	return m.secrets.set(accountIdentitySecret, string(encoded))
}

func (m *accountManager) saveCredential(credential accountCredential) error {
	encoded, err := json.Marshal(credential)
	if err != nil {
		return err
	}
	return m.secrets.set(accountCredentialSecret, string(encoded))
}
