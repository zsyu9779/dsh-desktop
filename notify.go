package main

import (
	"encoding/json"
	"fmt"
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

	for _, p := range []string{"/api/events.mux", "/api/events.host"} {
		go n.subscribe(baseURL, p)
	}
}

func (n *notifyManager) subscribe(baseURL, path string) {
	backoff := 500 * time.Millisecond
	const maxBackoff = 30 * time.Second

	for {
		if !n.isRunning() {
			return
		}
		wsURL, err := wsURLFor(baseURL, path)
		if err != nil {
			n.logf("notify: %v", err)
			return
		}
		c, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			n.logf("notify: dial %s: %v (retry in %v)", path, err, backoff)
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

		// A successful connect resets the backoff for the next failure.
		backoff = 500 * time.Millisecond

		for {
			_, msg, err := c.ReadMessage()
			if err != nil {
				break
			}
			if notif, ok := classifyFrame(msg); ok {
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
	n.mu.Unlock()

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
