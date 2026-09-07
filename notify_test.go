package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

var frameSeq int

func testFrame(method, payloadJSON string) []byte {
	frameSeq++
	return []byte(`{"type":"server-request","rpcId":"` + fmt.Sprintf("%d", frameSeq) + `","method":"` + method + `","payload":` + payloadJSON + `}`)
}

func TestClassifyQuestionAndApproval(t *testing.T) {
	cases := []struct {
		name        string
		raw         []byte
		wantType    string
		wantSession string
	}{
		{
			name:        "plain question",
			raw:         testFrame("question/requested", `{"type":"question/requested","sessionId":"s1","questions":[{"id":"q1","question":"what now?"}]}`),
			wantType:    "question",
			wantSession: "s1",
		},
		{
			name:        "plan review is approval",
			raw:         testFrame("question/requested", `{"type":"question/requested","sessionId":"s2","questions":[{"id":"q1","question":"approve plan?","intent":{"kind":"plan-review"}}]}`),
			wantType:    "approval",
			wantSession: "s2",
		},
		{
			name:        "tool approval",
			raw:         testFrame("approval/requested", `{"type":"approval/requested","sessionId":"s3","toolName":"bash"}`),
			wantType:    "approval",
			wantSession: "s3",
		},
		{
			name: "unrecognised frame is ignored",
			raw:  testFrame("session/event", `{"type":"session/event"}`),
		},
		{
			name:        "goal complete",
			raw:         testFrame("session/event", `{"type":"session/event","sessionId":"s5","event":{"type":"goal/change","data":{"operation":"complete"}}}`),
			wantType:    "completed",
			wantSession: "s5",
		},
		{
			name:        "goal block",
			raw:         testFrame("session/event", `{"type":"session/event","sessionId":"s5","event":{"type":"goal/change","data":{"operation":"block"}}}`),
			wantType:    "error",
			wantSession: "s5",
		},
		{
			name:        "turn error",
			raw:         testFrame("session/event", `{"type":"session/event","sessionId":"s6","event":{"type":"turn/end","data":{"reason":{"kind":"error"}}}}`),
			wantType:    "error",
			wantSession: "s6",
		},
		{
			name:        "turn blocked",
			raw:         testFrame("session/event", `{"type":"session/event","sessionId":"s6","event":{"type":"turn/end","data":{"reason":{"kind":"blocked"}}}}`),
			wantType:    "error",
			wantSession: "s6",
		},
		{
			name:        "agent error",
			raw:         testFrame("host/agent-error", `{"type":"host/agent-error","sessionId":"s7","message":"boom"}`),
			wantType:    "error",
			wantSession: "s7",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, ok := classifyFrame(tc.raw)
			if tc.wantType == "" {
				if ok {
					t.Fatalf("expected no classification, got %+v", n)
				}
				return
			}
			if !ok {
				t.Fatalf("expected classification, got none")
			}
			if n.Type != tc.wantType {
				t.Errorf("type = %q, want %q", n.Type, tc.wantType)
			}
			if n.SessionID != tc.wantSession {
				t.Errorf("sessionId = %q, want %q", n.SessionID, tc.wantSession)
			}
		})
	}
}

func TestSubscribeEmitsNotifications(t *testing.T) {
	upgrader := websocket.Upgrader{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/remote.mux", func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		_, _, _ = c.ReadMessage()
		_ = c.WriteMessage(websocket.TextMessage, muxItem(`{"type":"ready","clientId":"c1","host":{"home":"/tmp"}}`))
		_ = c.WriteMessage(websocket.TextMessage, muxItem(`{"type":"waterfall","event":"user-questions/request","eventId":"q1","agentId":"s1","request":{"questions":[{"id":"q1","question":"hi"}]}}`))
		_ = c.WriteMessage(websocket.TextMessage, muxItem(`{"type":"waterfall","event":"approval/request","eventId":"a1","agentId":"s1","request":{"toolName":"bash"}}`))
		time.Sleep(200 * time.Millisecond)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	n := newNotifyManager(nil)
	n.sink = make(chan Notification, 16)
	n.start(srv.URL)
	defer n.stop()

	var got []Notification
	timeout := time.After(2 * time.Second)
	for len(got) < 2 {
		select {
		case notif := <-n.sink:
			got = append(got, notif)
		case <-timeout:
			t.Fatalf("timed out; got %d notifications (%+v)", len(got), got)
		}
	}

	if got[0].Type != "question" || got[0].SessionID != "s1" {
		t.Errorf("first = %+v, want question/s1", got[0])
	}
	if got[1].Type != "approval" {
		t.Errorf("second = %+v, want approval", got[1])
	}
	if got[0].DeepLink == "" {
		t.Errorf("deepLink should be set to the base URL")
	}
	if got[0].TS == 0 {
		t.Errorf("ts should be set")
	}
}

func TestSubscribeThroughAuthenticatedEmbeddedProxy(t *testing.T) {
	upgrader := websocket.Upgrader{}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") == "one-time" {
			http.SetCookie(w, &http.Cookie{Name: "dsh-auth-test", Value: "session", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		if _, err := r.Cookie("dsh-auth-test"); err != nil {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		_, _ = fmt.Fprint(w, "ok")
	})
	mux.HandleFunc("/api/remote.mux", func(w http.ResponseWriter, r *http.Request) {
		if _, err := r.Cookie("dsh-auth-test"); err != nil {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		_, _, _ = c.ReadMessage()
		_ = c.WriteMessage(websocket.TextMessage, muxItem(`{"type":"waterfall","event":"user-questions/request","eventId":"auth-q1","agentId":"auth-s1","request":{"questions":[{"id":"q1","question":"hi"}]}}`))
		time.Sleep(300 * time.Millisecond)
	})
	upstream := httptest.NewServer(mux)
	t.Cleanup(upstream.Close)
	authenticatedURL, err := url.Parse(upstream.URL + "/?token=one-time")
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := newEmbeddedProxy(context.Background(), authenticatedURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(proxy.Close)

	n := newNotifyManager(nil)
	n.sink = make(chan Notification, 4)
	n.start(proxy.URL)
	t.Cleanup(n.stop)
	select {
	case got := <-n.sink:
		if got.SessionID != "auth-s1" {
			t.Fatalf("sessionId = %q, want auth-s1", got.SessionID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("authenticated notification websocket did not connect")
	}
}

func TestDedupReplayedFrames(t *testing.T) {
	upgrader := websocket.Upgrader{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/remote.mux", func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		_, _, _ = c.ReadMessage()
		// Same eventId emitted twice, as dsh can replay a still-pending waterfall.
		frame := muxItem(`{"type":"waterfall","event":"user-questions/request","eventId":"r1","agentId":"s1","request":{"questions":[{"id":"q1","question":"hi"}]}}`)
		_ = c.WriteMessage(websocket.TextMessage, frame)
		_ = c.WriteMessage(websocket.TextMessage, frame)
		time.Sleep(200 * time.Millisecond)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	n := newNotifyManager(nil)
	n.sink = make(chan Notification, 16)
	n.start(srv.URL)
	defer n.stop()

	select {
	case notif := <-n.sink:
		if notif.DedupeKey != "r1" {
			t.Errorf("dedupeKey = %q, want r1", notif.DedupeKey)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no notification received")
	}

	select {
	case notif := <-n.sink:
		t.Fatalf("expected duplicate to be dropped, got %+v", notif)
	case <-time.After(500 * time.Millisecond):
		// good: duplicate was dropped
	}
}

func TestReconnectAfterDrop(t *testing.T) {
	upgrader := websocket.Upgrader{}
	var mu sync.Mutex
	var connCount int
	mux := http.NewServeMux()
	mux.HandleFunc("/api/remote.mux", func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		mu.Lock()
		connCount++
		n := connCount
		mu.Unlock()
		_, _, _ = c.ReadMessage()
		if n == 1 {
			_ = c.WriteMessage(websocket.TextMessage, muxItem(`{"type":"waterfall","event":"user-questions/request","eventId":"a","agentId":"s1","request":{"questions":[{"id":"q1","question":"first"}]}}`))
			time.Sleep(50 * time.Millisecond)
			_ = c.Close()
			return
		}
		defer c.Close()
		_ = c.WriteMessage(websocket.TextMessage, muxItem(`{"type":"waterfall","event":"user-questions/request","eventId":"b","agentId":"s2","request":{"questions":[{"id":"q2","question":"second"}]}}`))
		time.Sleep(300 * time.Millisecond)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	n := newNotifyManager(nil)
	n.sink = make(chan Notification, 16)
	n.start(srv.URL)
	defer n.stop()

	var got []Notification
	timeout := time.After(5 * time.Second)
	for len(got) < 2 {
		select {
		case notif := <-n.sink:
			got = append(got, notif)
		case <-timeout:
			t.Fatalf("timed out; got %d notifications (%+v)", len(got), got)
		}
	}
	if got[0].DedupeKey != "a" {
		t.Errorf("first dedupeKey = %q, want a", got[0].DedupeKey)
	}
	if got[1].DedupeKey != "b" {
		t.Errorf("second dedupeKey = %q, want b", got[1].DedupeKey)
	}
}

func muxItem(value string) []byte {
	return []byte(`{"type":"item","streamId":"` + notifyMuxStreamID + `","value":` + value + `}`)
}
