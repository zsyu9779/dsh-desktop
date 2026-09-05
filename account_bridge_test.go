package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const bridgeTestSecret = "test-bridge-secret"

// signedBridgeFile 生成与插件一致的两行格式：payload + HMAC(payload)。
func signedBridgeFile(t *testing.T, payload any, secret string) []byte {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(raw)
	return append(append(append([]byte{}, raw...), '\n'), []byte(hex.EncodeToString(mac.Sum(nil)))...)
}

func writeSignedSession(t *testing.T, dir string, payload any, secret string) {
	t.Helper()
	content := signedBridgeFile(t, payload, secret)
	if err := os.WriteFile(filepath.Join(dir, accountBridgeSessionFile), content, 0o600); err != nil {
		t.Fatal(err)
	}
}

// 插件会话采用：不走 authorizer/authenticate，直接登记 Host 并进入已登录。
func TestAdoptExternalCredentialSignsInHost(t *testing.T) {
	store := newMemorySecretStore()
	server := &fakeAccountServer{}
	manager := newAccountManager(fakeAccountAuthorizer{token: "unused"}, server, store)

	status := manager.adoptExternalCredential(accountCredential{
		AccountID: "plugin-account", AccessToken: "plugin-token", ExpiresAt: time.Now().Add(time.Hour),
	})

	if status.State != accountStateSignedIn {
		t.Fatalf("state = %q (message: %s)", status.State, status.Message)
	}
	if status.AccountID != "plugin-account" {
		t.Fatalf("AccountID = %q", status.AccountID)
	}
	if server.authenticatedToken != "" {
		t.Fatalf("adopt must not call authenticate, got token %q", server.authenticatedToken)
	}
	if server.registration.HostID == "" {
		t.Fatal("Host was not registered under the adopted Account")
	}
	current, err := manager.currentCredential()
	if err != nil || current.AccessToken != "plugin-token" {
		t.Fatalf("persisted credential = %+v err=%v", current, err)
	}
}

func TestAdoptExternalCredentialRejectsInvalidOrExpired(t *testing.T) {
	store := newMemorySecretStore()
	server := &fakeAccountServer{}
	manager := newAccountManager(fakeAccountAuthorizer{}, server, store)

	expired := manager.adoptExternalCredential(accountCredential{
		AccountID: "plugin-account", AccessToken: "plugin-token", ExpiresAt: time.Now().Add(-time.Minute),
	})
	if expired.State != accountStateFailed {
		t.Fatalf("expired adopt state = %q", expired.State)
	}
	if server.registration.HostID != "" {
		t.Fatal("expired session must not register the Host")
	}

	hollow := manager.adoptExternalCredential(accountCredential{AccountID: "", AccessToken: ""})
	if hollow.State != accountStateFailed {
		t.Fatalf("hollow adopt state = %q", hollow.State)
	}
}

func startWatcherFor(t *testing.T, app *App, secret string) (context.CancelFunc, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("DSH_DESKTOP_STATE", dir)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	go watchAccountBridge(ctx, app, secret)
	time.Sleep(100 * time.Millisecond) // 让 watcher 先清掉陈旧文件
	return cancel, accountBridgeDir()
}

func TestWatchAccountBridgeAdoptsAndConsumesSession(t *testing.T) {
	store := newMemorySecretStore()
	server := &fakeAccountServer{}
	app := &App{account: newAccountManager(fakeAccountAuthorizer{token: "unused"}, server, store)}
	cancel, bridgeDir := startWatcherFor(t, app, bridgeTestSecret)
	defer cancel()

	writeSignedSession(t, bridgeDir, map[string]any{
		"accountID": "watched-account", "accessToken": "watched-token",
		"expiresAt": time.Now().Add(time.Hour).Format(time.RFC3339), "signedOut": false,
	}, bridgeTestSecret)

	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if app.account.currentStatus().State == accountStateSignedIn {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	status := app.account.currentStatus()
	if status.State != accountStateSignedIn || status.AccountID != "watched-account" {
		t.Fatalf("status = %+v", status)
	}
	if _, err := os.Stat(filepath.Join(bridgeDir, accountBridgeSessionFile)); !os.IsNotExist(err) {
		t.Fatal("session.json was not consumed")
	}
	if server.registration.HostID == "" {
		t.Fatal("Host was not registered")
	}
}

func TestWatchAccountBridgeHandlesSignOut(t *testing.T) {
	store := newMemorySecretStore()
	credential := accountCredential{AccountID: "account-a", AccessToken: "token-a", ExpiresAt: time.Now().Add(time.Hour)}
	server := &fakeAccountServer{}
	manager := newAccountManager(fakeAccountAuthorizer{token: "unused"}, server, store)
	if status := manager.adoptExternalCredential(credential); status.State != accountStateSignedIn {
		t.Fatalf("precondition sign-in failed: %+v", status)
	}
	app := &App{account: manager}
	cancel, bridgeDir := startWatcherFor(t, app, bridgeTestSecret)
	defer cancel()

	writeSignedSession(t, bridgeDir, map[string]any{"signedOut": true}, bridgeTestSecret)

	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if app.account.currentStatus().State == accountStateSignedOut {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if got := app.account.currentStatus().State; got != accountStateSignedOut {
		t.Fatalf("state = %q, want signed-out", got)
	}
	if _, err := os.Stat(filepath.Join(bridgeDir, accountBridgeSessionFile)); !os.IsNotExist(err) {
		t.Fatal("session.json was not consumed after sign-out")
	}
}

// 伪造（错误密钥/无签名）会话必须被拒：不登录、文件删除。
func TestWatchAccountBridgeRejectsForgedSession(t *testing.T) {
	store := newMemorySecretStore()
	server := &fakeAccountServer{}
	app := &App{account: newAccountManager(fakeAccountAuthorizer{token: "unused"}, server, store)}
	cancel, bridgeDir := startWatcherFor(t, app, bridgeTestSecret)
	defer cancel()

	writeSignedSession(t, bridgeDir, map[string]any{
		"accountID": "forged-account", "accessToken": "forged-token",
		"expiresAt": time.Now().Add(time.Hour).Format(time.RFC3339), "signedOut": false,
	}, "wrong-secret")

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if app.account.currentStatus().State != accountStateSignedOut {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if app.account.currentStatus().State != accountStateSignedOut {
		t.Fatalf("forged session signed the Host in: %+v", app.account.currentStatus())
	}
	if _, err := os.Stat(filepath.Join(bridgeDir, accountBridgeSessionFile)); !os.IsNotExist(err) {
		t.Fatal("forged session.json was not removed")
	}
}

func TestVerifyBridgeFileSignature(t *testing.T) {
	signed := signedBridgeFile(t, map[string]any{"signedOut": true}, bridgeTestSecret)
	payload := verifyBridgeFile(signed, bridgeTestSecret)
	if payload == nil {
		t.Fatal("valid signature rejected")
	}
	if verifyBridgeFile(signed, "wrong-secret") != nil {
		t.Fatal("wrong secret accepted")
	}
	if verifyBridgeFile([]byte("no-newline-here"), bridgeTestSecret) != nil {
		t.Fatal("malformed file accepted")
	}
}
