package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"sync"
	"time"

	"github.com/skip2/go-qrcode"
)

const remotePairingsSecret = "host-remote-pairings"

var errRemoteSetupNotPending = errors.New("Remote setup 没有待处理的 Pairing challenge")

type remoteSetupState string

const (
	remoteSetupIdle         remoteSetupState = "idle"
	remoteSetupPending      remoteSetupState = "pending"
	remoteSetupCompleted    remoteSetupState = "completed"
	remoteSetupExpired      remoteSetupState = "expired"
	remoteSetupCanceled     remoteSetupState = "canceled"
	remoteSetupCancelFailed remoteSetupState = "cancel-failed"
)

type remoteSetupChallenge struct {
	ID        string    `json:"id"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type pairedDevice struct {
	PairingID               string    `json:"pairingID"`
	DeviceID                string    `json:"deviceID"`
	LANDeviceID             string    `json:"lanDeviceID,omitempty"`
	Name                    string    `json:"name"`
	DeviceIdentityPublicKey []byte    `json:"deviceIdentityPublicKey,omitempty"`
	PairedAt                time.Time `json:"pairedAt,omitempty"`
}

type remoteSetupResult struct {
	State   remoteSetupState `json:"state"`
	Pairing pairedDevice     `json:"pairing,omitempty"`
}

type remoteSetupStatus struct {
	State       remoteSetupState `json:"state"`
	ChallengeID string           `json:"challengeID,omitempty"`
	ExpiresAt   time.Time        `json:"expiresAt,omitempty"`
	QRPayload   string           `json:"qrPayload,omitempty"`
	QR          string           `json:"qr,omitempty"`
	Devices     []pairedDevice   `json:"devices"`
	Message     string           `json:"message"`
}

type lanPairingIntent struct {
	HostID           string `json:"hostID"`
	DeviceID         string `json:"deviceID"`
	DeviceName       string `json:"deviceName"`
	HostLANPublicKey string `json:"hostLANPublicKey"`
	Nonce            string `json:"nonce"`
	HostProof        []byte `json:"hostProof"`
}

func (i lanPairingIntent) signingBytes() []byte {
	return []byte(i.HostID + "\x00" + i.DeviceID + "\x00" + i.DeviceName + "\x00" + i.HostLANPublicKey + "\x00" + i.Nonce)
}

type lanPairingProofSource interface {
	lanPublicKey(string) (string, error)
	signLANPairing(string, []byte) ([]byte, error)
}

type remoteSetupServer interface {
	createChallenge(context.Context, string, string) (remoteSetupChallenge, error)
	challengeResult(context.Context, string, string) (remoteSetupResult, error)
	cancelChallenge(context.Context, string, string) error
	registerLANPairing(context.Context, string, lanPairingIntent) (pairedDevice, error)
	stageLANPairing(context.Context, string, lanPairingIntent) error
	confirmPairing(context.Context, string, string, pairedDevice) error
}

func (m *remoteSetupManager) completeLANPairing(ctx context.Context, lanDeviceID string, paired pairedDevice) error {
	credential, identity, err := m.account.remoteIdentity()
	if err != nil {
		return err
	}
	if err := m.server.confirmPairing(ctx, credential.AccessToken, identity.HostID, paired); err != nil {
		return err
	}
	paired.LANDeviceID = lanDeviceID
	paired.PairedAt = m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.devices[paired.PairingID] = paired
	m.state = remoteSetupCompleted
	return m.saveDevicesLocked()
}

func (m *remoteSetupManager) stageLANProof(ctx context.Context, lanDeviceID string, challenge []byte) (hostAccountIdentity, []byte, error) {
	if lanDeviceID == "" || len(challenge) == 0 || m.lanProof == nil {
		return hostAccountIdentity{}, nil, errors.New("LAN Pairing proof unavailable")
	}
	credential, identity, err := m.account.remoteIdentity()
	if err != nil {
		return hostAccountIdentity{}, nil, err
	}
	intent := lanPairingIntent{
		HostID: identity.HostID, DeviceID: lanDeviceID, DeviceName: "LAN Device",
		Nonce: base64.RawURLEncoding.EncodeToString(challenge),
	}
	intent.HostLANPublicKey, err = m.lanProof.lanPublicKey(lanDeviceID)
	if err == nil {
		intent.HostProof, err = m.lanProof.signLANPairing(lanDeviceID, intent.signingBytes())
	}
	if err == nil {
		err = m.server.stageLANPairing(ctx, credential.AccessToken, intent)
	}
	return identity, intent.HostProof, err
}

type remoteSetupManager struct {
	mu        sync.Mutex
	account   *accountManager
	server    remoteSetupServer
	lanProof  lanPairingProofSource
	secrets   accountSecretStore
	now       func() time.Time
	state     remoteSetupState
	challenge remoteSetupChallenge
	devices   map[string]pairedDevice
}

func newRemoteSetupManager(account *accountManager, server remoteSetupServer, secrets accountSecretStore, now func() time.Time, lanProof ...lanPairingProofSource) *remoteSetupManager {
	m := &remoteSetupManager{account: account, server: server, secrets: secrets, now: now, state: remoteSetupIdle, devices: make(map[string]pairedDevice)}
	if len(lanProof) > 0 {
		m.lanProof = lanProof[0]
	}
	m.restoreDevices()
	if len(m.devices) > 0 {
		m.state = remoteSetupCompleted
	}
	return m
}

func (m *remoteSetupManager) start(ctx context.Context) (remoteSetupStatus, error) {
	m.mu.Lock()
	if m.state == remoteSetupPending && m.now().Before(m.challenge.ExpiresAt) {
		status := m.statusLocked()
		m.mu.Unlock()
		return status, nil
	}
	if m.state == remoteSetupCancelFailed {
		m.mu.Unlock()
		return remoteSetupStatus{}, errors.New("旧 Pairing challenge 尚未在 server 失效，请先重试取消")
	}
	m.mu.Unlock()

	credential, identity, err := m.account.remoteIdentity()
	if err != nil {
		return remoteSetupStatus{}, err
	}
	challenge, err := m.server.createChallenge(ctx, credential.AccessToken, identity.HostID)
	if err != nil {
		return remoteSetupStatus{}, err
	}
	if challenge.ID == "" || challenge.Token == "" || !challenge.ExpiresAt.After(m.now()) {
		return remoteSetupStatus{}, errors.New("Pairing server 返回了无效 challenge")
	}
	m.mu.Lock()
	m.challenge = challenge
	m.state = remoteSetupPending
	status := m.statusLocked()
	m.mu.Unlock()
	return status, nil
}

func (m *remoteSetupManager) refresh(ctx context.Context) (remoteSetupStatus, error) {
	m.mu.Lock()
	m.expireLocked()
	if m.state != remoteSetupPending && m.state != remoteSetupCancelFailed {
		m.mu.Unlock()
		return remoteSetupStatus{}, errRemoteSetupNotPending
	}
	challengeID := m.challenge.ID
	m.mu.Unlock()
	credential, _, err := m.account.remoteIdentity()
	if err != nil {
		return remoteSetupStatus{}, err
	}
	result, err := m.server.challengeResult(ctx, credential.AccessToken, challengeID)
	if err != nil {
		return remoteSetupStatus{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state != remoteSetupPending || m.challenge.ID != challengeID {
		return remoteSetupStatus{}, errRemoteSetupNotPending
	}
	switch result.State {
	case remoteSetupCompleted:
		if result.Pairing.PairingID == "" || result.Pairing.DeviceID == "" {
			return remoteSetupStatus{}, errors.New("Pairing server 返回了无效 Pairing")
		}
		m.devices[result.Pairing.PairingID] = result.Pairing
		if err := m.saveDevicesLocked(); err != nil {
			return remoteSetupStatus{}, err
		}
		m.state = remoteSetupCompleted
		m.challenge = remoteSetupChallenge{}
	case remoteSetupExpired:
		m.state = remoteSetupExpired
		m.challenge = remoteSetupChallenge{}
	case remoteSetupCanceled:
		m.state = remoteSetupCanceled
		m.challenge = remoteSetupChallenge{}
	case remoteSetupPending, "":
	default:
		return remoteSetupStatus{}, fmt.Errorf("未知 Remote setup 状态 %q", result.State)
	}
	return m.statusLocked(), nil
}

func (m *remoteSetupManager) cancel(ctx context.Context) (remoteSetupStatus, error) {
	m.mu.Lock()
	m.expireLocked()
	if m.state != remoteSetupPending && m.state != remoteSetupCancelFailed {
		status := m.statusLocked()
		m.mu.Unlock()
		return status, nil
	}
	challengeID := m.challenge.ID
	m.mu.Unlock()
	credential, _, err := m.account.remoteIdentity()
	if err != nil {
		return remoteSetupStatus{}, err
	}
	if err := m.server.cancelChallenge(ctx, credential.AccessToken, challengeID); err != nil {
		m.mu.Lock()
		m.state = remoteSetupCancelFailed
		status := m.statusLocked()
		m.mu.Unlock()
		return status, err
	}
	m.mu.Lock()
	m.state = remoteSetupCanceled
	m.challenge = remoteSetupChallenge{}
	status := m.statusLocked()
	m.mu.Unlock()
	return status, nil
}

func (m *remoteSetupManager) registerLAN(ctx context.Context, deviceID, deviceName string) (remoteSetupStatus, error) {
	if deviceID == "" || m.lanProof == nil {
		return remoteSetupStatus{}, errors.New("LAN Pairing 缺少 Device 身份")
	}
	credential, identity, err := m.account.remoteIdentity()
	if err != nil {
		return remoteSetupStatus{}, err
	}
	intent := lanPairingIntent{HostID: identity.HostID, DeviceID: deviceID, DeviceName: deviceName, Nonce: fmt.Sprintf("%d", m.now().UnixNano())}
	intent.HostLANPublicKey, err = m.lanProof.lanPublicKey(deviceID)
	if err == nil {
		intent.HostProof, err = m.lanProof.signLANPairing(deviceID, intent.signingBytes())
	}
	if err != nil {
		return remoteSetupStatus{}, err
	}
	paired, err := m.server.registerLANPairing(ctx, credential.AccessToken, intent)
	if err != nil {
		return remoteSetupStatus{}, err
	}
	if paired.PairingID == "" || paired.DeviceID == "" {
		return remoteSetupStatus{}, errors.New("Pairing server 返回了无效 LAN Pairing")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.devices[paired.PairingID] = paired
	m.state = remoteSetupCompleted
	if err := m.saveDevicesLocked(); err != nil {
		return remoteSetupStatus{}, err
	}
	return m.statusLocked(), nil
}

func (m *remoteSetupManager) status() remoteSetupStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireLocked()
	return m.statusLocked()
}

func (m *remoteSetupManager) expireLocked() {
	if (m.state == remoteSetupPending || m.state == remoteSetupCancelFailed) && !m.now().Before(m.challenge.ExpiresAt) {
		m.state = remoteSetupExpired
		m.challenge = remoteSetupChallenge{}
	}
}

func (m *remoteSetupManager) statusLocked() remoteSetupStatus {
	status := remoteSetupStatus{State: m.state, Devices: m.deviceListLocked()}
	switch m.state {
	case remoteSetupPending:
		status.ChallengeID = m.challenge.ID
		status.ExpiresAt = m.challenge.ExpiresAt
		status.QRPayload = "dsh://pair?v=1&challenge=" + url.QueryEscape(m.challenge.Token)
		png, err := qrcode.Encode(status.QRPayload, qrcode.Medium, 256)
		if err == nil {
			status.QR = "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
		}
		status.Message = "等待 Device 扫码批准"
	case remoteSetupCompleted:
		status.Message = "Remote 已设置"
	case remoteSetupExpired:
		status.Message = "Pairing QR 已过期，可重新生成"
	case remoteSetupCanceled:
		status.Message = "Remote setup 已取消"
	case remoteSetupCancelFailed:
		status.ChallengeID = m.challenge.ID
		status.ExpiresAt = m.challenge.ExpiresAt
		status.Message = "server 尚未确认取消；旧 QR 已隐藏，请重试取消"
	default:
		status.Message = "尚未设置 Remote"
	}
	return status
}

func (m *remoteSetupManager) deviceListLocked() []pairedDevice {
	list := make([]pairedDevice, 0, len(m.devices))
	for _, device := range m.devices {
		list = append(list, device)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].DeviceID < list[j].DeviceID })
	return list
}

// relayPairings 返回可经 Relay 复用的 Pairing 快照，供 Host Relay 握手派生密钥。
// 只保留携带 X25519 Device 身份公钥的 Pairing；纯 LAN 时代登记的 Pairing 没有
// identity key（DeviceIdentityPublicKey 为空），无法做 E2E，会被过滤。
func (m *remoteSetupManager) relayPairings() []relayPairing {
	m.mu.Lock()
	defer m.mu.Unlock()
	pairings := make([]relayPairing, 0, len(m.devices))
	for _, device := range m.devices {
		if device.PairingID == "" || device.DeviceID == "" || len(device.DeviceIdentityPublicKey) != 32 {
			continue
		}
		pairings = append(pairings, relayPairing{
			PairingID:       device.PairingID,
			DeviceID:        device.DeviceID,
			DevicePublicKey: append([]byte(nil), device.DeviceIdentityPublicKey...),
			Name:            device.Name,
		})
	}
	return pairings
}

// applyRevocations removes server-revoked Pairings from durable Host state and
// returns the affected Devices so the LAN registry can revoke them as well.
func (m *remoteSetupManager) applyRevocations(pairingIDs []string) []pairedDevice {
	m.mu.Lock()
	defer m.mu.Unlock()
	removed := make([]pairedDevice, 0, len(pairingIDs))
	for _, pairingID := range pairingIDs {
		if device, ok := m.devices[pairingID]; ok {
			removed = append(removed, device)
			delete(m.devices, pairingID)
		}
	}
	if len(removed) > 0 {
		_ = m.saveDevicesLocked()
		if len(m.devices) == 0 && m.state == remoteSetupCompleted {
			m.state = remoteSetupIdle
		}
	}
	return removed
}

func (m *remoteSetupManager) applyPairingSync(pairings []relayPairingSync) {
	m.mu.Lock()
	defer m.mu.Unlock()
	changed := false
	for _, pairing := range pairings {
		if pairing.PairingID == "" || pairing.DeviceID == "" || len(pairing.DeviceIdentityPublicKey) != 32 {
			continue
		}
		if existing, exists := m.devices[pairing.PairingID]; exists {
			if pairing.LANDeviceID != "" && existing.LANDeviceID != pairing.LANDeviceID {
				existing.LANDeviceID = pairing.LANDeviceID
				m.devices[pairing.PairingID] = existing
				changed = true
			}
			continue
		}
		m.devices[pairing.PairingID] = pairedDevice{
			PairingID: pairing.PairingID, DeviceID: pairing.DeviceID, Name: "Device",
			LANDeviceID: pairing.LANDeviceID, DeviceIdentityPublicKey: append([]byte(nil), pairing.DeviceIdentityPublicKey...), PairedAt: m.now(),
		}
		changed = true
	}
	if changed {
		m.state = remoteSetupCompleted
		_ = m.saveDevicesLocked()
	}
}

func (m *remoteSetupManager) restoreDevices() {
	encoded, err := m.secrets.get(remotePairingsSecret)
	if err != nil {
		return
	}
	var devices []pairedDevice
	if json.Unmarshal([]byte(encoded), &devices) != nil {
		return
	}
	for _, device := range devices {
		if device.PairingID != "" && device.DeviceID != "" {
			m.devices[device.PairingID] = device
		}
	}
}

func (m *remoteSetupManager) saveDevicesLocked() error {
	encoded, err := json.Marshal(m.deviceListLocked())
	if err != nil {
		return err
	}
	return m.secrets.set(remotePairingsSecret, string(encoded))
}

func (m *accountManager) remoteIdentity() (accountCredential, hostAccountIdentity, error) {
	m.operationMu.Lock()
	defer m.operationMu.Unlock()
	credential, err := m.loadCredential()
	if err != nil {
		return accountCredential{}, hostAccountIdentity{}, errors.New("Host 尚未登录 Account")
	}
	if !credential.ExpiresAt.IsZero() && time.Now().After(credential.ExpiresAt) {
		return accountCredential{}, hostAccountIdentity{}, errors.New("Account credential 已过期")
	}
	identity, err := m.loadIdentity()
	return credential, identity, err
}
