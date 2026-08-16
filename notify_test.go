package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
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
	mux.HandleFunc("/api/events.mux", func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		_ = c.WriteMessage(websocket.TextMessage, testFrame("question/requested", `{"type":"question/requested","sessionId":"s1","questions":[{"id":"q1","question":"hi"}]}`))
		_ = c.WriteMessage(websocket.TextMessage, testFrame("approval/requested", `{"type":"approval/requested","sessionId":"s1","toolName":"bash"}`))
		time.Sleep(200 * time.Millisecond)
	})
	mux.HandleFunc("/api/events.host", func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		time.Sleep(300 * time.Millisecond)
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

func TestDedupReplayedFrames(t *testing.T) {
	upgrader := websocket.Upgrader{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/events.mux", func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		// Same rpcId emitted twice, as dsh replays still-pending frames on reconnect.
		frame := []byte(`{"type":"server-request","rpcId":"r1","method":"question/requested","payload":{"type":"question/requested","sessionId":"s1","questions":[{"id":"q1","question":"hi"}]}}`)
		_ = c.WriteMessage(websocket.TextMessage, frame)
		_ = c.WriteMessage(websocket.TextMessage, frame)
		time.Sleep(200 * time.Millisecond)
	})
	mux.HandleFunc("/api/events.host", func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		time.Sleep(300 * time.Millisecond)
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
	mux.HandleFunc("/api/events.mux", func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		mu.Lock()
		connCount++
		n := connCount
		mu.Unlock()
		if n == 1 {
			_ = c.WriteMessage(websocket.TextMessage, []byte(`{"type":"server-request","rpcId":"a","method":"question/requested","payload":{"type":"question/requested","sessionId":"s1","questions":[{"id":"q1","question":"first"}]}}`))
			time.Sleep(50 * time.Millisecond)
			_ = c.Close()
			return
		}
		defer c.Close()
		_ = c.WriteMessage(websocket.TextMessage, []byte(`{"type":"server-request","rpcId":"b","method":"question/requested","payload":{"type":"question/requested","sessionId":"s2","questions":[{"id":"q2","question":"second"}]}}`))
		time.Sleep(300 * time.Millisecond)
	})
	mux.HandleFunc("/api/events.host", func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		time.Sleep(600 * time.Millisecond)
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

var _ = sync.Mutex{}
