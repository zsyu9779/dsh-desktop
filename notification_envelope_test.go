package main

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// notificationV1Vector 是与 dsh-ios 共享的 notification-v1 跨仓测试向量。
// 字段与 dsh-ios NotificationV1TestVector 逐一对应。
type notificationV1Vector struct {
	Version          int    `json:"version"`
	PairingID        string `json:"pairingID"`
	HostID           string `json:"hostID"`
	DeviceID         string `json:"deviceID"`
	DevicePrivateKey string `json:"devicePrivateKey"`
	DevicePublicKey  string `json:"devicePublicKey"`
	HostPrivateKey   string `json:"hostPrivateKey"`
	HostPublicKey    string `json:"hostPublicKey"`
	Context          string `json:"context"`
	EnvelopeKey      string `json:"envelopeKey"`
	EnvelopeAAD      string `json:"envelopeAAD"`
	Nonce            string `json:"nonce"`
	Plaintext        string `json:"plaintext"`
	Ciphertext       string `json:"ciphertext"`
}

func loadNotificationVector(t *testing.T) notificationV1Vector {
	t.Helper()
	raw, err := os.ReadFile("test-vectors/notification-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var vector notificationV1Vector
	if err := json.Unmarshal(raw, &vector); err != nil {
		t.Fatal(err)
	}
	return vector
}

// TestNotificationEnvelopeMatchesIOSVector 证明 Host 侧加密与 dsh-ios 的
// notification-v1 契约逐字节一致：context、envelope key 与 AES-GCM 密文都用
// 共享向量校验。
func TestNotificationEnvelopeMatchesIOSVector(t *testing.T) {
	vector := loadNotificationVector(t)
	hostPrivate := decodeVectorValue(t, vector.HostPrivateKey)
	devicePublic := decodeVectorValue(t, vector.DevicePublicKey)

	context := notificationEnvelopeContext(vector.PairingID, vector.HostID, vector.DeviceID)
	if got := base64.StdEncoding.EncodeToString(context); got != vector.Context {
		t.Fatalf("context = %q, want %q", got, vector.Context)
	}
	if got := string(notificationEnvelopeAAD()); got != vector.EnvelopeAAD {
		t.Fatalf("AAD = %q, want %q", got, vector.EnvelopeAAD)
	}

	key, err := notificationEnvelopeKey(hostPrivate, devicePublic, vector.PairingID, vector.HostID, vector.DeviceID)
	if err != nil {
		t.Fatal(err)
	}
	assertVectorKey(t, key, vector.EnvelopeKey)

	// 用向量中的 nonce 复现密文：证明 Host 侧 seal 与 iOS 侧一致。
	sealed, err := relaySealWithNonce(
		decodeVectorValue(t, vector.Plaintext),
		key,
		notificationEnvelopeAAD(),
		decodeVectorValue(t, vector.Nonce),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := base64.StdEncoding.EncodeToString(sealed); got != vector.Ciphertext {
		t.Fatalf("ciphertext = %q, want %q", got, vector.Ciphertext)
	}

	// 反向打开，解出定位信息（kind + sessionID + summary）。
	plaintext, err := openNotificationEnvelope(decodeVectorValue(t, vector.Ciphertext), key)
	if err != nil {
		t.Fatal(err)
	}
	var decoded notificationEnvelopePlaintext
	if err := json.Unmarshal(plaintext, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Version != 1 || decoded.Type != "approval" || decoded.SessionID != "session-secret" || decoded.Summary != "工具请求授权" {
		t.Fatalf("decoded plaintext = %+v", decoded)
	}
}

// TestNotificationEnvelopeRoundtripsAllFourKinds 覆盖四类高价值事件的明文往返。
func TestNotificationEnvelopeRoundtripsAllFourKinds(t *testing.T) {
	vector := loadNotificationVector(t)
	hostPrivate := decodeVectorValue(t, vector.HostPrivateKey)
	devicePrivate := decodeVectorValue(t, vector.DevicePrivateKey)
	devicePublic := decodeVectorValue(t, vector.DevicePublicKey)
	hostPublic := decodeVectorValue(t, vector.HostPublicKey)

	cases := []Notification{
		{Type: "question", SessionID: "session-1", Summary: "新的提问"},
		{Type: "approval", SessionID: "session-1", Summary: "工具请求授权"},
		{Type: "completed", SessionID: "session-1", Summary: "目标已完成"},
		{Type: "error", SessionID: "session-1", Summary: "任务出错"},
	}
	for _, notif := range cases {
		hostKey, err := notificationEnvelopeKey(hostPrivate, devicePublic, vector.PairingID, vector.HostID, vector.DeviceID)
		if err != nil {
			t.Fatal(err)
		}
		plaintext, err := encodeNotificationEnvelope(notif)
		if err != nil {
			t.Fatal(err)
		}
		sealed, err := sealNotificationEnvelope(plaintext, hostKey)
		if err != nil {
			t.Fatal(err)
		}
		deviceKey, err := notificationEnvelopeKey(devicePrivate, hostPublic, vector.PairingID, vector.HostID, vector.DeviceID)
		if err != nil {
			t.Fatal(err)
		}
		opened, err := openNotificationEnvelope(sealed, deviceKey)
		if err != nil {
			t.Fatal(err)
		}
		var decoded notificationEnvelopePlaintext
		if err := json.Unmarshal(opened, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.Version != 1 || decoded.Type != notif.Type || decoded.SessionID != notif.SessionID || decoded.Summary != notif.Summary {
			t.Fatalf("roundtrip %q = %+v, want %+v", notif.Type, decoded, notif)
		}
	}
}

// TestBuildNotificationFanOutProducesIndependentEnvelopes 验证每个目标 Device
// 得到独立密文，且只能用自身 Pairing 密钥解开。
func TestBuildNotificationFanOutProducesIndependentEnvelopes(t *testing.T) {
	vector := loadNotificationVector(t)
	hostPrivate := decodeVectorValue(t, vector.HostPrivateKey)
	hostPublic := decodeVectorValue(t, vector.HostPublicKey)
	hostID := vector.HostID

	device1ID, device1Pairing, device1Priv, device1Pub := newTestDevice(t)
	device2ID, device2Pairing, device2Priv, device2Pub := newTestDevice(t)

	pairings := []relayPairing{
		{PairingID: device1Pairing, DeviceID: device1ID, DevicePublicKey: device1Pub},
		{PairingID: device2Pairing, DeviceID: device2ID, DevicePublicKey: device2Pub},
	}
	notif := Notification{Type: "approval", SessionID: "session-1", Summary: "工具请求授权", DedupeKey: "rpc-1"}

	fanOut, err := buildNotificationFanOut(hostID, hostPrivate, pairings, notif)
	if err != nil {
		t.Fatal(err)
	}
	if fanOut.HostID != hostID {
		t.Errorf("HostID = %q, want %q", fanOut.HostID, hostID)
	}
	if fanOut.EventID != "rpc-1" {
		t.Errorf("EventID = %q, want rpc-1 (dedupe identity)", fanOut.EventID)
	}
	if len(fanOut.Targets) != 2 {
		t.Fatalf("Targets = %d, want 2", len(fanOut.Targets))
	}

	// 每个 Device 用自己的私钥解密，得到同一明文；密文彼此独立。
	var contents []string
	seenEnvelope := map[string]bool{}
	for i, target := range fanOut.Targets {
		var priv, peer, pairingID string
		switch target.DeviceID {
		case device1ID:
			priv, peer, pairingID = b64str(device1Priv), b64str(hostPublic), device1Pairing
		case device2ID:
			priv, peer, pairingID = b64str(device2Priv), b64str(hostPublic), device2Pairing
		default:
			t.Fatalf("unexpected target Device %q", target.DeviceID)
		}
		key, err := notificationEnvelopeKey(
			decodeVectorValue(t, priv),
			decodeVectorValue(t, peer),
			pairingID, hostID, target.DeviceID,
		)
		if err != nil {
			t.Fatal(err)
		}
		plaintext, err := openNotificationEnvelope(target.Envelope, key)
		if err != nil {
			t.Fatalf("Device %d 无法解开自己的 envelope: %v", i, err)
		}
		if seenEnvelope[string(target.Envelope)] {
			t.Fatal("两个 Device 的 envelope 应彼此独立")
		}
		seenEnvelope[string(target.Envelope)] = true
		contents = append(contents, string(plaintext))
	}
	if len(contents) != 2 || contents[0] != contents[1] {
		t.Fatalf("两个 Device 应解出同一明文, got %q vs %q", contents[0], contents[1])
	}

	// 交叉解密必须失败：device1 的私钥解不开 device2 的 envelope。
	device1Key, err := notificationEnvelopeKey(device1Priv, hostPublic, device1Pairing, hostID, device1ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := openNotificationEnvelope(fanOut.Targets[1].Envelope, device1Key); err == nil {
		t.Fatal("device1 不应能解开 device2 的 envelope")
	}
}

// TestBuildNotificationFanOutSkipsInvalidPairings 验证只对携带有效 X25519 公钥的
// Pairing 派生 envelope，全部无效时报错。
func TestBuildNotificationFanOutSkipsInvalidPairings(t *testing.T) {
	vector := loadNotificationVector(t)
	hostPrivate := decodeVectorValue(t, vector.HostPrivateKey)
	_, _, _, validPub := newTestDevice(t)

	notif := Notification{Type: "completed", SessionID: "session-1", Summary: "目标已完成", DedupeKey: "rpc-1"}

	t.Run("invalid pairings are skipped", func(t *testing.T) {
		pairings := []relayPairing{
			{PairingID: "", DeviceID: "d1", DevicePublicKey: validPub},           // 缺 PairingID
			{PairingID: "p2", DeviceID: "d2"},                                    // 缺公钥
			{PairingID: "p3", DeviceID: "d3", DevicePublicKey: make([]byte, 31)}, // 公钥长度错误
			{PairingID: "p4", DeviceID: "d4", DevicePublicKey: validPub},         // 有效
		}
		fanOut, err := buildNotificationFanOut(vector.HostID, hostPrivate, pairings, notif)
		if err != nil {
			t.Fatal(err)
		}
		if len(fanOut.Targets) != 1 || fanOut.Targets[0].DeviceID != "d4" {
			t.Fatalf("Targets = %+v, want 仅 d4", fanOut.Targets)
		}
	})

	t.Run("no valid pairing returns error", func(t *testing.T) {
		_, err := buildNotificationFanOut(vector.HostID, hostPrivate, []relayPairing{
			{PairingID: "p2", DeviceID: "d2"},
		}, notif)
		if err == nil {
			t.Fatal("expected error when no target Device remains")
		}
	})
}

// TestBuildNotificationFanOutRejectsInvalidInput 拒绝缺失身份或非四类 kind 的输入。
func TestBuildNotificationFanOutRejectsInvalidInput(t *testing.T) {
	vector := loadNotificationVector(t)
	hostPrivate := decodeVectorValue(t, vector.HostPrivateKey)
	_, pairing, _, pub := newTestDevice(t)
	pairings := []relayPairing{{PairingID: pairing, DeviceID: "d1", DevicePublicKey: pub}}

	if _, err := buildNotificationFanOut("", hostPrivate, pairings, Notification{Type: "question", SessionID: "s", DedupeKey: "k"}); err == nil {
		t.Fatal("expected error for empty hostID")
	}
	if _, err := buildNotificationFanOut(vector.HostID, nil, pairings, Notification{Type: "question", SessionID: "s", DedupeKey: "k"}); err == nil {
		t.Fatal("expected error for missing host private key")
	}
	if _, err := buildNotificationFanOut(vector.HostID, hostPrivate, pairings, Notification{Type: "mystery", SessionID: "s", DedupeKey: "k"}); err == nil {
		t.Fatal("expected error for non-four-kind type")
	}
	if _, err := buildNotificationFanOut(vector.HostID, hostPrivate, pairings, Notification{Type: "question", SessionID: "", DedupeKey: "k"}); err == nil {
		t.Fatal("expected error for empty sessionID")
	}
}

// TestBridgeFansOutAtDedupeGate 验证去重门控是 fan-out 的唯一入口：重放的 frame
// 不会再次派生 envelope，且 EventID 稳定等于 rpcId。
func TestBridgeFansOutAtDedupeGate(t *testing.T) {
	vector := loadNotificationVector(t)
	hostPrivate := decodeVectorValue(t, vector.HostPrivateKey)
	_, pairing, _, pub := newTestDevice(t)
	identity := relayHostIdentity{
		HostID:     vector.HostID,
		PrivateKey: hostPrivate,
		Pairings:   []relayPairing{{PairingID: pairing, DeviceID: "d1", DevicePublicKey: pub}},
	}

	upgrader := websocket.Upgrader{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/remote.mux", func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		_, _, _ = c.ReadMessage()
		frame := muxItem(`{"type":"waterfall","event":"user-questions/request","eventId":"r1","agentId":"s1","request":{"questions":[{"id":"q1","question":"hi"}]}}`)
		_ = c.WriteMessage(websocket.TextMessage, frame)
		_ = c.WriteMessage(websocket.TextMessage, frame) // 重放
		time.Sleep(200 * time.Millisecond)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	n := newNotifyManager(nil)
	n.bridge = &notificationBridge{identity: fakeNotificationIdentity{identity: identity}}
	n.fanOutSink = make(chan notificationFanOut, 8)
	n.start(srv.URL)
	defer n.stop()

	select {
	case fanOut := <-n.fanOutSink:
		if fanOut.EventID != "r1" {
			t.Errorf("EventID = %q, want r1", fanOut.EventID)
		}
		if len(fanOut.Targets) != 1 || fanOut.Targets[0].DeviceID != "d1" {
			t.Errorf("Targets = %+v, want 仅 d1", fanOut.Targets)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("未收到 fan-out")
	}

	select {
	case fanOut := <-n.fanOutSink:
		t.Fatalf("重放应被去重门控丢弃, got %+v", fanOut)
	case <-time.After(500 * time.Millisecond):
		// 良好：重放未派生第二次 fan-out
	}
}

type fakeNotificationIdentity struct {
	identity relayHostIdentity
	err      error
}

func (f fakeNotificationIdentity) relayIdentity() (relayHostIdentity, error) {
	return f.identity, f.err
}

// newTestDevice 生成一个随机的 X25519 Device 身份，返回 deviceID、pairingID、
// private key 与 public key。
func newTestDevice(t *testing.T) (deviceID, pairingID string, privateKey, publicKey []byte) {
	t.Helper()
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := newDeviceID()
	return "device-" + id[:8], "pairing-" + id[:8], key.Bytes(), key.PublicKey().Bytes()
}

// b64str 用标准 base64 编码原始字节，便于复用 decodeVectorValue。
func b64str(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}
