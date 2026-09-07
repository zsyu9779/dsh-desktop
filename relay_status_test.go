package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// errorRelayIdentitySource 固定返回错误，用于模拟 Account credential 过期等身份不可用。
type errorRelayIdentitySource struct{ err error }

func (s errorRelayIdentitySource) relayIdentity() (relayHostIdentity, error) {
	return relayHostIdentity{}, s.err
}

func TestRelayStatusCallbackReportsTransitions(t *testing.T) {
	vector := loadRelayVector(t)
	connection := newFakeRelayHostConnection()
	host := newRelayHost(staticRelayIdentitySource{identity: vectorRelayHostIdentity(t, vector)}, staticRelayConnector{connection: connection}, &recordingRelayUpstream{})
	states := make(chan relayStatus, 16)
	host.onStatus = func(s relayStatus) { states <- s }

	done := make(chan error, 1)
	go func() { done <- host.serveOnce(context.Background()) }()
	waitForRelayState(t, host, relayOnline)

	if first := <-states; first.State != relayConnecting {
		t.Fatalf("first status = %q, want connecting", first.State)
	}
	if second := <-states; second.State != relayOnline {
		t.Fatalf("second status = %q, want online", second.State)
	}

	connection.closeReceive()
	<-done
	if third := <-states; third.State != relayOffline {
		t.Fatalf("third status = %q, want offline", third.State)
	}
}

func TestRelayOfflineWhenIdentityExpired(t *testing.T) {
	host := newRelayHost(errorRelayIdentitySource{err: errors.New("Account credential 已过期")}, staticRelayConnector{}, &recordingRelayUpstream{})
	if err := host.serveOnce(context.Background()); err == nil {
		t.Fatal("serveOnce error = nil, want identity error")
	}
	status := host.status()
	if status.State != relayOffline {
		t.Fatalf("state = %q, want offline", status.State)
	}
	if !strings.Contains(status.Message, "Account credential 已过期") {
		t.Fatalf("message = %q, want expiry reason", status.Message)
	}
}

func TestRelayStartIsIdempotent(t *testing.T) {
	vector := loadRelayVector(t)
	connection := newFakeRelayHostConnection()
	connector := &flakyRelayConnector{failures: 0, connection: connection}
	host := newRelayHost(staticRelayIdentitySource{identity: vectorRelayHostIdentity(t, vector)}, connector, &recordingRelayUpstream{})
	host.start()
	host.start() // 第二次应为 no-op，不得建立第二条连接
	waitForRelayState(t, host, relayOnline)
	if n := connector.attemptsSoFar(); n != 1 {
		t.Fatalf("connector attempts = %d, want 1", n)
	}
	host.stop()
	waitForRelayState(t, host, relayOffline)
}

// manualRelayClock 提供可控的 sleepAfter：记录每次退避时长，返回由 advance 显式
// 关闭的 channel，从而在无真实时间流逝下确定性推进重连循环。
type manualRelayClock struct {
	mu     sync.Mutex
	delays []time.Duration
	waits  []chan time.Time
}

func (c *manualRelayClock) sleepAfter(d time.Duration) <-chan time.Time {
	ch := make(chan time.Time)
	c.mu.Lock()
	c.delays = append(c.delays, d)
	c.waits = append(c.waits, ch)
	c.mu.Unlock()
	return ch
}

func (c *manualRelayClock) advance() {
	c.mu.Lock()
	if len(c.waits) == 0 {
		c.mu.Unlock()
		return
	}
	ch := c.waits[0]
	c.waits = c.waits[1:]
	c.mu.Unlock()
	close(ch)
}

func (c *manualRelayClock) pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.waits)
}

func (c *manualRelayClock) snapshot() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.delays...)
}

func waitForPendingWaits(t *testing.T, clock *manualRelayClock, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if clock.pending() >= n {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("pending waits = %d, want >= %d", clock.pending(), n)
}

// closingRelayConnection 在 close 时回调一次，用于跟踪当前存活连接数。
type closingRelayConnection struct {
	inner   relayHostConnection
	onClose func()
	once    sync.Once
}

func (c *closingRelayConnection) receiveEvent() (relayEvent, error) { return c.inner.receiveEvent() }
func (c *closingRelayConnection) sendFrame(f relayFrame) error      { return c.inner.sendFrame(f) }
func (c *closingRelayConnection) close() error {
	c.once.Do(c.onClose)
	return c.inner.close()
}

// liveTrackingRelayConnector 前 failures 次失败，之后依次返回预置连接，并统计
// 当前存活连接数与历史成功连接数。
type liveTrackingRelayConnector struct {
	mu          sync.Mutex
	failures    int
	attempts    int
	conns       []relayHostConnection
	next        int
	live        int
	established int
}

func (c *liveTrackingRelayConnector) connect(context.Context, relayHostEndpoint) (relayHostConnection, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.attempts++
	if c.attempts <= c.failures {
		return nil, errors.New("connect refused")
	}
	if c.next >= len(c.conns) {
		return nil, errors.New("no more connections")
	}
	conn := c.conns[c.next]
	c.next++
	c.live++
	c.established++
	return &closingRelayConnection{inner: conn, onClose: func() {
		c.mu.Lock()
		c.live--
		c.mu.Unlock()
	}}, nil
}

func (c *liveTrackingRelayConnector) liveCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.live
}

func (c *liveTrackingRelayConnector) establishedCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.established
}

func TestRelayReconnectBackoffConvergesWithoutDuplicateConnections(t *testing.T) {
	vector := loadRelayVector(t)
	clock := &manualRelayClock{}
	conn1 := newFakeRelayHostConnection()
	conn2 := newFakeRelayHostConnection()
	connector := &liveTrackingRelayConnector{failures: 2, conns: []relayHostConnection{conn1, conn2}}
	host := newRelayHost(staticRelayIdentitySource{identity: vectorRelayHostIdentity(t, vector)}, connector, &recordingRelayUpstream{})
	host.randomBytes = sequenceRelayRandom(decodeVectorValue(t, vector.HostNonce))
	host.reconnectMin = 100 * time.Millisecond
	host.reconnectMax = 800 * time.Millisecond
	host.sleepAfter = clock.sleepAfter

	host.start()

	// 前两次 connect 失败：退避等待依次出现，状态停在 offline。
	waitForPendingWaits(t, clock, 1)
	if state := host.status().State; state != relayOffline {
		t.Fatalf("after first failure state = %q, want offline", state)
	}
	clock.advance()
	waitForPendingWaits(t, clock, 1)
	clock.advance()

	// 第三次 connect 成功上线，且仅有这一条存活连接。
	waitForRelayState(t, host, relayOnline)
	if live := connector.liveCount(); live != 1 {
		t.Fatalf("live connections = %d, want 1", live)
	}
	delays := clock.snapshot()
	if len(delays) != 2 || delays[0] != 200*time.Millisecond || delays[1] != 400*time.Millisecond {
		t.Fatalf("delays = %v, want [200ms 400ms]", delays)
	}

	// 断线后：旧连接关闭，退避重置为 min，重连到 conn2，仍只有一条存活连接。
	conn1.closeReceive()
	waitForPendingWaits(t, clock, 1)
	if live := connector.liveCount(); live != 0 {
		t.Fatalf("live connections after drop = %d, want 0", live)
	}
	clock.advance()
	waitForRelayState(t, host, relayOnline)
	if established := connector.establishedCount(); established != 2 {
		t.Fatalf("established connections = %d, want 2", established)
	}
	if live := connector.liveCount(); live != 1 {
		t.Fatalf("live connections after reconnect = %d, want 1", live)
	}
	delays = clock.snapshot()
	if len(delays) != 3 || delays[2] != 100*time.Millisecond {
		t.Fatalf("delays after drop = %v, want third entry 100ms", delays)
	}

	host.stop()
	waitForRelayState(t, host, relayOffline)
}

func TestRelayHandshakeMarksDeviceActive(t *testing.T) {
	vector := loadRelayVector(t)
	connection := newFakeRelayHostConnection()
	host := newRelayHost(staticRelayIdentitySource{identity: vectorRelayHostIdentity(t, vector)}, staticRelayConnector{connection: connection}, &recordingRelayUpstream{})
	host.randomBytes = sequenceRelayRandom(decodeVectorValue(t, vector.HostNonce))

	active := make(chan deviceActivity, 4)
	host.onDeviceActive = func(activity deviceActivity) {
		active <- activity
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go host.serveOnce(ctx)
	waitForRelayState(t, host, relayOnline)

	channelID := "opaque-channel-1"
	connection.receive <- relayEvent{Type: relayChannelOpened, ChannelID: channelID}
	connection.receive <- relayEvent{Type: relayCiphertext, ChannelID: channelID, Ciphertext: vectorDeviceHello(t, vector, channelID)}
	_ = receiveRelayFrame(t, connection.send) // host hello

	select {
	case got := <-active:
		if got.DeviceID != vector.DeviceID {
			t.Fatalf("active deviceID = %q, want %q", got.DeviceID, vector.DeviceID)
		}
		if got.Transport != deviceTransportRelay {
			t.Fatalf("active transport = %q, want relay", got.Transport)
		}
	case <-time.After(time.Second):
		t.Fatal("Relay handshake did not mark device active")
	}
}

func receiveDeviceActivity(t *testing.T, ch <-chan deviceActivity) deviceActivity {
	t.Helper()
	select {
	case a := <-ch:
		return a
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for device activity")
		return deviceActivity{}
	}
}

func TestRelayRPCTrafficRefreshesDeviceActivity(t *testing.T) {
	vector := loadRelayVector(t)
	hostNonce := decodeVectorValue(t, vector.HostNonce)
	upstream := newRelayTestUpstream()
	connection := newFakeRelayHostConnection()
	host := newRelayHost(staticRelayIdentitySource{identity: vectorRelayHostIdentity(t, vector)}, staticRelayConnector{connection: connection}, upstream)
	host.randomBytes = sequenceRelayRandom(hostNonce)

	active := make(chan deviceActivity, 8)
	host.onDeviceActive = func(a deviceActivity) { active <- a }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go host.serveOnce(ctx)
	waitForRelayState(t, host, relayOnline)

	deviceToHost, _ := openRelayTestChannel(t, connection, vector, "opaque-rpc", hostNonce)
	if first := receiveDeviceActivity(t, active); first.DeviceID != vector.DeviceID {
		t.Fatalf("handshake activity deviceID = %q, want %q", first.DeviceID, vector.DeviceID)
	}

	// 后续 RPC 应再次标记（刷新 LastActive），与 LAN 每次请求对称。
	connection.receive <- relayEvent{Type: relayCiphertext, ChannelID: "opaque-rpc", Ciphertext: relaySealRPCRequest(t, deviceToHost, "opaque-rpc", "session.list", "rpc-1")}
	_ = receiveRelayFrame(t, connection.send)
	second := receiveDeviceActivity(t, active)
	if second.DeviceID != vector.DeviceID || second.Transport != deviceTransportRelay {
		t.Fatalf("second activity = %+v, want relay for %q", second, vector.DeviceID)
	}
}

func TestRelayHeartbeatDetectsSilentPeer(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		// 静默：不读、不回 pong，模拟休眠唤醒后的死连接。
		<-release
		_ = conn.Close()
	}))
	defer func() { close(release); server.Close() }()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	wrapped := &websocketRelayHostConnection{connection: conn, done: make(chan struct{})}
	wrapped.startHeartbeat(30*time.Millisecond, 80*time.Millisecond)
	defer wrapped.close()

	// 读超时后 receiveEvent 必须报错（对端静默、无 pong），而非永久阻塞。
	errc := make(chan error, 1)
	go func() { _, err := wrapped.receiveEvent(); errc <- err }()
	select {
	case err := <-errc:
		if err == nil {
			t.Fatal("receiveEvent returned nil error on silent peer")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("heartbeat did not detect silent peer within 2s")
	}
}
