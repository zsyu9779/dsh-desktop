package main

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

// relayTestUpstream 是可编程 upstream：unary 与 stream 行为都可在测试里注入。
type relayTestUpstream struct {
	streamMethods map[string]bool
	unary         func(context.Context, string, []byte) ([]byte, error)
	stream        func(context.Context, string) (relayStream, error)
}

func newRelayTestUpstream() *relayTestUpstream {
	return &relayTestUpstream{
		streamMethods: map[string]bool{},
		unary: func(context.Context, string, []byte) ([]byte, error) {
			return serverResponse("r", "ok"), nil
		},
		stream: func(context.Context, string) (relayStream, error) {
			return newFakeRelayStream(), nil
		},
	}
}

func (u *relayTestUpstream) call(ctx context.Context, method string, body []byte) ([]byte, error) {
	return u.unary(ctx, method, body)
}
func (u *relayTestUpstream) openStream(ctx context.Context, method string) (relayStream, error) {
	return u.stream(ctx, method)
}
func (u *relayTestUpstream) isStreamMethod(method string) bool { return u.streamMethods[method] }

// serverResponse 构造一个最小 server-response envelope。
func serverResponse(rpcID, value string) []byte {
	encoded, _ := json.Marshal(map[string]any{
		"type":   "server-response",
		"rpcId":  rpcID,
		"result": map[string]any{"ok": true, "value": value},
	})
	return encoded
}

// relayTestNonce 构造一个填充给定字节的 32 字节 hostNonce。
func relayTestNonce(fill byte) []byte {
	nonce := make([]byte, 32)
	for i := range nonce {
		nonce[i] = fill
	}
	return nonce
}

// fakeRelayStream 是内存里的下传流：按序吐帧，空时阻塞，close 后 receiveFrame 报错。
type fakeRelayStream struct {
	frames chan []byte
	closed chan struct{}
	once   sync.Once
}

func newFakeRelayStream(frames ...[]byte) *fakeRelayStream {
	s := &fakeRelayStream{frames: make(chan []byte, 16), closed: make(chan struct{})}
	for _, frame := range frames {
		s.frames <- frame
	}
	return s
}
func (s *fakeRelayStream) receiveFrame() ([]byte, error) {
	select {
	case frame := <-s.frames:
		return frame, nil
	case <-s.closed:
		return nil, errors.New("stream closed")
	}
}
func (s *fakeRelayStream) close() error {
	s.once.Do(func() { close(s.closed) })
	return nil
}

// sequencedRelayConnector 依次返回预置连接，用于验证重连使用全新连接。
type sequencedRelayConnector struct {
	mu          sync.Mutex
	connections []relayHostConnection
	index       int
}

func (s *sequencedRelayConnector) connect(context.Context, relayHostEndpoint) (relayHostConnection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.index >= len(s.connections) {
		return nil, errors.New("no more connections")
	}
	connection := s.connections[s.index]
	s.index++
	return connection, nil
}
func (s *sequencedRelayConnector) current() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.index
}

// relayHostConnectionKeys 按给定 hostNonce 推导一对 channel 连接密钥。
func relayHostConnectionKeys(t *testing.T, vector relayV1Vector, hostNonce []byte) ([]byte, []byte) {
	t.Helper()
	hostPrivate := decodeVectorValue(t, vector.HostPrivateKey)
	devicePublic := decodeVectorValue(t, vector.DevicePublicKey)
	ephPublic := decodeVectorValue(t, vector.EphemeralPublicKey)
	deviceNonce := decodeVectorValue(t, vector.DeviceNonce)
	staticSecret, err := relaySharedSecret(hostPrivate, devicePublic)
	if err != nil {
		t.Fatal(err)
	}
	ephSecret, err := relaySharedSecret(hostPrivate, ephPublic)
	if err != nil {
		t.Fatal(err)
	}
	material := append(append([]byte(nil), staticSecret...), ephSecret...)
	contextBytes := relayHandshakeContext(vector.PairingID, vector.HostID, vector.DeviceID, devicePublic, decodeVectorValue(t, vector.HostPublicKey))
	connectionMaterial := append(append([]byte(nil), material...), contextBytes...)
	salt := append(append([]byte(nil), deviceNonce...), hostNonce...)
	return relayDeriveKey(connectionMaterial, salt, relayV1+"/device-host"),
		relayDeriveKey(connectionMaterial, salt, relayV1+"/host-device")
}

// relaySealRPCRequest 构造一个 client-request 的加密 RPC 密文。
func relaySealRPCRequest(t *testing.T, deviceToHostKey []byte, channelID, method, rpcID string) []byte {
	t.Helper()
	plaintext, err := json.Marshal(map[string]any{
		"type": "client-request", "rpcId": rpcID, "method": method, "payload": map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := relaySealWithNonce(plaintext, deviceToHostKey, []byte(relayRPCAAD(channelID, "device-host")), make([]byte, 12))
	if err != nil {
		t.Fatal(err)
	}
	return ciphertext
}

// openRelayTestChannel 打开一个 channel 并完成握手，返回该 channel 的连接密钥。
func openRelayTestChannel(t *testing.T, connection *fakeRelayHostConnection, vector relayV1Vector, channelID string, hostNonce []byte) ([]byte, []byte) {
	t.Helper()
	connection.receive <- relayEvent{Type: relayChannelOpened, ChannelID: channelID}
	connection.receive <- relayEvent{Type: relayCiphertext, ChannelID: channelID, Ciphertext: vectorDeviceHello(t, vector, channelID)}
	hostHello := receiveRelayFrame(t, connection.send)
	if _, err := relayOpen(hostHello.Ciphertext, decodeVectorValue(t, vector.HostHelloKey), []byte(relayHelloAAD(channelID))); err != nil {
		t.Fatalf("channel %s host hello cannot be authenticated: %v", channelID, err)
	}
	return relayHostConnectionKeys(t, vector, hostNonce)
}

func TestHostConcurrentlyServesMultipleChannelsWithoutCrosstalk(t *testing.T) {
	vector := loadRelayVector(t)
	hostNonceA := decodeVectorValue(t, vector.HostNonce)
	hostNonceB := relayTestNonce(0x01)

	slowRelease := make(chan struct{})
	upstream := newRelayTestUpstream()
	upstream.unary = func(_ context.Context, method string, _ []byte) ([]byte, error) {
		if method == "slow" {
			<-slowRelease
			return serverResponse("slow", "slow-result"), nil
		}
		return serverResponse("fast", "fast-result"), nil
	}

	connection := newFakeRelayHostConnection()
	host := newRelayHost(staticRelayIdentitySource{identity: vectorRelayHostIdentity(t, vector)}, staticRelayConnector{connection: connection}, upstream)
	host.randomBytes = sequenceRelayRandom(hostNonceA, hostNonceB)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- host.serveOnce(ctx) }()
	waitForRelayState(t, host, relayOnline)

	deviceToHostA, hostToDeviceA := openRelayTestChannel(t, connection, vector, "opaque-a", hostNonceA)
	deviceToHostB, hostToDeviceB := openRelayTestChannel(t, connection, vector, "opaque-b", hostNonceB)

	// 并发发送：A 阻塞在 slow upstream，B 立即返回。
	connection.receive <- relayEvent{Type: relayCiphertext, ChannelID: "opaque-a", Ciphertext: relaySealRPCRequest(t, deviceToHostA, "opaque-a", "slow", "rpc-a")}
	connection.receive <- relayEvent{Type: relayCiphertext, ChannelID: "opaque-b", Ciphertext: relaySealRPCRequest(t, deviceToHostB, "opaque-b", "fast", "rpc-b")}

	// B 的响应应先到且归属正确的 channel，证明慢 channel 不阻塞兄弟 channel。
	frameB := receiveRelayFrame(t, connection.send)
	if frameB.ChannelID != "opaque-b" {
		t.Fatalf("fast response channel = %q, want opaque-b", frameB.ChannelID)
	}
	responseB, err := relayOpen(frameB.Ciphertext, hostToDeviceB, []byte(relayRPCAAD("opaque-b", "host-device")))
	if err != nil {
		t.Fatal(err)
	}
	if string(responseB) != string(serverResponse("fast", "fast-result")) {
		t.Fatalf("fast response = %s", responseB)
	}

	// 释放 slow 后，A 的响应应归属 opaque-a。
	close(slowRelease)
	frameA := receiveRelayFrame(t, connection.send)
	if frameA.ChannelID != "opaque-a" {
		t.Fatalf("slow response channel = %q, want opaque-a", frameA.ChannelID)
	}
	responseA, err := relayOpen(frameA.Ciphertext, hostToDeviceA, []byte(relayRPCAAD("opaque-a", "host-device")))
	if err != nil {
		t.Fatal(err)
	}
	if string(responseA) != string(serverResponse("slow", "slow-result")) {
		t.Fatalf("slow response = %s", responseA)
	}

	stopRelayHost(t, cancel, connection, done)
}

func TestHostMapsEventStreamMethodToLocalDSHAndRoutesFrames(t *testing.T) {
	vector := loadRelayVector(t)
	hostNonce := decodeVectorValue(t, vector.HostNonce)

	eventFrame, _ := json.Marshal(map[string]any{
		"type":    "server-request",
		"rpcId":   "evt-1",
		"method":  "session/event",
		"payload": map[string]any{"type": "session/event"},
	})
	upstream := newRelayTestUpstream()
	upstream.streamMethods = map[string]bool{relayStreamMethodMux: true}
	upstream.stream = func(_ context.Context, method string) (relayStream, error) {
		if method != relayStreamMethodMux {
			t.Errorf("openStream method = %q, want events.mux", method)
		}
		return newFakeRelayStream(eventFrame), nil
	}

	connection := newFakeRelayHostConnection()
	host := newRelayHost(staticRelayIdentitySource{identity: vectorRelayHostIdentity(t, vector)}, staticRelayConnector{connection: connection}, upstream)
	host.randomBytes = sequenceRelayRandom(hostNonce)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- host.serveOnce(ctx) }()
	waitForRelayState(t, host, relayOnline)

	const channelID = "opaque-stream"
	deviceToHost, hostToDevice := openRelayTestChannel(t, connection, vector, channelID, hostNonce)

	// 以 events.mux 作为路由请求，触发本地 dsh 事件流映射。
	connection.receive <- relayEvent{Type: relayCiphertext, ChannelID: channelID, Ciphertext: relaySealRPCRequest(t, deviceToHost, channelID, relayStreamMethodMux, "rpc-stream")}

	frame := receiveRelayFrame(t, connection.send)
	if frame.ChannelID != channelID {
		t.Fatalf("stream frame channel = %q, want %q", frame.ChannelID, channelID)
	}
	plaintext, err := relayOpen(frame.Ciphertext, hostToDevice, []byte(relayRPCAAD(channelID, "host-device")))
	if err != nil {
		t.Fatal(err)
	}
	if string(plaintext) != string(eventFrame) {
		t.Fatalf("stream frame = %s, want %s", plaintext, eventFrame)
	}

	stopRelayHost(t, cancel, connection, done)
}

func TestSingleChannelFailureDoesNotAffectSibling(t *testing.T) {
	vector := loadRelayVector(t)
	hostNonceA := decodeVectorValue(t, vector.HostNonce)
	hostNonceB := relayTestNonce(0x55)

	upstream := newRelayTestUpstream()
	upstream.unary = func(_ context.Context, method string, _ []byte) ([]byte, error) {
		if method == "fail" {
			return nil, errors.New("upstream exploded")
		}
		return serverResponse("ok", "healthy"), nil
	}

	connection := newFakeRelayHostConnection()
	host := newRelayHost(staticRelayIdentitySource{identity: vectorRelayHostIdentity(t, vector)}, staticRelayConnector{connection: connection}, upstream)
	host.randomBytes = sequenceRelayRandom(hostNonceA, hostNonceB)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- host.serveOnce(ctx) }()
	waitForRelayState(t, host, relayOnline)

	deviceToHostA, _ := openRelayTestChannel(t, connection, vector, "opaque-a", hostNonceA)
	deviceToHostB, hostToDeviceB := openRelayTestChannel(t, connection, vector, "opaque-b", hostNonceB)

	// A 的 upstream 失败 → 仅 A 收到空帧关闭；B 继续健康。
	connection.receive <- relayEvent{Type: relayCiphertext, ChannelID: "opaque-a", Ciphertext: relaySealRPCRequest(t, deviceToHostA, "opaque-a", "fail", "rpc-a")}
	connection.receive <- relayEvent{Type: relayCiphertext, ChannelID: "opaque-b", Ciphertext: relaySealRPCRequest(t, deviceToHostB, "opaque-b", "ok", "rpc-b")}

	seenCloseA := false
	seenResponseB := false
	for !seenCloseA || !seenResponseB {
		frame := receiveRelayFrame(t, connection.send)
		switch frame.ChannelID {
		case "opaque-a":
			if len(frame.Ciphertext) != 0 {
				t.Fatalf("failed channel A frame should be empty close, got %d bytes", len(frame.Ciphertext))
			}
			seenCloseA = true
		case "opaque-b":
			response, err := relayOpen(frame.Ciphertext, hostToDeviceB, []byte(relayRPCAAD("opaque-b", "host-device")))
			if err != nil {
				t.Fatal(err)
			}
			if string(response) != string(serverResponse("ok", "healthy")) {
				t.Fatalf("healthy response = %s", response)
			}
			seenResponseB = true
		default:
			t.Fatalf("unexpected frame channel = %q", frame.ChannelID)
		}
	}

	stopRelayHost(t, cancel, connection, done)
}

func TestRelayReconnectBackoffIsBounded(t *testing.T) {
	host := newRelayHost(staticRelayIdentitySource{}, staticRelayConnector{}, &recordingRelayUpstream{})
	host.reconnectMin = 100 * time.Millisecond
	host.reconnectMax = 800 * time.Millisecond

	want := []time.Duration{200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond, 800 * time.Millisecond}
	current := host.reconnectMin
	for _, w := range want {
		current = host.reconnectAfter(current)
		if current != w {
			t.Fatalf("backoff = %v, want %v", current, w)
		}
	}
}

func TestHostReconnectsAfterConnectFailureWithBackoff(t *testing.T) {
	vector := loadRelayVector(t)
	connection := newFakeRelayHostConnection()
	connector := &flakyRelayConnector{failures: 2, connection: connection}
	upstream := newRelayTestUpstream()

	host := newRelayHost(staticRelayIdentitySource{identity: vectorRelayHostIdentity(t, vector)}, connector, upstream)
	host.randomBytes = sequenceRelayRandom(decodeVectorValue(t, vector.HostNonce))
	host.reconnectMin = 10 * time.Millisecond
	host.reconnectMax = 50 * time.Millisecond

	host.start()
	// 前两次 connect 失败，退避重连后第三次成功上线。
	waitForRelayState(t, host, relayOnline)
	if attempts := connector.attemptsSoFar(); attempts < 3 {
		t.Fatalf("connector attempts = %d, want >= 3 after two failures", attempts)
	}

	host.stop()
	waitForRelayState(t, host, relayOffline)
}

func TestHostReconnectsAndRequiresFreshHandshakeAfterDisconnect(t *testing.T) {
	vector := loadRelayVector(t)
	hostNonce1 := decodeVectorValue(t, vector.HostNonce)
	hostNonce2 := relayTestNonce(0x7e)

	conn1 := newFakeRelayHostConnection()
	conn2 := newFakeRelayHostConnection()
	connector := &sequencedRelayConnector{connections: []relayHostConnection{conn1, conn2}}
	upstream := newRelayTestUpstream()
	upstream.unary = func(context.Context, string, []byte) ([]byte, error) {
		return serverResponse("ok", "ok"), nil
	}

	host := newRelayHost(staticRelayIdentitySource{identity: vectorRelayHostIdentity(t, vector)}, connector, upstream)
	host.randomBytes = sequenceRelayRandom(hostNonce1, hostNonce2)
	host.reconnectMin = 10 * time.Millisecond
	host.reconnectMax = 50 * time.Millisecond

	host.start()
	waitForRelayState(t, host, relayOnline)

	// 第一次连接完成握手，记录旧密钥。
	oldDeviceToHost, _ := openRelayTestChannel(t, conn1, vector, "opaque-old", hostNonce1)

	// 断开连接，触发有界退避重连。
	conn1.closeReceive()
	waitForRelayConnectorIndex(t, connector, 2)
	waitForRelayState(t, host, relayOnline)

	// 旧密钥在新连接上必须被拒绝：新 channel 尚未握手，旧 RPC 密文无法通过握手。
	conn2.receive <- relayEvent{Type: relayChannelOpened, ChannelID: "opaque-replay"}
	conn2.receive <- relayEvent{Type: relayCiphertext, ChannelID: "opaque-replay", Ciphertext: relaySealRPCRequest(t, oldDeviceToHost, "opaque-replay", "session.list", "rpc-old")}
	rejected := receiveRelayFrame(t, conn2.send)
	if rejected.ChannelID != "opaque-replay" || len(rejected.Ciphertext) != 0 {
		t.Fatalf("old key replay was accepted on new connection: %+v", rejected)
	}

	// 新连接重新握手成功，密钥来自新 hostNonce。
	conn2.receive <- relayEvent{Type: relayChannelOpened, ChannelID: "opaque-fresh"}
	conn2.receive <- relayEvent{Type: relayCiphertext, ChannelID: "opaque-fresh", Ciphertext: vectorDeviceHello(t, vector, "opaque-fresh")}
	hostHello := receiveRelayFrame(t, conn2.send)
	if _, err := relayOpen(hostHello.Ciphertext, decodeVectorValue(t, vector.HostHelloKey), []byte(relayHelloAAD("opaque-fresh"))); err != nil {
		t.Fatalf("fresh handshake failed on reconnection: %v", err)
	}

	host.stop()
	waitForRelayState(t, host, relayOffline)
}

func waitForRelayConnectorIndex(t *testing.T, connector *sequencedRelayConnector, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if connector.current() == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("connector index = %d, want %d", connector.current(), want)
}

// flakyRelayConnector 前 failures 次 connect 失败，之后返回预置连接。
type flakyRelayConnector struct {
	mu         sync.Mutex
	failures   int
	attempts   int
	connection relayHostConnection
}

func (f *flakyRelayConnector) connect(context.Context, relayHostEndpoint) (relayHostConnection, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attempts++
	if f.attempts <= f.failures {
		return nil, errors.New("connect refused")
	}
	return f.connection, nil
}

func (f *flakyRelayConnector) attemptsSoFar() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.attempts
}

// stopRelayHost 取消上下文、关闭连接并等待 serveOnce 返回。
func stopRelayHost(t *testing.T, cancel context.CancelFunc, connection *fakeRelayHostConnection, done <-chan error) {
	t.Helper()
	cancel()
	connection.closeReceive()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Host Relay connection did not stop")
	}
}
