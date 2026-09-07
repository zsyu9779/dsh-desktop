package main

import (
	"context"
	"crypto/ecdh"
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

	identityKeyTypeX25519  = "x25519"
	identityKeyTypeEd25519 = "ed25519"
	x25519PrivateKeySize   = 32
)

var (
	errAccountSecretNotFound        = errors.New("Account secret not found")
	errAccountAuthorizationCanceled = errors.New("Account authorization canceled")
	errAccountAuthorizationExpired  = errors.New("Account authorization expired")
	errAccountRejected              = errors.New("Account request rejected")
	errInvalidHostIdentity          = errors.New("Host identity 格式无效")
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

// hostAccountMigration 描述一次 Host endpoint identity key 迁移：用旧 key 证明当前
// key，再把 endpoint 的 identity public key 替换为新的 X25519 key。endpoint ID
// （HostID）保持不变，所以既有 Pairing 无需重新扫码。
type hostAccountMigration struct {
	HostID               string `json:"hostID"`
	OldIdentityPublicKey string `json:"oldIdentityPublicKey"`
	NewIdentityPublicKey string `json:"newIdentityPublicKey"`
}

type accountAuthorizer interface {
	authorize(context.Context) (string, error)
}

type accountServer interface {
	authenticate(context.Context, string) (accountCredential, error)
	registerHost(context.Context, string, hostAccountRegistration) error
	migrateHost(context.Context, string, hostAccountMigration) error
}

type accountSecretStore interface {
	get(string) (string, error)
	set(string, string) error
	delete(string) error
}

type hostAccountIdentity struct {
	KeyType            string          `json:"keyType,omitempty"`
	HostID             string          `json:"hostID"`
	PrivateKey         string          `json:"privateKey"`
	LegacyPrivateKey   string          `json:"legacyPrivateKey,omitempty"`
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
	identity, err = m.migrateLegacyIdentity(ctx, credential, identity)
	if err != nil {
		return m.failFor(err, "Host 身份迁移")
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
		if identity.isX25519() || identity.isEd25519() {
			return identity, nil
		}
		return hostAccountIdentity{}, errInvalidHostIdentity
	}
	if !errors.Is(err, errAccountSecretNotFound) {
		return hostAccountIdentity{}, err
	}
	return m.createX25519Identity()
}

// newX25519IdentityKey 生成 X25519 key，返回 base64 编码的 private key 与原始
// public key 字节。
func newX25519IdentityKey() (privateKey string, publicKey []byte, err error) {
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", nil, err
	}
	return base64.RawStdEncoding.EncodeToString(key.Bytes()), key.PublicKey().Bytes(), nil
}

// createX25519Identity 生成并持久化新的 X25519 key-agreement identity。HostID 由
// X25519 public key 的 SHA-256 派生，private key 仅以 base64 持久化到 secure store。
func (m *accountManager) createX25519Identity() (hostAccountIdentity, error) {
	privateKey, publicKey, err := newX25519IdentityKey()
	if err != nil {
		return hostAccountIdentity{}, err
	}
	digest := sha256.Sum256(publicKey)
	identity := hostAccountIdentity{
		KeyType:            identityKeyTypeX25519,
		HostID:             hex.EncodeToString(digest[:]),
		PrivateKey:         privateKey,
		RegisteredAccounts: make(map[string]bool),
	}
	if err := m.saveIdentity(identity); err != nil {
		return hostAccountIdentity{}, err
	}
	return identity, nil
}

// migrateLegacyIdentity 把旧 Ed25519 identity 迁移为 X25519 key-agreement identity。
// 迁移保留原 Host ID；server 迁移确认前，旧 Ed25519 key 保留在 LegacyPrivateKey，
// X25519 key 也会先持久化，使失败重试复用同一 key、不产生新 Host 或要求重新扫码。
func (m *accountManager) migrateLegacyIdentity(ctx context.Context, credential accountCredential, identity hostAccountIdentity) (hostAccountIdentity, error) {
	if identity.isX25519() && identity.LegacyPrivateKey == "" {
		return identity, nil
	}
	migrated, err := identity.asX25519()
	if err != nil {
		return hostAccountIdentity{}, err
	}
	if migrated.RegisteredAccounts[credential.AccountID] {
		oldKey, err := migrated.ed25519PublicKey()
		if err != nil {
			return hostAccountIdentity{}, err
		}
		newKey, err := migrated.x25519PublicKey()
		if err != nil {
			return hostAccountIdentity{}, err
		}
		// 迁移前先持久化 X25519 key（保留旧 Ed25519），确保重试复用同一 key。
		if err := m.saveIdentity(migrated); err != nil {
			return hostAccountIdentity{}, err
		}
		if err := m.server.migrateHost(ctx, credential.AccessToken, hostAccountMigration{
			HostID:               migrated.HostID,
			OldIdentityPublicKey: base64.RawStdEncoding.EncodeToString(oldKey),
			NewIdentityPublicKey: base64.RawStdEncoding.EncodeToString(newKey),
		}); err != nil {
			return hostAccountIdentity{}, err
		}
	}
	migrated.LegacyPrivateKey = ""
	if err := m.saveIdentity(migrated); err != nil {
		return hostAccountIdentity{}, err
	}
	return migrated, nil
}

// isX25519 判断 identity 是否为规范的 X25519 key-agreement identity。
func (i hostAccountIdentity) isX25519() bool {
	if i.KeyType != identityKeyTypeX25519 || i.HostID == "" {
		return false
	}
	key, err := base64.RawStdEncoding.DecodeString(i.PrivateKey)
	return err == nil && len(key) == x25519PrivateKeySize
}

// isEd25519 判断 identity 是否为旧版 Ed25519 identity（KeyType 缺省即旧格式）。
func (i hostAccountIdentity) isEd25519() bool {
	if i.KeyType != "" && i.KeyType != identityKeyTypeEd25519 {
		return false
	}
	key, err := base64.RawStdEncoding.DecodeString(i.PrivateKey)
	return err == nil && len(key) == ed25519.PrivateKeySize
}

// x25519PrivateKey 从 secure store 中的 identity 派生 X25519 private key 供 Relay
// 握手使用；private key 绝不进入日志、QR 或 server。
func (i hostAccountIdentity) x25519PrivateKey() ([]byte, error) {
	if !i.isX25519() {
		return nil, errors.New("Host identity 尚未迁移为 X25519")
	}
	return base64.RawStdEncoding.DecodeString(i.PrivateKey)
}

func (i hostAccountIdentity) x25519PublicKey() ([]byte, error) {
	privateKey, err := i.x25519PrivateKey()
	if err != nil {
		return nil, err
	}
	key, err := ecdh.X25519().NewPrivateKey(privateKey)
	if err != nil {
		return nil, err
	}
	return key.PublicKey().Bytes(), nil
}

// ed25519PublicKey 返回旧 Ed25519 public key（迁移用 old key）：旧格式取自
// PrivateKey，迁移中的 X25519 形态取自 LegacyPrivateKey。
func (i hostAccountIdentity) ed25519PublicKey() ([]byte, error) {
	encoded := i.PrivateKey
	if i.isX25519() {
		encoded = i.LegacyPrivateKey
	}
	if encoded == "" {
		return nil, errors.New("Host identity 缺少旧 Ed25519 key")
	}
	privateKey, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil || len(privateKey) != ed25519.PrivateKeySize {
		return nil, errors.New("Host identity 的 Ed25519 key 无效")
	}
	return ed25519.PrivateKey(privateKey).Public().(ed25519.PublicKey), nil
}

// asX25519 基于现有 identity 生成 X25519 形态并保留原 Host ID；旧 Ed25519 key
// 暂存于 LegacyPrivateKey 直到 server 迁移确认。
func (i hostAccountIdentity) asX25519() (hostAccountIdentity, error) {
	if i.isX25519() {
		return i, nil
	}
	if !i.isEd25519() {
		return hostAccountIdentity{}, errInvalidHostIdentity
	}
	privateKey, _, err := newX25519IdentityKey()
	if err != nil {
		return hostAccountIdentity{}, err
	}
	return hostAccountIdentity{
		KeyType:            identityKeyTypeX25519,
		HostID:             i.HostID,
		PrivateKey:         privateKey,
		LegacyPrivateKey:   i.PrivateKey,
		RegisteredAccounts: i.RegisteredAccounts,
	}, nil
}

func (i hostAccountIdentity) registration() (hostAccountRegistration, error) {
	publicKey, err := i.x25519PublicKey()
	if err != nil {
		return hostAccountRegistration{}, errors.New("invalid identity private key")
	}
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
