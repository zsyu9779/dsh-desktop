package main

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// legacyEd25519Identity 构造一个 ticket-02 时代持久化的 Ed25519 identity（无
// KeyType 字段），HostID 由 Ed25519 public key 的 SHA-256 派生。
func legacyEd25519Identity(t *testing.T) hostAccountIdentity {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(publicKey)
	return hostAccountIdentity{
		HostID:             hex.EncodeToString(digest[:]),
		PrivateKey:         base64.RawStdEncoding.EncodeToString(privateKey),
		RegisteredAccounts: map[string]bool{"account-a": true},
	}
}

func seedIdentity(t *testing.T, store accountSecretStore, identity hostAccountIdentity) {
	t.Helper()
	encoded, err := json.Marshal(identity)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.set(accountIdentitySecret, string(encoded)); err != nil {
		t.Fatal(err)
	}
}

func TestNewHostIdentityIsX25519(t *testing.T) {
	store := newMemorySecretStore()
	server := &fakeAccountServer{credential: validAccountCredential()}
	manager := newAccountManager(fakeAccountAuthorizer{token: "apple-token"}, server, store)

	status := manager.signIn(context.Background())
	if status.State != accountStateSignedIn {
		t.Fatalf("state = %q, want signed-in (message: %s)", status.State, status.Message)
	}
	identity, err := manager.loadIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if !identity.isX25519() {
		t.Fatalf("new identity KeyType = %q, want X25519", identity.KeyType)
	}
	if identity.isEd25519() {
		t.Fatal("new identity must not be Ed25519")
	}
	publicKey, err := identity.x25519PublicKey()
	if err != nil || len(publicKey) != x25519PrivateKeySize {
		t.Fatalf("x25519PublicKey() = (%x, %v), want 32-byte public key", publicKey, err)
	}
	digest := sha256.Sum256(publicKey)
	if identity.HostID != hex.EncodeToString(digest[:]) {
		t.Fatalf("HostID = %q, want sha256(publicKey)", identity.HostID)
	}
	if server.registration.IdentityPublicKey != base64.RawStdEncoding.EncodeToString(publicKey) {
		t.Fatalf("registered IdentityPublicKey = %q, want %q", server.registration.IdentityPublicKey, base64.RawStdEncoding.EncodeToString(publicKey))
	}
	if server.migrateCount != 0 {
		t.Fatalf("migrateCount = %d, want 0 for new identity", server.migrateCount)
	}
}

func TestLegacyEd25519IdentityMigratesToX25519PreservingHostID(t *testing.T) {
	store := newMemorySecretStore()
	legacy := legacyEd25519Identity(t)
	seedIdentity(t, store, legacy)
	server := &fakeAccountServer{credential: validAccountCredential()}
	manager := newAccountManager(fakeAccountAuthorizer{token: "apple-token"}, server, store)

	status := manager.signIn(context.Background())
	if status.State != accountStateSignedIn {
		t.Fatalf("state = %q, want signed-in (message: %s)", status.State, status.Message)
	}
	if status.HostID != legacy.HostID {
		t.Fatalf("HostID = %q, want preserved %q", status.HostID, legacy.HostID)
	}
	if server.migrateCount != 1 {
		t.Fatalf("migrateCount = %d, want 1", server.migrateCount)
	}
	oldKey, err := legacy.ed25519PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	if server.migration.HostID != legacy.HostID {
		t.Fatalf("migration HostID = %q, want %q", server.migration.HostID, legacy.HostID)
	}
	if server.migration.OldIdentityPublicKey != base64.RawStdEncoding.EncodeToString(oldKey) {
		t.Fatalf("migration old key = %q, want legacy Ed25519 public key", server.migration.OldIdentityPublicKey)
	}

	migrated, err := manager.loadIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if !migrated.isX25519() {
		t.Fatalf("migrated identity KeyType = %q, want X25519", migrated.KeyType)
	}
	if migrated.HostID != legacy.HostID {
		t.Fatalf("migrated HostID = %q, want preserved %q", migrated.HostID, legacy.HostID)
	}
	if migrated.isEd25519() {
		t.Fatal("migrated identity still Ed25519")
	}
	newPublicKey, err := migrated.x25519PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	if server.migration.NewIdentityPublicKey != base64.RawStdEncoding.EncodeToString(newPublicKey) {
		t.Fatalf("migration new key = %q, want migrated X25519 public key", server.migration.NewIdentityPublicKey)
	}
	if server.registrationCount != 0 {
		t.Fatalf("registrationCount = %d, want 0 (already registered)", server.registrationCount)
	}
}

func TestMigrationFailurePreservesLegacyIdentityAndRetries(t *testing.T) {
	store := newMemorySecretStore()
	legacy := legacyEd25519Identity(t)
	seedIdentity(t, store, legacy)
	server := &fakeAccountServer{credential: validAccountCredential(), migrateErr: temporaryNetworkError{}}
	manager := newAccountManager(fakeAccountAuthorizer{token: "apple-token"}, server, store)

	status := manager.signIn(context.Background())
	if status.State != accountStateNetworkError {
		t.Fatalf("state = %q, want network-error (message: %s)", status.State, status.Message)
	}
	if !status.Retryable {
		t.Fatal("migration failure must be retryable")
	}
	// 失败后：X25519 key 已持久化、旧 Ed25519 key 保留在 LegacyPrivateKey，HostID 不变。
	persisted, err := manager.loadIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if !persisted.isX25519() {
		t.Fatalf("identity after failed migration = %#v, want in-progress X25519", persisted)
	}
	if persisted.LegacyPrivateKey == "" {
		t.Fatal("legacy Ed25519 key was dropped before migration succeeded")
	}
	if persisted.HostID != legacy.HostID {
		t.Fatalf("HostID changed on failed migration: %q, want %q", persisted.HostID, legacy.HostID)
	}
	inProgressKey, err := persisted.x25519PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	if server.migrateCount != 1 {
		t.Fatalf("migrateCount = %d, want 1", server.migrateCount)
	}

	server.migrateErr = nil
	retry := manager.signIn(context.Background())
	if retry.State != accountStateSignedIn {
		t.Fatalf("retry state = %q, want signed-in (message: %s)", retry.State, retry.Message)
	}
	if retry.HostID != legacy.HostID {
		t.Fatalf("retry HostID = %q, want preserved %q", retry.HostID, legacy.HostID)
	}
	if server.migrateCount != 2 {
		t.Fatalf("migrateCount = %d, want 2", server.migrateCount)
	}
	migrated, err := manager.loadIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if !migrated.isX25519() {
		t.Fatalf("identity after retry = %#v, want X25519", migrated)
	}
	if migrated.LegacyPrivateKey != "" {
		t.Fatal("LegacyPrivateKey not cleared after successful migration")
	}
	// 重试复用同一 X25519 key，不产生新 Host 或新 key。
	finalKey, err := migrated.x25519PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(inProgressKey, finalKey) {
		t.Fatal("X25519 key changed across retry")
	}
}

func TestX25519IdentityReusedAcrossRestart(t *testing.T) {
	store := newMemorySecretStore()
	server := &fakeAccountServer{credential: validAccountCredential()}
	first := newAccountManager(fakeAccountAuthorizer{token: "first-token"}, server, store)
	firstStatus := first.signIn(context.Background())
	if firstStatus.State != accountStateSignedIn {
		t.Fatalf("first sign-in failed: %+v", firstStatus)
	}
	firstIdentity, err := first.loadIdentity()
	if err != nil {
		t.Fatal(err)
	}
	firstPublicKey, err := firstIdentity.x25519PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	first.signOut()

	restarted := newAccountManager(fakeAccountAuthorizer{token: "second-token"}, server, store)
	secondStatus := restarted.signIn(context.Background())
	if secondStatus.State != accountStateSignedIn {
		t.Fatalf("restart sign-in failed: %+v", secondStatus)
	}
	if secondStatus.HostID != firstStatus.HostID {
		t.Fatalf("restart HostID = %q, want %q", secondStatus.HostID, firstStatus.HostID)
	}
	secondIdentity, err := restarted.loadIdentity()
	if err != nil {
		t.Fatal(err)
	}
	secondPublicKey, err := secondIdentity.x25519PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstPublicKey, secondPublicKey) {
		t.Fatal("X25519 public key changed across restart")
	}
	if server.migrateCount != 0 {
		t.Fatalf("migrateCount = %d, want 0", server.migrateCount)
	}
	if server.registrationCount != 1 {
		t.Fatalf("registrationCount = %d, want 1", server.registrationCount)
	}
}

// TestX25519IdentityMatchesRelayV1Vector 用跨仓 relay-v1 向量验证 crypto/ecdh 的
// X25519 key 派生与 iOS Curve25519 / Go curve25519 一致，供 Relay 握手直接复用。
func TestX25519IdentityMatchesRelayV1Vector(t *testing.T) {
	hostPrivateKey, hostPublicKey, devicePublicKey, staticSharedSecret, hostID := loadRelayV1Vector(t)

	identity := hostAccountIdentity{
		KeyType:            identityKeyTypeX25519,
		HostID:             hostID,
		PrivateKey:         base64.RawStdEncoding.EncodeToString(hostPrivateKey),
		RegisteredAccounts: make(map[string]bool),
	}
	derived, err := identity.x25519PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(derived, hostPublicKey) {
		t.Fatalf("X25519 public key = %x, want vector %x", derived, hostPublicKey)
	}

	hostKey, err := ecdh.X25519().NewPrivateKey(hostPrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	deviceKey, err := ecdh.X25519().NewPublicKey(devicePublicKey)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := hostKey.ECDH(deviceKey)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(shared, staticSharedSecret) {
		t.Fatalf("X25519 shared secret = %x, want vector %x", shared, staticSharedSecret)
	}
}

// loadRelayV1Vector 读取跨仓 relay-v1 测试向量，返回 Host/Device X25519 key 与
// 静态共享密钥的原始字节及 HostID。向量字段均为 base64 标准编码。
func loadRelayV1Vector(t *testing.T) (hostPrivateKey, hostPublicKey, devicePublicKey, staticSharedSecret []byte, hostID string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("test-vectors", "relay-v1.json"))
	if err != nil {
		t.Fatalf("read relay-v1 vector: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode relay-v1 vector: %v", err)
	}
	decode := func(name string) []byte {
		var encoded string
		if err := json.Unmarshal(raw[name], &encoded); err != nil {
			t.Fatalf("decode %s: %v", name, err)
		}
		value, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			t.Fatalf("decode %s: %v", name, err)
		}
		return value
	}
	var hostIDValue string
	if err := json.Unmarshal(raw["hostID"], &hostIDValue); err != nil {
		t.Fatalf("decode hostID: %v", err)
	}
	return decode("hostPrivateKey"), decode("hostPublicKey"), decode("devicePublicKey"), decode("staticSharedSecret"), hostIDValue
}
