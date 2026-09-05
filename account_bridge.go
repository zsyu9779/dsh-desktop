package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	accountBridgeSessionFile  = "session.json"
	accountBridgePollInterval = 800 * time.Millisecond
)

// bridgeSession 由 dsh 设置页登录插件（dsh-account-login）写入。字段与 JSON 键
// 大小写不敏感匹配（accountID/accessToken/expiresAt/signedOut）。
type bridgeSession struct {
	AccountID   string
	AccessToken string
	ExpiresAt   time.Time
	SignedOut   bool
}

// accountBridgeDir 返回桌面壳与 dsh 插件共享的会话桥目录。桌面壳把它与一次性
// 写方密钥（DSH_ACCOUNT_BRIDGE_SECRET）通过 env 传给 dsh 子进程（见 dsh.go
// buildCommand 的 env 注入）。
func accountBridgeDir() string {
	return filepath.Join(stateDir(), "account-bridge")
}

// accountBridgeSecret 每次启动随机生成一次，作为 session.json 的写方认证密钥：
// 仅知道该 secret 的插件（dsh 子进程）能写出被采用的会话，防同用户其它进程伪造。
var accountBridgeSecret = sync.OnceValue(func() string {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return ""
	}
	return hex.EncodeToString(random)
})

// signBridgePayload 返回 payload + "\n" + hmacHex(payload) 两行格式。
func signBridgePayload(payload []byte, secret string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	return append(append(append([]byte{}, payload...), '\n'), []byte(hex.EncodeToString(mac.Sum(nil)))...)
}

// verifyBridgeFile 校验两行格式（payload + HMAC 行）并返回 payload；失败返回 nil。
func verifyBridgeFile(raw []byte, secret string) []byte {
	if secret == "" {
		return nil
	}
	newline := bytes.IndexByte(raw, '\n')
	if newline <= 0 || newline == len(raw)-1 {
		return nil
	}
	payload := raw[:newline]
	macLine := bytes.TrimSpace(raw[newline+1:])
	expected, err := hex.DecodeString(string(macLine))
	if err != nil {
		return nil
	}
	computed := hmac.New(sha256.New, []byte(secret))
	_, _ = computed.Write(payload)
	if !hmac.Equal(expected, computed.Sum(nil)) {
		return nil
	}
	return payload
}

// bridgeStatus 桌面壳写出的账号/Relay 状态，供 dsh 设置页插件轮询显示“已登录”。
// 非密文、不参与会话 HMAC；仅 0600 + 原子替换。
type bridgeStatus struct {
	State      string `json:"state"`
	AccountID  string `json:"accountID,omitempty"`
	HostID     string `json:"hostID,omitempty"`
	Message    string `json:"message,omitempty"`
	RelayState string `json:"relayState,omitempty"`
}

func writeAccountBridgeStatus(status accountStatus, relayState string) {
	dir := accountBridgeDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	payload, err := json.Marshal(bridgeStatus{
		State: string(status.State), AccountID: status.AccountID, HostID: status.HostID,
		Message: status.Message, RelayState: relayState,
	})
	if err != nil {
		return
	}
	target := filepath.Join(dir, "status.json")
	temporary := target + ".tmp"
	if err := os.WriteFile(temporary, payload, 0o600); err != nil {
		return
	}
	_ = os.Rename(temporary, target)
}

// refreshAccountBridgeStatus 把当前账号与 Relay 状态刷到桥目录 status.json。
func (a *App) refreshAccountBridgeStatus() {
	relayState := ""
	if a.relay != nil {
		relayState = string(a.relay.status().State)
	}
	writeAccountBridgeStatus(a.account.currentStatus(), relayState)
}

// accountBridgeConsumer 由 App 实现，接收桥会话并执行登录/退出副作用。
type accountBridgeConsumer interface {
	adoptExternalCredential(accountCredential) accountStatus
	onBridgeSignedIn(accountStatus)
	onBridgeSignedOut()
	bridgeAdoptFailed(accountStatus)
}

// watchAccountBridge 轮询桥目录并消费插件写下的 session.json。
//   - 启动先清掉陈旧文件（防重启竞态覆盖 keyring 轨）；
//   - 仅接受带 HMAC 的会话（写方认证）；
//   - 采用成功才删除；瞬时失败保留文件下轮重试，直到凭据过期/损坏；
//   - 失败状态照常上报 UI。
func watchAccountBridge(ctx context.Context, consumer accountBridgeConsumer, secret string) {
	dir := accountBridgeDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		log.Printf("account-bridge: mkdir %s: %v", dir, err)
		return
	}
	_ = os.Remove(filepath.Join(dir, accountBridgeSessionFile))
	ticker := time.NewTicker(accountBridgePollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			path := filepath.Join(dir, accountBridgeSessionFile)
			raw, err := os.ReadFile(path)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				continue
			}
			payload := verifyBridgeFile(raw, secret)
			if payload == nil {
				log.Printf("account-bridge: rejecting session with invalid signature")
				_ = os.Remove(path)
				continue
			}
			var session bridgeSession
			if err := json.Unmarshal(payload, &session); err != nil {
				log.Printf("account-bridge: malformed session payload")
				_ = os.Remove(path)
				continue
			}
			if session.SignedOut {
				_ = os.Remove(path)
				consumer.onBridgeSignedOut()
				continue
			}
			if session.AccountID == "" || session.AccessToken == "" || session.ExpiresAt.IsZero() {
				log.Printf("account-bridge: invalid session content, dropping")
				_ = os.Remove(path)
				continue
			}
			if time.Now().After(session.ExpiresAt) {
				_ = os.Remove(path)
				continue
			}
			status := consumer.adoptExternalCredential(accountCredential{
				AccountID:   session.AccountID,
				AccessToken: session.AccessToken,
				ExpiresAt:   session.ExpiresAt,
			})
			if status.State == accountStateSignedIn {
				// 采用成功即消费，避免反复重放。
				_ = os.Remove(path)
				consumer.onBridgeSignedIn(status)
			} else {
				// 瞬时失败（如登记 Host 时网络不可用）：保留文件下轮重试并上报。
				consumer.bridgeAdoptFailed(status)
			}
		}
	}
}
