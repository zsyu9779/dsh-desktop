package main

import (
	"encoding/json"
	"errors"
)

// notificationV1 是 Notification envelope 的版本化 E2E 加密契约标签。
// 一次性推送没有交互式握手，因此密钥由 Pairing 时登记的 X25519 身份密钥做
// 静态 ECDH 派生，再用 HKDF-SHA256 派生 AES-256-GCM 密钥。与 dsh-ios 的
// NotificationEnvelope 逐一对应（见 dsh-ios/Notification/NotificationEnvelope.swift）。
const notificationV1 = "dsh-notification-v1"

// notificationEnvelopeInfo 是 HKDF 的 info 标签，同时充当 AES-GCM 的 AAD。
const notificationEnvelopeInfo = notificationV1 + "/envelope"

// notificationEnvelopeAAD 返回 envelope 加密的附加认证数据。
func notificationEnvelopeAAD() []byte { return []byte(notificationEnvelopeInfo) }

// notificationEnvelopePlaintext 是 Device 解密的 envelope 明文。定位信息只有
// type + sessionID，summary 仅在 app 内解密后展示；APNs 与 Relay 均不可见。
type notificationEnvelopePlaintext struct {
	Version   int    `json:"version"`
	Type      string `json:"type"`
	SessionID string `json:"sessionID"`
	Summary   string `json:"summary"`
}

// notificationEnvelopeContext 派生 envelope 密钥的 HKDF salt。采用与 Relay 握手
// 相同的长度前缀编码，但使用 notification 版本域，且只包含 pairingID/hostID/
// deviceID 三个标识（不含身份公钥）。
func notificationEnvelopeContext(pairingID, hostID, deviceID string) []byte {
	context := append([]byte(nil), []byte(notificationV1+"\x00")...)
	for _, field := range []string{pairingID, hostID, deviceID} {
		context = appendLengthPrefixedField(context, []byte(field))
	}
	return context
}

// notificationEnvelopeKey 由静态 ECDH 共享密钥派生 AES-256-GCM 密钥。
func notificationEnvelopeKey(privateKey, peerPublicKey []byte, pairingID, hostID, deviceID string) ([]byte, error) {
	shared, err := relaySharedSecret(privateKey, peerPublicKey)
	if err != nil {
		return nil, err
	}
	return relayDeriveKey(shared, notificationEnvelopeContext(pairingID, hostID, deviceID), notificationEnvelopeInfo), nil
}

// encodeNotificationEnvelope 编码 envelope 明文。version 固定为 1，type 是四类
// kind 之一，与 dsh-ios NotificationKind rawValue 一致。
func encodeNotificationEnvelope(n Notification) ([]byte, error) {
	return json.Marshal(notificationEnvelopePlaintext{
		Version:   1,
		Type:      n.Type,
		SessionID: n.SessionID,
		Summary:   n.Summary,
	})
}

func sealNotificationEnvelope(plaintext, key []byte) ([]byte, error) {
	return relaySeal(plaintext, key, notificationEnvelopeAAD())
}

func openNotificationEnvelope(combined, key []byte) ([]byte, error) {
	return relayOpen(combined, key, notificationEnvelopeAAD())
}

// notificationTarget 是一个目标 Device 的加密 Notification envelope。
type notificationTarget struct {
	DeviceID string `json:"deviceID"`
	Envelope []byte `json:"envelope"`
}

// notificationFanOut 是一次事件的加密 fan-out 输入：hostID + 稳定 eventID +
// 每个目标 Device 各自的密文。eventID 即 dedupe identity（rpcId），server 据此
// 按事件与 Device 去重，重放不会重复投递。
type notificationFanOut struct {
	HostID  string               `json:"hostID"`
	EventID string               `json:"eventID"`
	Targets []notificationTarget `json:"devices"`
}

// notificationKinds 是 Host 生成的高价值事件四类 kind；其他事件不产生 Notification。
var notificationKinds = map[string]bool{
	"question":  true,
	"approval":  true,
	"completed": true,
	"error":     true,
}

// buildNotificationFanOut 把一条 Notification 转为面向每个目标 Device 的独立
// 加密 envelope。只使用携带有效 X25519 Device 公钥的 Pairing；单个 Device 的
// 派生或加密失败不影响其他 Device。
func buildNotificationFanOut(hostID string, hostPrivateKey []byte, pairings []relayPairing, notif Notification) (notificationFanOut, error) {
	if hostID == "" || len(hostPrivateKey) != 32 {
		return notificationFanOut{}, errors.New("Host notification identity unavailable")
	}
	if !notificationKinds[notif.Type] || notif.SessionID == "" || notif.DedupeKey == "" {
		return notificationFanOut{}, errors.New("invalid Notification for fan-out")
	}
	plaintext, err := encodeNotificationEnvelope(notif)
	if err != nil {
		return notificationFanOut{}, err
	}
	out := notificationFanOut{HostID: hostID, EventID: notif.DedupeKey}
	for _, pairing := range pairings {
		if !pairing.valid() {
			continue
		}
		key, err := notificationEnvelopeKey(hostPrivateKey, pairing.DevicePublicKey, pairing.PairingID, hostID, pairing.DeviceID)
		if err != nil {
			continue
		}
		sealed, err := sealNotificationEnvelope(plaintext, key)
		if err != nil {
			continue
		}
		out.Targets = append(out.Targets, notificationTarget{DeviceID: pairing.DeviceID, Envelope: sealed})
	}
	if len(out.Targets) == 0 {
		return notificationFanOut{}, errors.New("no target Device for Notification fan-out")
	}
	return out, nil
}

// notificationBridge 把去重后的 Notification 派生为面向每个目标 Device 的加密
// envelope fan-out。它只做纯派生与加密，不发起网络投递（投递由 dsh-server 侧
// ticket 21 与跨仓 harness ticket 24 负责）。
type notificationBridge struct {
	identity relayIdentitySource
}

func (b *notificationBridge) fanOut(notif Notification) (notificationFanOut, error) {
	identity, err := b.identity.relayIdentity()
	if err != nil {
		return notificationFanOut{}, err
	}
	return buildNotificationFanOut(identity.HostID, identity.PrivateKey, identity.Pairings, notif)
}
