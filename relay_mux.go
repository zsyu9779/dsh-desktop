package main

import (
	"context"
	"sync"
)

// relayChannelIncomingDepth 是每个 channel 的入站密文队列深度。队列满时仅关闭
// 该 channel，不阻塞读循环，也不影响其他 channel。
const relayChannelIncomingDepth = 8

// relayHostMux 拥有一条 Host Relay 连接上的全部 channel：读循环串行接收事件，
// 写操作串行化（gorilla/websocket 只允许单一写者），每个 channel 在独立
// goroutine 里完成握手并映射到本地 dsh，因此单 channel 的慢、超时或错误不会
// 阻塞其他 channel。
type relayHostMux struct {
	identity        relayIdentitySource
	upstream        relayUpstream
	acceptHandshake func(relayHostIdentity, *relayHostChannel, []byte) ([]byte, bool)
	connection      relayHostConnection
	ctx             context.Context

	writeMu  sync.Mutex
	mu       sync.Mutex
	closed   bool
	channels map[string]*relayHostChannel

	// onDeviceActive 在 channel 完成握手时回调，标记该 Device 正经 Relay 传输。
	onDeviceActive func(activity deviceActivity)
	onRegistrySync func(version uint64, revokedPairingIDs []string, pairings []relayPairingSync)
}

func newRelayHostMux(host *relayHost, connection relayHostConnection, ctx context.Context) *relayHostMux {
	return &relayHostMux{
		identity:        host.identity,
		upstream:        host.upstream,
		acceptHandshake: host.acceptHandshake,
		connection:      connection,
		ctx:             ctx,
		channels:        make(map[string]*relayHostChannel),
		onDeviceActive:  host.onDeviceActive,
		onRegistrySync:  host.onRegistrySync,
	}
}

func (m *relayHostMux) serve() error {
	for {
		event, err := m.connection.receiveEvent()
		if err != nil {
			return err
		}
		switch event.Type {
		case relayRegistrySynced:
			if m.onRegistrySync != nil {
				m.onRegistrySync(event.RevocationVersion, append([]string(nil), event.RevokedPairingIDs...), append([]relayPairingSync(nil), event.Pairings...))
			}
		case relayChannelOpened:
			m.openChannel(event.ChannelID)
		case relayChannelClosed:
			m.closeChannel(event.ChannelID)
		case relayCiphertext:
			m.deliver(event.ChannelID, event.Ciphertext)
		}
	}
}

func (m *relayHostMux) openChannel(id string) {
	if id == "" {
		return
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	if _, exists := m.channels[id]; exists {
		m.mu.Unlock()
		return
	}
	channel := &relayHostChannel{
		id:       id,
		incoming: make(chan []byte, relayChannelIncomingDepth),
		done:     make(chan struct{}),
	}
	m.channels[id] = channel
	m.mu.Unlock()
	go m.runChannel(channel)
}

func (m *relayHostMux) closeChannel(id string) {
	m.mu.Lock()
	channel := m.channels[id]
	delete(m.channels, id)
	m.mu.Unlock()
	if channel != nil {
		channel.close()
	}
}

func (m *relayHostMux) deliver(id string, ciphertext []byte) {
	if len(ciphertext) == 0 {
		return
	}
	m.mu.Lock()
	channel := m.channels[id]
	m.mu.Unlock()
	if channel == nil {
		return
	}
	select {
	case channel.incoming <- ciphertext:
	case <-channel.done:
	default:
		// 入站队列满：仅关闭该 channel，保护读循环与其他 channel。
		m.closeChannel(id)
	}
}

func (m *relayHostMux) closeAll() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	channels := make([]*relayHostChannel, 0, len(m.channels))
	for _, channel := range m.channels {
		channels = append(channels, channel)
	}
	m.channels = make(map[string]*relayHostChannel)
	m.mu.Unlock()
	for _, channel := range channels {
		channel.close()
	}
}

func (m *relayHostMux) sendFrame(frame relayFrame) error {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	if err := m.connection.sendFrame(frame); err != nil {
		_ = m.connection.close()
		return err
	}
	return nil
}

func (m *relayHostMux) runChannel(channel *relayHostChannel) {
	defer channel.close()
	for {
		select {
		case <-channel.done:
			return
		case <-m.ctx.Done():
			return
		case ciphertext := <-channel.incoming:
			if !m.handleChannelCiphertext(channel, ciphertext) {
				// 仅关闭这一个 opaque channel：空密文会被 Relay 的有界帧校验器
				// 拒绝，从而通知 Device 并结束该 channel，不暴露任何业务标识。
				m.mu.Lock()
				if m.channels[channel.id] == channel {
					delete(m.channels, channel.id)
				}
				m.mu.Unlock()
				_ = m.sendFrame(relayFrame{ChannelID: channel.id})
				return
			}
		}
	}
}

// markRelayActive 在握手成功或后续收到有效密文时，把该 Device 标记为经 Relay
// 活跃并刷新活动时间戳（与 LAN 每次认证请求对称）。
func (m *relayHostMux) markRelayActive(channel *relayHostChannel) {
	if m.onDeviceActive != nil && channel.deviceID != "" {
		m.onDeviceActive(deviceActivity{DeviceID: channel.deviceID, Name: channel.name, Transport: deviceTransportRelay})
	}
}

func (m *relayHostMux) handleChannelCiphertext(channel *relayHostChannel, ciphertext []byte) bool {
	if !channel.ready {
		// Pairing 可能在 Host 长连接已在线期间完成；在 channel 握手时刷新
		// identity 快照，使首次 Relay 尝试无需重连或重新扫码即可成功。
		identity, err := m.identity.relayIdentity()
		if err != nil || validateRelayIdentity(identity) != nil {
			return false
		}
		response, ok := m.acceptHandshake(identity, channel, ciphertext)
		if !ok {
			return false
		}
		m.markRelayActive(channel)
		return m.sendFrame(relayFrame{ChannelID: channel.id, Ciphertext: response}) == nil
	}

	plaintext, err := relayOpen(ciphertext, channel.deviceToHostKey, relayRPCAAD(channel.id, "device-host"))
	if err != nil {
		return false
	}
	m.markRelayActive(channel)
	method, err := relayRPCMethod(plaintext)
	if err != nil {
		return false
	}
	if m.upstream.isStreamMethod(method) {
		return m.serveStream(channel, method)
	}
	response, err := m.upstream.call(m.ctx, method, plaintext)
	if err != nil {
		return false
	}
	sealed, err := relaySeal(response, channel.hostToDeviceKey, relayRPCAAD(channel.id, "host-device"))
	if err != nil {
		return false
	}
	return m.sendFrame(relayFrame{ChannelID: channel.id, Ciphertext: sealed}) == nil
}

func (m *relayHostMux) serveStream(channel *relayHostChannel, method string) bool {
	stream, err := m.upstream.openStream(m.ctx, method)
	if err != nil {
		return false
	}
	stop := make(chan struct{})
	var once sync.Once
	closeStream := func() {
		once.Do(func() {
			close(stop)
			_ = stream.close()
		})
	}
	defer closeStream()
	// channel 关闭或连接上下文取消时打断阻塞中的 receiveFrame。
	go func() {
		select {
		case <-channel.done:
		case <-m.ctx.Done():
		case <-stop:
		}
		closeStream()
	}()

	for {
		frame, err := stream.receiveFrame()
		if err != nil {
			return false
		}
		sealed, err := relaySeal(frame, channel.hostToDeviceKey, relayRPCAAD(channel.id, "host-device"))
		if err != nil {
			return false
		}
		if err := m.sendFrame(relayFrame{ChannelID: channel.id, Ciphertext: sealed}); err != nil {
			return false
		}
	}
}
