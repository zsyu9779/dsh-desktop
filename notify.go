package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Notification is a structured, host-generated signal of a high-value Session
// event, surfaced to the Owner's Devices. See CONTEXT.md.
type Notification struct {
	Type      string `json:"type"` // question | approval | completed | error
	SessionID string `json:"sessionId"`
	Summary   string `json:"summary"`
	DeepLink  string `json:"deepLink"`
	TS        int64  `json:"ts"`
	DedupeKey string `json:"dedupeKey"`
}

// serverRequest is the dsh wire envelope that wraps every event frame. The
// field names match dsh's JSON keys case-insensitively, so no tags are needed.
type serverRequest struct {
	Type    string
	Method  string
	RPCID   string
	Payload json.RawMessage
}

// framePayload is the subset of a frame payload that classifyFrame reads.
type framePayload struct {
	Type      string
	SessionID string
	Questions []questionItem
	ToolName  string
}

type questionItem struct {
	ID       string
	Question string
	Intent   *struct {
		Kind string
	}
}

// sessionEventFrame is the payload of a session/event mux frame. Event.Data
// holds the per-event payload (goal/change -> operation; turn/end -> reason.kind).
type sessionEventFrame struct {
	Type      string
	SessionID string
	Event     struct {
		Type string
		Data struct {
			Operation string
			Reason    struct {
				Kind string
			}
		}
	}
}

// agentErrorFrame is the payload of a host/agent-error host frame.
type agentErrorFrame struct {
	Type      string
	SessionID string
	Message   string
}

const notifyMuxStreamID = "dsh-desktop-notify"

type gatewayMuxFrame struct {
	Type     string          `json:"type"`
	StreamID string          `json:"streamId"`
	Value    json.RawMessage `json:"value"`
}

type forwardedEventFrame struct {
	Type    string            `json:"type"`
	Event   string            `json:"event"`
	EventID string            `json:"eventId"`
	AgentID string            `json:"agentId"`
	Args    []json.RawMessage `json:"args"`
	Request json.RawMessage   `json:"request"`
}

// classifyFrame parses one server-request frame and returns a Notification if
// it is a high-value event (question, approval, completed, or error).
func classifyFrame(raw []byte) (n Notification, ok bool) {
	var env serverRequest
	if err := json.Unmarshal(raw, &env); err != nil {
		return Notification{}, false
	}
	// Stamp the dedupe key from the envelope rpcId, which dsh reuses verbatim
	// when it replays still-pending question/approval frames on reconnect.
	defer func() {
		if ok && n.DedupeKey == "" {
			n.DedupeKey = env.RPCID
		}
	}()

	// Peek the frame type so the payload can be parsed into the right shape.
	var probe struct {
		Type string
	}
	if err := json.Unmarshal(env.Payload, &probe); err != nil {
		return Notification{}, false
	}

	switch probe.Type {
	case "question/requested":
		var p framePayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return Notification{}, false
		}
		planReview := false
		summary := ""
		for _, q := range p.Questions {
			if q.Intent != nil && q.Intent.Kind == "plan-review" {
				planReview = true
			}
			if summary == "" {
				summary = q.Question
			}
		}
		n := Notification{Type: "question", SessionID: p.SessionID, Summary: summary}
		if planReview {
			n.Type = "approval"
			if n.Summary == "" {
				n.Summary = "计划审批待处理"
			}
		}
		if n.Summary == "" {
			n.Summary = "新的提问"
		}
		return n, true

	case "approval/requested":
		var p framePayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return Notification{}, false
		}
		summary := "工具请求授权"
		if p.ToolName != "" {
			summary = p.ToolName + " 请求授权"
		}
		return Notification{Type: "approval", SessionID: p.SessionID, Summary: summary}, true

	case "session/event":
		var f sessionEventFrame
		if err := json.Unmarshal(env.Payload, &f); err != nil {
			return Notification{}, false
		}
		switch f.Event.Type {
		case "goal/change":
			switch f.Event.Data.Operation {
			case "complete":
				return Notification{Type: "completed", SessionID: f.SessionID, Summary: "目标已完成"}, true
			case "block":
				return Notification{Type: "error", SessionID: f.SessionID, Summary: "目标被阻塞"}, true
			}
		case "turn/end":
			switch f.Event.Data.Reason.Kind {
			case "error":
				return Notification{Type: "error", SessionID: f.SessionID, Summary: "任务出错"}, true
			case "blocked":
				return Notification{Type: "error", SessionID: f.SessionID, Summary: "任务被阻塞"}, true
			}
		}
		return Notification{}, false

	case "host/agent-error":
		var f agentErrorFrame
		if err := json.Unmarshal(env.Payload, &f); err != nil {
			return Notification{}, false
		}
		summary := f.Message
		if summary == "" {
			summary = "Agent 出错"
		}
		return Notification{Type: "error", SessionID: f.SessionID, Summary: summary}, true

	default:
		return Notification{}, false
	}
}

// classifyMuxFrame unwraps DSH 0.1.2's /api/remote.mux carrier and maps the
// forwarded application events that matter to desktop notifications.
func classifyMuxFrame(raw []byte) (n Notification, ok, terminal bool) {
	var mux gatewayMuxFrame
	if err := json.Unmarshal(raw, &mux); err != nil || mux.StreamID != notifyMuxStreamID {
		return Notification{}, false, false
	}
	if mux.Type == "end" || mux.Type == "error" {
		return Notification{}, false, true
	}
	if mux.Type != "item" || len(mux.Value) == 0 {
		return Notification{}, false, false
	}
	var event forwardedEventFrame
	if err := json.Unmarshal(mux.Value, &event); err != nil || event.Type == "ready" {
		return Notification{}, false, false
	}
	switch event.Type + ":" + event.Event {
	case "emit:api-session/status":
		var sessionID string
		var running bool
		if len(event.Args) != 2 || json.Unmarshal(event.Args[0], &sessionID) != nil || json.Unmarshal(event.Args[1], &running) != nil || running {
			return Notification{}, false, false
		}
		return Notification{Type: "completed", SessionID: sessionID, Summary: "任务已完成", DedupeKey: event.EventID}, true, false
	case "emit:api-session/error":
		var sessionID, message string
		if len(event.Args) != 2 || json.Unmarshal(event.Args[0], &sessionID) != nil || json.Unmarshal(event.Args[1], &message) != nil {
			return Notification{}, false, false
		}
		if message == "" {
			message = "任务出错"
		}
		return Notification{Type: "error", SessionID: sessionID, Summary: message, DedupeKey: event.EventID}, true, false
	case "waterfall:approval/request":
		var request struct {
			ToolName string `json:"toolName"`
			Reason   string `json:"reason"`
		}
		if json.Unmarshal(event.Request, &request) != nil {
			return Notification{}, false, false
		}
		summary := request.ToolName + " 请求授权"
		if request.ToolName == "" {
			summary = "工具请求授权"
		}
		return Notification{Type: "approval", SessionID: event.AgentID, Summary: summary, DedupeKey: event.EventID}, true, false
	case "waterfall:user-questions/request":
		var request struct {
			Questions []questionItem `json:"questions"`
		}
		if json.Unmarshal(event.Request, &request) != nil || len(request.Questions) == 0 {
			return Notification{}, false, false
		}
		typ := "question"
		if request.Questions[0].Intent != nil && request.Questions[0].Intent.Kind == "plan-review" {
			typ = "approval"
		}
		return Notification{Type: typ, SessionID: event.AgentID, Summary: request.Questions[0].Question, DedupeKey: event.EventID}, true, false
	default:
		return Notification{}, false, false
	}
}

// maxSeenDedupe bounds the in-memory set of recently emitted dedupe keys.
const maxSeenDedupe = 256

// notifyManager subscribes to dsh's real-time event streams and surfaces
// high-value events as Notifications to the frontend panel.
type notifyManager struct {
	app *App

	mu      sync.Mutex
	running bool
	conns   []*websocket.Conn
	baseURL string
	seen    map[string]struct{}

	// sink is an injectable channel used by tests; when nil, notifications are
	// emitted to the frontend via Wails.
	sink chan Notification

	// bridge 派生面向每个目标 Device 的加密 Notification envelope；nil 时仅前端通知。
	bridge *notificationBridge

	// fanOutSink 接收派生的 fan-out（测试注入点）；nil 时静默丢弃，投递见后续 ticket。
	fanOutSink chan notificationFanOut
}

func newNotifyManager(app *App) *notifyManager {
	return &notifyManager{app: app}
}

// start opens WebSocket subscriptions to dsh's two event streams.
func (n *notifyManager) start(baseURL string) {
	n.mu.Lock()
	if n.running {
		n.mu.Unlock()
		return
	}
	n.running = true
	n.baseURL = baseURL
	n.mu.Unlock()

	go n.subscribe(baseURL)
}

func (n *notifyManager) subscribe(baseURL string) {
	backoff := 500 * time.Millisecond
	const maxBackoff = 30 * time.Second

	for {
		if !n.isRunning() {
			return
		}
		wsURL, err := wsURLFor(baseURL, "/api/remote.mux")
		if err != nil {
			n.logf("notify: %v", err)
			return
		}
		authHeader, err := websocketAuthHeader(baseURL)
		if err != nil {
			n.logf("notify: %v", err)
			return
		}
		c, _, err := websocket.DefaultDialer.Dial(wsURL, authHeader)
		if err != nil {
			n.logf("notify: dial /api/remote.mux: %v (retry in %v)", err, backoff)
			if !n.sleepOrStop(backoff) {
				return
			}
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}

		n.mu.Lock()
		if !n.running {
			n.mu.Unlock()
			_ = c.Close()
			return
		}
		n.conns = append(n.conns, c)
		n.mu.Unlock()
		if err := c.WriteJSON(map[string]any{
			"type":     "open",
			"streamId": notifyMuxStreamID,
			"endpoint": "$events",
			"payload":  map[string]any{"args": map[string]any{}},
		}); err != nil {
			_ = c.Close()
			continue
		}

		// A successful connect resets the backoff for the next failure.
		backoff = 500 * time.Millisecond

		for {
			_, msg, err := c.ReadMessage()
			if err != nil {
				break
			}
			if notif, ok, terminal := classifyMuxFrame(msg); terminal {
				break
			} else if ok {
				n.emit(notif)
			}
		}

		// Connection dropped: unregister, then the loop re-dials.
		n.mu.Lock()
		n.conns = removeConn(n.conns, c)
		n.mu.Unlock()
		_ = c.Close()
	}
}

func (n *notifyManager) emit(notif Notification) {
	if notif.TS == 0 {
		notif.TS = time.Now().UnixMilli()
	}
	if notif.DeepLink == "" {
		notif.DeepLink = n.baseURL
	}
	if notif.DedupeKey == "" {
		notif.DedupeKey = fmt.Sprintf("%s:%s", notif.Type, notif.SessionID)
	}

	n.mu.Lock()
	if n.seen == nil {
		n.seen = make(map[string]struct{})
	}
	if _, dup := n.seen[notif.DedupeKey]; dup {
		n.mu.Unlock()
		return
	}
	if len(n.seen) >= maxSeenDedupe {
		n.seen = make(map[string]struct{})
	}
	n.seen[notif.DedupeKey] = struct{}{}
	sink := n.sink
	bridge := n.bridge
	fanOutSink := n.fanOutSink
	n.mu.Unlock()

	// 去重门控通过后，把 Notification 派生为面向每个目标 Device 的加密 envelope。
	// 派生失败（如 Host 未登录 Account、无有效 Pairing）不影响前端通知。
	if bridge != nil {
		if fanOut, err := bridge.fanOut(notif); err == nil && fanOutSink != nil {
			select {
			case fanOutSink <- fanOut:
			default:
			}
		}
	}

	if sink != nil {
		select {
		case sink <- notif:
		default:
		}
		return
	}
	if n.app != nil && n.app.ctx != nil {
		runtime.EventsEmit(n.app.ctx, "notifications", notif)
	}
}

func (n *notifyManager) isRunning() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.running
}

// sleepOrStop sleeps in small increments so stop() stays responsive; it returns
// false if the manager was stopped during the sleep.
func (n *notifyManager) sleepOrStop(d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if !n.isRunning() {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
	return true
}

func removeConn(conns []*websocket.Conn, target *websocket.Conn) []*websocket.Conn {
	out := conns[:0]
	for _, c := range conns {
		if c != target {
			out = append(out, c)
		}
	}
	return out
}

func (n *notifyManager) stop() {
	n.mu.Lock()
	n.running = false
	conns := n.conns
	n.conns = nil
	n.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
}

func (n *notifyManager) logf(format string, args ...interface{}) {
	if n.app != nil && n.app.dsh != nil {
		n.app.dsh.logf("[notify] "+format, args...)
	}
}

// wsURLFor derives the WebSocket URL for a dsh event path from the dsh web UI
// base URL (http/https -> ws/wss, same host).
func wsURLFor(baseURL, path string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("bad base url %q: %w", baseURL, err)
	}
	scheme := "ws"
	if u.Scheme == "https" {
		scheme = "wss"
	}
	return scheme + "://" + u.Host + path, nil
}

// websocketAuthHeader carries the embedded proxy capability explicitly.
// gorilla/websocket deliberately does not infer Basic Auth from URL userinfo.
func websocketAuthHeader(baseURL string) (http.Header, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("bad base url %q: %w", baseURL, err)
	}
	header := http.Header{}
	if u.User != nil {
		password, _ := u.User.Password()
		request := &http.Request{Header: header}
		request.SetBasicAuth(u.User.Username(), password)
	}
	return header, nil
}
