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

// classifyFrame parses one server-request frame and returns a Notification if
// it is a high-value event this milestone recognises (question or approval).
func classifyFrame(raw []byte) (Notification, bool) {
	var env serverRequest
	if err := json.Unmarshal(raw, &env); err != nil {
		return Notification{}, false
	}
	var p framePayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return Notification{}, false
	}

	switch p.Type {
	case "question/requested":
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
		summary := "工具请求授权"
		if p.ToolName != "" {
			summary = p.ToolName + " 请求授权"
		}
		return Notification{Type: "approval", SessionID: p.SessionID, Summary: summary}, true
	default:
		return Notification{}, false
	}
}

// notifyManager subscribes to dsh's real-time event streams and surfaces
// high-value events as Notifications to the frontend panel.
type notifyManager struct {
	app *App

	mu      sync.Mutex
	running bool
	conns   []*websocket.Conn
	baseURL string

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
	wsURL, err := wsURLFor(baseURL, path)
	if err != nil {
		n.logf("notify: %v", err)
		return
	}
	c, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		n.logf("notify: dial %s: %v", path, err)
		return
	}

	n.mu.Lock()
	if !n.running {
		n.mu.Unlock()
		_ = c.Close()
		return
	}
	n.conns = append(n.conns, c)
	n.mu.Unlock()

	for {
		_, msg, err := c.ReadMessage()
		if err != nil {
			break
		}
		if notif, ok := classifyFrame(msg); ok {
			n.emit(notif)
		}
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
