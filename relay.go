package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"sync/atomic"
	"time"
)

type relayEventType string

const (
	relayChannelOpened relayEventType = "channel_opened"
	relayCiphertext    relayEventType = "ciphertext"
	relayChannelClosed relayEventType = "channel_closed"
)

type relayEvent struct {
	Type       relayEventType `json:"type"`
	ChannelID  string         `json:"channelID"`
	Ciphertext []byte         `json:"ciphertext,omitempty"`
}

type relayFrame struct {
	ChannelID  string `json:"channelID"`
	Ciphertext []byte `json:"ciphertext"`
}

type relayPairing struct {
	PairingID       string
	DeviceID        string
	DevicePublicKey []byte
	Name            string
}

type relayHostIdentity struct {
	AccountID   string
	AccessToken string
	HostID      string
	PrivateKey  []byte
	Pairings    []relayPairing
}

type relayHostEndpoint struct {
	AccessToken string
	HostID      string
}

type relayIdentitySource interface {
	relayIdentity() (relayHostIdentity, error)
}

type relayHostConnector interface {
	connect(context.Context, relayHostEndpoint) (relayHostConnection, error)
}

type relayHostConnection interface {
	receiveEvent() (relayEvent, error)
	sendFrame(relayFrame) error
	close() error
}

type relayUpstream interface {
	// call 转发一个 unary client-request，返回本地 dsh 的 server-response。
	call(context.Context, string, []byte) ([]byte, error)
	// openStream 为 stream method 打开本地 dsh 的 WebSocket/事件流。
	openStream(context.Context, string) (relayStream, error)
	// isStreamMethod 报告 method 是否映射到 WebSocket/事件流。
	isStreamMethod(string) bool
}

// relayStream 是本地 dsh 的 WebSocket/事件流。dsh 的事件流只下传：
// 上行仍走 HTTP，向事件流发送 Device 侧消息会被 1008 关闭。
type relayStream interface {
	receiveFrame() ([]byte, error)
	close() error
}

type relayConnectionState string

const (
	relayConnecting relayConnectionState = "connecting"
	relayOnline     relayConnectionState = "online"
	relayOffline    relayConnectionState = "offline"
)

type relayStatus struct {
	State   relayConnectionState `json:"state"`
	Message string               `json:"message"`
}

type relayHost struct {
	identity    relayIdentitySource
	connector   relayHostConnector
	upstream    relayUpstream
	randomBytes func(int) ([]byte, error)

	// reconnectMin / reconnectMax 界定断线重连的有界退避区间。
	reconnectMin time.Duration
	reconnectMax time.Duration

	mu      sync.Mutex
	current relayStatus
	cancel  context.CancelFunc
	done    chan struct{}
	active  relayHostConnection

	// servedOnline 记录最近一次 serveOnce 是否进入过在线态，用于退避重置。
	servedOnline atomic.Bool

	// onStatus 在 Relay 状态变化时回调（生产环境由 App 注入以推送 UI 事件）。
	onStatus func(relayStatus)
	// onDeviceActive 在 Device 经 Relay 完成握手时回调，标记活动 Device 传输。
	onDeviceActive func(activity deviceActivity)
	// sleepAfter 是重连退避等待的注入点；默认 time.After，测试可注入可控时钟。
	sleepAfter func(time.Duration) <-chan time.Time
}

const (
	relayReconnectMin = 500 * time.Millisecond
	relayReconnectMax = 30 * time.Second
)

func newRelayHost(identity relayIdentitySource, connector relayHostConnector, upstream relayUpstream) *relayHost {
	return &relayHost{
		identity: identity, connector: connector, upstream: upstream,
		randomBytes:  relayRandomBytes,
		reconnectMin: relayReconnectMin,
		reconnectMax: relayReconnectMax,
		sleepAfter:   time.After,
		current:      relayStatus{State: relayOffline, Message: "Relay 离线"},
	}
}

func (h *relayHost) status() relayStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.current
}

func (h *relayHost) setStatus(state relayConnectionState, message string) {
	status := relayStatus{State: state, Message: message}
	h.mu.Lock()
	h.current = status
	h.mu.Unlock()
	if h.onStatus != nil {
		h.onStatus(status)
	}
}

func (h *relayHost) start() {
	h.mu.Lock()
	if h.cancel != nil {
		h.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	h.cancel = cancel
	h.done = done
	h.mu.Unlock()
	go func() {
		defer close(done)
		delay := h.reconnectMin
		for {
			_ = h.serveOnce(ctx)
			select {
			case <-ctx.Done():
				return
			default:
			}
			if h.servedOnline.Load() {
				// 成功上线后重置退避，下一次断线可快速重连。
				delay = h.reconnectMin
			} else {
				delay = h.reconnectAfter(delay)
			}
			select {
			case <-ctx.Done():
				return
			case <-h.sleepAfter(delay):
			}
		}
	}()
}

func (h *relayHost) stop() {
	h.mu.Lock()
	cancel, done, active := h.cancel, h.done, h.active
	h.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if active != nil {
		_ = active.close()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	}
	h.mu.Lock()
	if h.done == done {
		h.cancel, h.done = nil, nil
	}
	h.mu.Unlock()
}

// reconnectAfter 返回下一次重连等待时长：指数退避且有上界。
func (h *relayHost) reconnectAfter(current time.Duration) time.Duration {
	next := current * 2
	if next < current || next > h.reconnectMax {
		return h.reconnectMax
	}
	return next
}

func (h *relayHost) serveOnce(ctx context.Context) error {
	// 记录本次 serveOnce 是否进入在线态；start() 据此决定退避重置还是增长。
	h.servedOnline.Store(false)
	h.setStatus(relayConnecting, "正在连接 Relay")
	identity, err := h.identity.relayIdentity()
	if err != nil {
		h.setStatus(relayOffline, "Relay 离线："+err.Error())
		return err
	}
	if err := validateRelayIdentity(identity); err != nil {
		h.setStatus(relayOffline, "Relay 离线：Host 身份不可用")
		return err
	}
	connection, err := h.connector.connect(ctx, relayHostEndpoint{AccessToken: identity.AccessToken, HostID: identity.HostID})
	if err != nil {
		h.setStatus(relayOffline, "Relay 离线：连接失败")
		return err
	}
	h.mu.Lock()
	h.active = connection
	h.mu.Unlock()
	watcherDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = connection.close()
		case <-watcherDone:
		}
	}()
	defer func() {
		close(watcherDone)
		_ = connection.close()
		h.mu.Lock()
		if h.active == connection {
			h.active = nil
		}
		h.mu.Unlock()
	}()
	h.setStatus(relayOnline, "Relay 在线")
	h.servedOnline.Store(true)
	defer h.setStatus(relayOffline, "Relay 离线")

	mux := newRelayHostMux(h, connection, ctx)
	defer mux.closeAll()
	return mux.serve()
}

type relayHostChannel struct {
	id              string
	deviceToHostKey []byte
	hostToDeviceKey []byte
	ready           bool
	deviceID        string
	name            string

	incoming  chan []byte
	done      chan struct{}
	closeOnce sync.Once
}

func (c *relayHostChannel) close() {
	c.closeOnce.Do(func() { close(c.done) })
}

type relayDeviceHello struct {
	Version            int    `json:"version"`
	Type               string `json:"type"`
	EphemeralPublicKey []byte `json:"ephemeralPublicKey"`
	DeviceNonce        []byte `json:"deviceNonce"`
}

type relayHostHello struct {
	Version     int    `json:"version"`
	Type        string `json:"type"`
	Accepted    bool   `json:"accepted"`
	DeviceNonce []byte `json:"deviceNonce"`
	HostNonce   []byte `json:"hostNonce"`
}

func (h *relayHost) acceptHandshake(identity relayHostIdentity, channel *relayHostChannel, ciphertext []byte) ([]byte, bool) {
	hostPublicKey, err := relayPublicKey(identity.PrivateKey)
	if err != nil {
		return nil, false
	}
	for _, pairing := range identity.Pairings {
		contextBytes := relayHandshakeContext(pairing.PairingID, identity.HostID, pairing.DeviceID, pairing.DevicePublicKey, hostPublicKey)
		staticSecret, err := relaySharedSecret(identity.PrivateKey, pairing.DevicePublicKey)
		if err != nil {
			continue
		}
		staticKey := relayDeriveKey(staticSecret, contextBytes, relayV1+"/device-hello")
		plaintext, err := relayOpen(ciphertext, staticKey, relayHelloAAD(channel.id))
		if err != nil {
			continue
		}
		var hello relayDeviceHello
		if json.Unmarshal(plaintext, &hello) != nil || hello.Version != 1 || hello.Type != "device_hello" || len(hello.DeviceNonce) != 32 || len(hello.EphemeralPublicKey) != 32 {
			return nil, false
		}
		ephemeralSecret, err := relaySharedSecret(identity.PrivateKey, hello.EphemeralPublicKey)
		if err != nil {
			return nil, false
		}
		material := append(append([]byte(nil), staticSecret...), ephemeralSecret...)
		preliminaryKey := relayDeriveKey(material, append(append([]byte(nil), contextBytes...), hello.DeviceNonce...), relayV1+"/host-hello")
		hostNonce, err := h.randomBytes(32)
		if err != nil {
			return nil, false
		}
		connectionMaterial := append(append([]byte(nil), material...), contextBytes...)
		salt := append(append([]byte(nil), hello.DeviceNonce...), hostNonce...)
		channel.deviceToHostKey = relayDeriveKey(connectionMaterial, salt, relayV1+"/device-host")
		channel.hostToDeviceKey = relayDeriveKey(connectionMaterial, salt, relayV1+"/host-device")
		channel.ready = true
		channel.deviceID = pairing.DeviceID
		channel.name = pairing.Name
		encoded, err := json.Marshal(relayHostHello{Version: 1, Type: "host_hello", Accepted: true, DeviceNonce: hello.DeviceNonce, HostNonce: hostNonce})
		if err != nil {
			return nil, false
		}
		sealed, err := relaySeal(encoded, preliminaryKey, relayHelloAAD(channel.id))
		return sealed, err == nil
	}
	return nil, false
}

var relayMethodPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func relayRPCMethod(request []byte) (string, error) {
	var envelope struct {
		Type   string `json:"type"`
		RPCID  string `json:"rpcId"`
		Method string `json:"method"`
	}
	if err := json.Unmarshal(request, &envelope); err != nil {
		return "", err
	}
	if envelope.Type != "client-request" || envelope.RPCID == "" || !relayMethodPattern.MatchString(envelope.Method) {
		return "", errors.New("invalid Relay RPC envelope")
	}
	return envelope.Method, nil
}

func validateRelayIdentity(identity relayHostIdentity) error {
	if identity.AccountID == "" || identity.AccessToken == "" || identity.HostID == "" || len(identity.PrivateKey) != 32 {
		return errors.New("incomplete Host Relay identity")
	}
	for _, pairing := range identity.Pairings {
		if pairing.PairingID == "" || pairing.DeviceID == "" || len(pairing.DevicePublicKey) != 32 {
			return fmt.Errorf("invalid Pairing %q", pairing.PairingID)
		}
	}
	return nil
}

func relayHelloAAD(channelID string) []byte {
	return []byte(relayV1 + "/hello/" + channelID)
}

func relayRPCAAD(channelID, direction string) []byte {
	return []byte(relayV1 + "/rpc/" + direction + "/" + channelID)
}

func relayRandomBytes(size int) ([]byte, error) {
	value := make([]byte, size)
	_, err := rand.Read(value)
	return value, err
}
