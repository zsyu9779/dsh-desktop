package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type accountRelayIdentitySource struct {
	account  *accountManager
	pairings *remoteSetupManager
}

func (s accountRelayIdentitySource) relayIdentity() (relayHostIdentity, error) {
	credential, identity, err := s.account.remoteIdentity()
	if err != nil {
		return relayHostIdentity{}, err
	}
	// Host Account 身份已是 X25519 key-agreement key（ticket 32）；Relay 握手
	// 用它派生密钥，绝不复用旧 Ed25519 身份。
	privateKey, err := identity.x25519PrivateKey()
	if err != nil {
		return relayHostIdentity{}, errors.New("Host Relay identity unavailable")
	}
	return relayHostIdentity{
		AccountID: credential.AccountID, AccessToken: credential.AccessToken,
		HostID: identity.HostID, PrivateKey: privateKey, Pairings: s.pairings.relayPairings(),
	}, nil
}

type websocketRelayHostConnector struct {
	url    string
	dialer *websocket.Dialer
}

const relayHostReadLimit = 2 << 20

// Relay 心跳：Host 定期发 ping，并要求在 pongWait 内收到 pong。macOS 休眠唤醒后
// 旧 TCP 连接会静默失效，读超时让 receiveEvent 报错并触发有界退避重连，而不是
// 永远停在"在线"的假象上。
const (
	relayPingPeriod   = 20 * time.Second
	relayPongWait     = 60 * time.Second
	relayWriteTimeout = 10 * time.Second
)

func newWebsocketRelayHostConnector(rawURL string) *websocketRelayHostConnector {
	return &websocketRelayHostConnector{url: strings.TrimSpace(rawURL), dialer: websocket.DefaultDialer}
}

func (c *websocketRelayHostConnector) connect(ctx context.Context, endpoint relayHostEndpoint) (relayHostConnection, error) {
	if c.url == "" {
		return nil, errors.New("Relay 服务地址未配置")
	}
	parsed, err := url.Parse(c.url)
	if err != nil || parsed.Scheme != "wss" {
		return nil, errors.New("Relay 服务地址无效")
	}
	query := parsed.Query()
	query.Set("hostID", endpoint.HostID)
	parsed.RawQuery = query.Encode()
	header := http.Header{"Authorization": {"Bearer " + endpoint.AccessToken}}
	connection, response, err := c.dialer.DialContext(ctx, parsed.String(), header)
	if response != nil && response.Body != nil {
		response.Body.Close()
	}
	if err != nil {
		return nil, err
	}
	connection.SetReadLimit(relayHostReadLimit)
	wrapped := &websocketRelayHostConnection{connection: connection, done: make(chan struct{})}
	wrapped.startHeartbeat(relayPingPeriod, relayPongWait)
	return wrapped, nil
}

type websocketRelayHostConnection struct {
	connection *websocket.Conn
	done       chan struct{}
	once       sync.Once
}

// startHeartbeat 设置读超时与 pong 处理，并启动一个周期 ping 的 goroutine。
// 读超时到期会让 ReadJSON 报错，从而让上层 serveOnce 结束并进入重连。
func (c *websocketRelayHostConnection) startHeartbeat(pingPeriod, pongWait time.Duration) {
	if pingPeriod <= 0 || pongWait <= 0 {
		return
	}
	_ = c.connection.SetReadDeadline(time.Now().Add(pongWait))
	c.connection.SetPongHandler(func(string) error {
		return c.connection.SetReadDeadline(time.Now().Add(pongWait))
	})
	go func() {
		ticker := time.NewTicker(pingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := c.connection.WriteControl(websocket.PingMessage, nil, time.Now().Add(relayWriteTimeout)); err != nil {
					return
				}
			case <-c.done:
				return
			}
		}
	}()
}

func (c *websocketRelayHostConnection) receiveEvent() (relayEvent, error) {
	var event relayEvent
	err := c.connection.ReadJSON(&event)
	return event, err
}
func (c *websocketRelayHostConnection) sendFrame(frame relayFrame) error {
	return c.connection.WriteJSON(frame)
}
func (c *websocketRelayHostConnection) close() error {
	c.once.Do(func() { close(c.done) })
	return c.connection.Close()
}

type dshRelayUpstream struct {
	url    func() string
	client *http.Client
}

func newDSHRelayUpstream(url func() string) *dshRelayUpstream {
	return &dshRelayUpstream{url: url, client: &http.Client{Timeout: 30 * time.Second}}
}

func (u *dshRelayUpstream) call(ctx context.Context, method string, body []byte) ([]byte, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(u.url()), "/")
	if baseURL == "" {
		return nil, errors.New("本地 dsh 尚未就绪")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/"+url.PathEscape(method), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := u.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	// 非 2xx 表示本地 dsh 的 RPC 端点未正常处理请求（trust 栅栏拒绝、方法不存在
	// 或内部错误），其 body 不是合法 RPC envelope，不能当作成功响应加密返回。
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("local dsh returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("local dsh returned HTTP %d without an RPC envelope", response.StatusCode)
	}
	return data, nil
}

const (
	// relayStreamMethodMux 是 dsh 的 Session 事件流下传通道 method。
	relayStreamMethodMux = "events.mux"
	// relayStreamMethodHost 是 dsh 的 Host 事件流下传通道 method。
	relayStreamMethodHost = "events.host"
)

// isStreamMethod 报告 method 是否映射到本地 dsh 的事件流 WebSocket 下传通道。
// dsh 只暴露 events.mux（Session 事件）与 events.host（Host 事件）两个下传流。
func (u *dshRelayUpstream) isStreamMethod(method string) bool {
	return method == relayStreamMethodMux || method == relayStreamMethodHost
}

// openStream 打开本地 dsh 的 WebSocket/事件流。method 选择目标下传通道，
// 不上行给 dsh（dsh 事件流禁止 Device 侧消息）。
func (u *dshRelayUpstream) openStream(ctx context.Context, method string) (relayStream, error) {
	if !u.isStreamMethod(method) {
		return nil, fmt.Errorf("method %q 不是本地 dsh 事件流", method)
	}
	baseURL := strings.TrimRight(strings.TrimSpace(u.url()), "/")
	if baseURL == "" {
		return nil, errors.New("本地 dsh 尚未就绪")
	}
	wsURL, err := wsURLFor(baseURL, "/api/"+url.PathEscape(method))
	if err != nil {
		return nil, err
	}
	connection, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return nil, err
	}
	connection.SetReadLimit(relayHostReadLimit)
	return &dshRelayStream{connection: connection}, nil
}

// dshRelayStream 是本地 dsh 事件流的下传句柄。
type dshRelayStream struct {
	connection *websocket.Conn
}

func (s *dshRelayStream) receiveFrame() ([]byte, error) {
	_, data, err := s.connection.ReadMessage()
	return data, err
}

func (s *dshRelayStream) close() error { return s.connection.Close() }

func relayServerURL() string { return strings.TrimSpace(os.Getenv("DSH_RELAY_URL")) }
