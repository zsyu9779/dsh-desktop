package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestWebSocketRelayConnectorAuthenticatesHostOnOutboundTLSConnection(t *testing.T) {
	connected := make(chan struct{}, 1)
	upgrader := websocket.Upgrader{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("hostID") != "host-1" {
			t.Errorf("hostID = %q", request.URL.Query().Get("hostID"))
		}
		if request.Header.Get("Authorization") != "Bearer account-token" {
			t.Errorf("Authorization = %q", request.Header.Get("Authorization"))
		}
		connection, err := upgrader.Upgrade(response, request, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer connection.Close()
		connected <- struct{}{}
		_ = connection.WriteJSON(relayEvent{Type: relayChannelOpened, ChannelID: "opaque-channel"})
		var frame relayFrame
		if err := connection.ReadJSON(&frame); err != nil {
			t.Error(err)
			return
		}
		if frame.ChannelID != "opaque-channel" || string(frame.Ciphertext) != "opaque" {
			t.Errorf("frame = %+v", frame)
		}
	}))
	defer server.Close()

	connector := newWebsocketRelayHostConnector("wss" + strings.TrimPrefix(server.URL, "https"))
	connector.dialer = &websocket.Dialer{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} // 测试服务器证书
	connection, err := connector.connect(context.Background(), relayHostEndpoint{HostID: "host-1", AccessToken: "account-token"})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.close()
	<-connected
	event, err := connection.receiveEvent()
	if err != nil || event.Type != relayChannelOpened || event.ChannelID != "opaque-channel" {
		t.Fatalf("event = %+v, err = %v", event, err)
	}
	if err := connection.sendFrame(relayFrame{ChannelID: event.ChannelID, Ciphertext: []byte("opaque")}); err != nil {
		t.Fatal(err)
	}
}

func TestWebSocketRelayConnectorRejectsPlaintextTransportAndServerRejection(t *testing.T) {
	plaintext := newWebsocketRelayHostConnector("ws://relay.example/host")
	if _, err := plaintext.connect(context.Background(), relayHostEndpoint{HostID: "host-1", AccessToken: "secret"}); err == nil {
		t.Fatal("plaintext Relay transport was accepted")
	}

	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		http.Error(response, "rejected", http.StatusForbidden)
	}))
	defer server.Close()
	connector := newWebsocketRelayHostConnector("wss" + strings.TrimPrefix(server.URL, "https"))
	connector.dialer = &websocket.Dialer{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} // 测试服务器证书
	if _, err := connector.connect(context.Background(), relayHostEndpoint{HostID: "host-1", AccessToken: "secret"}); err == nil {
		t.Fatal("Relay HTTP rejection was reported as connected")
	}
}

func TestWebSocketRelayConnectorBoundsUntrustedRelayFrames(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		connection, err := upgrader.Upgrade(response, request, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer connection.Close()
		_ = connection.WriteMessage(websocket.TextMessage, []byte(strings.Repeat("x", relayHostReadLimit+1)))
	}))
	defer server.Close()
	connector := newWebsocketRelayHostConnector("wss" + strings.TrimPrefix(server.URL, "https"))
	connector.dialer = &websocket.Dialer{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} // 测试服务器证书
	connection, err := connector.connect(context.Background(), relayHostEndpoint{HostID: "host-1", AccessToken: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.close()
	if _, err := connection.receiveEvent(); err == nil {
		t.Fatal("oversized Relay frame was accepted")
	}
}

func TestDSHRelayUpstreamForwardsTheRealRPCEnvelope(t *testing.T) {
	want := []byte(`{"type":"client-request","rpcId":"rpc-1","method":"session.list","payload":{"workspace":"alpha"}}`)
	response := []byte(`{"type":"server-response","rpcId":"rpc-1","result":{"ok":true,"value":[]}}`)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/api/session.list" {
			t.Errorf("request = %s %s", request.Method, request.URL.Path)
		}
		body, _ := io.ReadAll(request.Body)
		if string(body) != string(want) {
			t.Errorf("body = %s, want %s", body, want)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write(response)
	}))
	defer server.Close()

	upstream := newDSHRelayUpstream(func() string { return server.URL })
	got, err := upstream.call(context.Background(), "session.list", want)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(response) {
		t.Fatalf("response = %s, want %s", got, response)
	}
}

func TestDSHRelayUpstreamRejectsAnEmptyNonRPCResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	upstream := newDSHRelayUpstream(func() string { return server.URL })
	if _, err := upstream.call(context.Background(), "session.list", []byte(`{"type":"client-request"}`)); err == nil {
		t.Fatal("empty upstream rejection was accepted as an RPC response")
	}
}

func TestDSHRelayUpstreamRejectsNonSuccessHTTPStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusForbidden)
		_, _ = writer.Write([]byte(`{"type":"server-response","rpcId":"rpc-1"}`))
	}))
	defer server.Close()
	upstream := newDSHRelayUpstream(func() string { return server.URL })
	if _, err := upstream.call(context.Background(), "session.list", []byte(`{"type":"client-request","rpcId":"rpc-1","method":"session.list"}`)); err == nil {
		t.Fatal("non-2xx upstream response with a body was accepted as an RPC response")
	}
}

func TestPairingRegistrySuppliesOnlyX25519DeviceIdentitiesToRelay(t *testing.T) {
	vector := loadRelayVector(t)
	account, store := signedInAccountManager(t)
	manager := newRemoteSetupManager(account, &fakeRemoteSetupServer{}, store, timeNowForRelayTest)
	manager.devices["valid"] = pairedDevice{
		PairingID: "valid", DeviceID: "device-1",
		DeviceIdentityPublicKey: decodeVectorValue(t, vector.DevicePublicKey),
	}
	manager.devices["legacy"] = pairedDevice{PairingID: "legacy", DeviceID: "device-old"}

	identity, err := (accountRelayIdentitySource{account: account, pairings: manager}).relayIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if len(identity.Pairings) != 1 || identity.Pairings[0].PairingID != "valid" {
		encoded, _ := json.Marshal(identity.Pairings)
		t.Fatalf("Relay Pairings = %s, want only valid X25519 identity", encoded)
	}
}

func TestDSHRelayUpstreamRoutesStreamMethodsToEventWebSocket(t *testing.T) {
	var gotPath string
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		gotPath = request.URL.Path
		connection, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer connection.Close()
		_ = connection.WriteMessage(websocket.TextMessage, []byte(`{"type":"server-request","rpcId":"e1","method":"session/event"}`))
	}))
	defer server.Close()

	upstream := newDSHRelayUpstream(func() string { return server.URL })
	if !upstream.isStreamMethod(relayStreamMethodMux) || !upstream.isStreamMethod(relayStreamMethodHost) {
		t.Fatal("events.mux / events.host should be stream methods")
	}
	if upstream.isStreamMethod("session.list") {
		t.Fatal("session.list should not be a stream method")
	}

	stream, err := upstream.openStream(context.Background(), relayStreamMethodMux)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.close()
	frame, err := stream.receiveFrame()
	if err != nil {
		t.Fatal(err)
	}
	if string(frame) != `{"type":"server-request","rpcId":"e1","method":"session/event"}` {
		t.Fatalf("frame = %s", frame)
	}
	if gotPath != "/api/"+relayStreamMethodMux {
		t.Fatalf("path = %q, want /api/events.mux", gotPath)
	}
}

func TestDSHRelayUpstreamRejectsNonStreamMethodForOpenStream(t *testing.T) {
	upstream := newDSHRelayUpstream(func() string { return "http://127.0.0.1:1" })
	if _, err := upstream.openStream(context.Background(), "session.list"); err == nil {
		t.Fatal("openStream accepted a unary method")
	}
}

func timeNowForRelayTest() time.Time { return time.Unix(0, 0) }
