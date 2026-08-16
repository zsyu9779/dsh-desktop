package main

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func testFrame(method, payloadJSON string) []byte {
	return []byte(`{"type":"server-request","rpcId":"1","method":"` + method + `","payload":` + payloadJSON + `}`)
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

var _ = sync.Mutex{}
