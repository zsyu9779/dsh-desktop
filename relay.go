package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sync"
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
	call(context.Context, string, []byte) ([]byte, error)
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

	mu      sync.Mutex
	current relayStatus
	cancel  context.CancelFunc
	done    chan struct{}
	active  relayHostConnection
}

func newRelayHost(identity relayIdentitySource, connector relayHostConnector, upstream relayUpstream) *relayHost {
	return &relayHost{
		identity: identity, connector: connector, upstream: upstream,
		randomBytes: relayRandomBytes,
		current:     relayStatus{State: relayOffline, Message: "Relay 离线"},
	}
}

func (h *relayHost) status() relayStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.current
}

func (h *relayHost) setStatus(state relayConnectionState, message string) {
	h.mu.Lock()
	h.current = relayStatus{State: state, Message: message}
	h.mu.Unlock()
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
		for {
			_ = h.serveOnce(ctx)
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
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

func (h *relayHost) serveOnce(ctx context.Context) error {
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
	defer h.setStatus(relayOffline, "Relay 离线")

	channels := make(map[string]*relayHostChannel)
	for {
		event, err := connection.receiveEvent()
		if err != nil {
			return err
		}
		switch event.Type {
		case relayChannelOpened:
			if event.ChannelID != "" {
				channels[event.ChannelID] = &relayHostChannel{id: event.ChannelID}
			}
		case relayChannelClosed:
			delete(channels, event.ChannelID)
		case relayCiphertext:
			channel := channels[event.ChannelID]
			if channel == nil || len(event.Ciphertext) == 0 {
				continue
			}
			response, ok := h.handleCiphertext(ctx, channel, event.Ciphertext)
			if !ok {
				// 空密文会被 Relay 的有界帧校验器拒绝，仅关闭这一个 opaque channel，
				// 并即时通知 Device，不暴露任何业务标识。
				if err := connection.sendFrame(relayFrame{ChannelID: channel.id}); err != nil {
					return err
				}
				delete(channels, event.ChannelID)
				continue
			}
			if err := connection.sendFrame(relayFrame{ChannelID: channel.id, Ciphertext: response}); err != nil {
				return err
			}
		}
	}
}

type relayHostChannel struct {
	id              string
	deviceToHostKey []byte
	hostToDeviceKey []byte
	ready           bool
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

func (h *relayHost) handleCiphertext(ctx context.Context, channel *relayHostChannel, ciphertext []byte) ([]byte, bool) {
	if !channel.ready {
		// Pairing 可能在 Host 长连接已在线期间完成；在 channel 握手时刷新
		// identity 快照，使首次 Relay 尝试无需重连或重新扫码即可成功。
		identity, err := h.identity.relayIdentity()
		if err != nil || validateRelayIdentity(identity) != nil {
			return nil, false
		}
		return h.acceptHandshake(identity, channel, ciphertext)
	}
	plaintext, err := relayOpen(ciphertext, channel.deviceToHostKey, relayRPCAAD(channel.id, "device-host"))
	if err != nil {
		return nil, false
	}
	method, err := relayRPCMethod(plaintext)
	if err != nil {
		return nil, false
	}
	response, err := h.upstream.call(ctx, method, plaintext)
	if err != nil {
		return nil, false
	}
	sealed, err := relaySeal(response, channel.hostToDeviceKey, relayRPCAAD(channel.id, "host-device"))
	return sealed, err == nil
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
