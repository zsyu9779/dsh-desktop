package main

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestHostConnectsAndForwardsOneAuthenticatedEncryptedAPIRequest(t *testing.T) {
	vector := loadRelayVector(t)
	connection := newFakeRelayHostConnection()
	upstream := &recordingRelayUpstream{response: []byte(`{"type":"server-response","rpcId":"rpc-vector-1","result":{"ok":true,"value":["session-1"]}}`)}
	host := newRelayHost(
		staticRelayIdentitySource{identity: vectorRelayHostIdentity(t, vector)},
		staticRelayConnector{connection: connection},
		upstream,
	)
	host.randomBytes = sequenceRelayRandom(decodeVectorValue(t, vector.HostNonce))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- host.serveOnce(ctx) }()
	waitForRelayState(t, host, relayOnline)

	channelID := "opaque-channel-1"
	connection.receive <- relayEvent{Type: relayChannelOpened, ChannelID: channelID}
	connection.receive <- relayEvent{
		Type:       relayCiphertext,
		ChannelID:  channelID,
		Ciphertext: vectorDeviceHello(t, vector, channelID),
	}
	hostHello := receiveRelayFrame(t, connection.send)
	deviceToHost, hostToDevice := vectorConnectionKeys(t, vector)
	if _, err := relayOpen(hostHello.Ciphertext, decodeVectorValue(t, vector.HostHelloKey), []byte(vector.HelloAAD)); err != nil {
		t.Fatalf("Host hello cannot be authenticated by Device: %v", err)
	}

	connection.receive <- relayEvent{
		Type:       relayCiphertext,
		ChannelID:  channelID,
		Ciphertext: decodeVectorValue(t, vector.RPCCiphertext),
	}
	responseFrame := receiveRelayFrame(t, connection.send)
	response, err := relayOpen(responseFrame.Ciphertext, hostToDevice, []byte(vector.HostToDeviceAAD))
	if err != nil {
		t.Fatal(err)
	}
	if string(response) != string(upstream.response) {
		t.Fatalf("response = %s, want %s", response, upstream.response)
	}
	if upstream.method != "session.get" || string(upstream.request) != string(decodeVectorValue(t, vector.RPCPlaintext)) {
		t.Fatalf("upstream = method %q request %s", upstream.method, upstream.request)
	}
	for _, secret := range [][]byte{[]byte("session.get"), []byte("session-secret")} {
		if containsBytes(hostHello.Ciphertext, secret) || containsBytes(responseFrame.Ciphertext, secret) {
			t.Fatalf("Relay-visible ciphertext exposed %q", secret)
		}
	}
	if len(deviceToHost) != 32 {
		t.Fatalf("device-to-Host connection key length = %d", len(deviceToHost))
	}

	cancel()
	connection.closeReceive()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Host Relay connection did not stop")
	}
	waitForRelayState(t, host, relayOffline)
}

func TestPairingCompletedWhileHostIsOnlineCanUseItsFirstRelayChannel(t *testing.T) {
	vector := loadRelayVector(t)
	identity := vectorRelayHostIdentity(t, vector)
	identity.Pairings = nil
	source := &mutableRelayIdentitySource{identity: identity}
	connection := newFakeRelayHostConnection()
	host := newRelayHost(source, staticRelayConnector{connection: connection}, &recordingRelayUpstream{})
	host.randomBytes = sequenceRelayRandom(decodeVectorValue(t, vector.HostNonce))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go host.serveOnce(ctx)
	waitForRelayState(t, host, relayOnline)

	source.set(vectorRelayHostIdentity(t, vector))
	connection.receive <- relayEvent{Type: relayChannelOpened, ChannelID: "opaque-channel-1"}
	connection.receive <- relayEvent{Type: relayCiphertext, ChannelID: "opaque-channel-1", Ciphertext: vectorDeviceHello(t, vector, "opaque-channel-1")}
	hostHello := receiveRelayFrame(t, connection.send)
	if _, err := relayOpen(hostHello.Ciphertext, decodeVectorValue(t, vector.HostHelloKey), []byte(vector.HelloAAD)); err != nil {
		t.Fatalf("new Pairing was not usable on existing Host connection: %v", err)
	}
}

func TestInvalidPairingTamperedHandshakeAndWrongRPCKeyNeverReachUpstream(t *testing.T) {
	vector := loadRelayVector(t)
	tests := []struct {
		name       string
		identity   relayHostIdentity
		ciphertext func(*testing.T, relayV1Vector, string) []byte
		afterHello bool
	}{
		{
			name: "invalid Pairing",
			identity: func() relayHostIdentity {
				identity := vectorRelayHostIdentity(t, vector)
				identity.Pairings[0].PairingID = "another-pairing"
				return identity
			}(),
			ciphertext: vectorDeviceHello,
		},
		{
			name:     "tampered handshake",
			identity: vectorRelayHostIdentity(t, vector),
			ciphertext: func(t *testing.T, vector relayV1Vector, channelID string) []byte {
				ciphertext := vectorDeviceHello(t, vector, channelID)
				ciphertext[len(ciphertext)-1] ^= 1
				return ciphertext
			},
		},
		{
			name:       "wrong RPC key",
			identity:   vectorRelayHostIdentity(t, vector),
			ciphertext: vectorDeviceHello,
			afterHello: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			connection := newFakeRelayHostConnection()
			upstream := &recordingRelayUpstream{}
			host := newRelayHost(staticRelayIdentitySource{identity: tt.identity}, staticRelayConnector{connection: connection}, upstream)
			host.randomBytes = sequenceRelayRandom(decodeVectorValue(t, vector.HostNonce))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go host.serveOnce(ctx)
			waitForRelayState(t, host, relayOnline)
			channelID := "opaque-channel-1"
			connection.receive <- relayEvent{Type: relayChannelOpened, ChannelID: channelID}
			connection.receive <- relayEvent{Type: relayCiphertext, ChannelID: channelID, Ciphertext: tt.ciphertext(t, vector, channelID)}
			if tt.afterHello {
				_ = receiveRelayFrame(t, connection.send)
				wrong := append([]byte(nil), decodeVectorValue(t, vector.RPCCiphertext)...)
				wrong[len(wrong)-1] ^= 1
				connection.receive <- relayEvent{Type: relayCiphertext, ChannelID: channelID, Ciphertext: wrong}
			}
			closed := receiveRelayFrame(t, connection.send)
			if closed.ChannelID != channelID || len(closed.Ciphertext) != 0 {
				t.Fatalf("rejection frame = %+v, want channel-scoped close", closed)
			}
			if upstream.calls() != 0 {
				t.Fatalf("invalid traffic reached upstream %d time(s)", upstream.calls())
			}
		})
	}
}

func TestRelayStopClosesTheActiveConnectionBeforeReturning(t *testing.T) {
	vector := loadRelayVector(t)
	connection := newFakeRelayHostConnection()
	host := newRelayHost(staticRelayIdentitySource{identity: vectorRelayHostIdentity(t, vector)}, staticRelayConnector{connection: connection}, &recordingRelayUpstream{})
	host.start()
	waitForRelayState(t, host, relayOnline)

	host.stop()

	waitForRelayState(t, host, relayOffline)
	if _, open := <-connection.receive; open {
		t.Fatal("active Relay connection remained readable after stop")
	}
}

func TestAccountSignOutClosesRelayBeforeCredentialIsCleared(t *testing.T) {
	vector := loadRelayVector(t)
	connection := newFakeRelayHostConnection()
	host := newRelayHost(staticRelayIdentitySource{identity: vectorRelayHostIdentity(t, vector)}, staticRelayConnector{connection: connection}, &recordingRelayUpstream{})
	store := newMemorySecretStore()
	account := newAccountManager(fakeAccountAuthorizer{}, &fakeAccountServer{}, store)
	encoded, _ := json.Marshal(validAccountCredential())
	store.values[accountCredentialSecret] = string(encoded)
	app := &App{account: account, relay: host}
	host.start()
	waitForRelayState(t, host, relayOnline)

	status := app.SignOutAccount()

	if status.State != accountStateSignedOut {
		t.Fatalf("Account status = %+v", status)
	}
	waitForRelayState(t, host, relayOffline)
	if _, open := <-connection.receive; open {
		t.Fatal("Relay remained active after Account sign-out")
	}
}

func TestRelayReportsOfflineWhenConnectionDrops(t *testing.T) {
	vector := loadRelayVector(t)
	connection := newFakeRelayHostConnection()
	host := newRelayHost(staticRelayIdentitySource{identity: vectorRelayHostIdentity(t, vector)}, staticRelayConnector{connection: connection}, &recordingRelayUpstream{})
	done := make(chan error, 1)
	go func() { done <- host.serveOnce(context.Background()) }()
	waitForRelayState(t, host, relayOnline)
	connection.closeReceive()
	if err := <-done; err == nil {
		t.Fatal("disconnect error = nil")
	}
	waitForRelayState(t, host, relayOffline)
}

func vectorRelayHostIdentity(t *testing.T, vector relayV1Vector) relayHostIdentity {
	t.Helper()
	return relayHostIdentity{
		AccountID:   "account-1",
		AccessToken: "account-token",
		HostID:      vector.HostID,
		PrivateKey:  decodeVectorValue(t, vector.HostPrivateKey),
		Pairings: []relayPairing{{
			PairingID:       vector.PairingID,
			DeviceID:        vector.DeviceID,
			DevicePublicKey: decodeVectorValue(t, vector.DevicePublicKey),
		}},
	}
}

func vectorDeviceHello(t *testing.T, vector relayV1Vector, channelID string) []byte {
	t.Helper()
	plaintext, err := json.Marshal(map[string]any{
		"version":            1,
		"type":               "device_hello",
		"ephemeralPublicKey": decodeVectorValue(t, vector.EphemeralPublicKey),
		"deviceNonce":        decodeVectorValue(t, vector.DeviceNonce),
	})
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := relaySealWithNonce(
		plaintext,
		decodeVectorValue(t, vector.DeviceHelloKey),
		[]byte(relayV1+"/hello/"+channelID),
		make([]byte, 12),
	)
	if err != nil {
		t.Fatal(err)
	}
	return ciphertext
}

func vectorConnectionKeys(t *testing.T, vector relayV1Vector) ([]byte, []byte) {
	t.Helper()
	return decodeVectorValue(t, vector.DeviceToHostKey), decodeVectorValue(t, vector.HostToDeviceKey)
}

func sequenceRelayRandom(values ...[]byte) func(int) ([]byte, error) {
	var mu sync.Mutex
	return func(size int) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		if len(values) == 0 || len(values[0]) != size {
			return nil, errors.New("unexpected random request")
		}
		value := append([]byte(nil), values[0]...)
		values = values[1:]
		return value, nil
	}
}

type staticRelayIdentitySource struct{ identity relayHostIdentity }

func (s staticRelayIdentitySource) relayIdentity() (relayHostIdentity, error) { return s.identity, nil }

type mutableRelayIdentitySource struct {
	mu       sync.Mutex
	identity relayHostIdentity
}

func (s *mutableRelayIdentitySource) relayIdentity() (relayHostIdentity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.identity, nil
}

func (s *mutableRelayIdentitySource) set(identity relayHostIdentity) {
	s.mu.Lock()
	s.identity = identity
	s.mu.Unlock()
}

type staticRelayConnector struct{ connection relayHostConnection }

func (s staticRelayConnector) connect(context.Context, relayHostEndpoint) (relayHostConnection, error) {
	return s.connection, nil
}

type fakeRelayHostConnection struct {
	receive chan relayEvent
	send    chan relayFrame
	once    sync.Once
}

func newFakeRelayHostConnection() *fakeRelayHostConnection {
	return &fakeRelayHostConnection{receive: make(chan relayEvent, 8), send: make(chan relayFrame, 8)}
}

func (c *fakeRelayHostConnection) receiveEvent() (relayEvent, error) {
	event, ok := <-c.receive
	if !ok {
		return relayEvent{}, errors.New("Relay disconnected")
	}
	return event, nil
}
func (c *fakeRelayHostConnection) sendFrame(frame relayFrame) error { c.send <- frame; return nil }
func (c *fakeRelayHostConnection) close() error                     { c.closeReceive(); return nil }
func (c *fakeRelayHostConnection) closeReceive()                    { c.once.Do(func() { close(c.receive) }) }

type recordingRelayUpstream struct {
	mu       sync.Mutex
	method   string
	request  []byte
	response []byte
	count    int
}

func (u *recordingRelayUpstream) call(_ context.Context, method string, request []byte) ([]byte, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.count++
	u.method = method
	u.request = append([]byte(nil), request...)
	return append([]byte(nil), u.response...), nil
}
func (u *recordingRelayUpstream) calls() int { u.mu.Lock(); defer u.mu.Unlock(); return u.count }

func waitForRelayState(t *testing.T, host *relayHost, want relayConnectionState) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if host.status().State == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("Relay state = %q, want %q", host.status().State, want)
}

func receiveRelayFrame(t *testing.T, frames <-chan relayFrame) relayFrame {
	t.Helper()
	select {
	case frame := <-frames:
		return frame
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Host Relay frame")
		return relayFrame{}
	}
}

func containsBytes(value, part []byte) bool {
	for index := 0; index+len(part) <= len(value); index++ {
		if string(value[index:index+len(part)]) == string(part) {
			return true
		}
	}
	return false
}
