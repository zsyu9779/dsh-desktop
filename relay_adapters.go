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
	return &websocketRelayHostConnection{connection: connection}, nil
}

type websocketRelayHostConnection struct {
	connection *websocket.Conn
}

func (c *websocketRelayHostConnection) receiveEvent() (relayEvent, error) {
	var event relayEvent
	err := c.connection.ReadJSON(&event)
	return event, err
}
func (c *websocketRelayHostConnection) sendFrame(frame relayFrame) error {
	return c.connection.WriteJSON(frame)
}
func (c *websocketRelayHostConnection) close() error { return c.connection.Close() }

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

func relayServerURL() string { return strings.TrimSpace(os.Getenv("DSH_RELAY_URL")) }
