package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// mutableRelayGate 是可在测试中切换的 Relay 订阅门禁。
type mutableRelayGate struct {
	mu sync.Mutex
	on bool
}

func (g *mutableRelayGate) allowed() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.on
}

func (g *mutableRelayGate) set(v bool) {
	g.mu.Lock()
	g.on = v
	g.mu.Unlock()
}

func TestRelayGateBlocksAndAllowsConnection(t *testing.T) {
	vector := loadRelayVector(t)
	connection := newFakeRelayHostConnection()
	gate := &mutableRelayGate{}
	host := newRelayHost(staticRelayIdentitySource{identity: vectorRelayHostIdentity(t, vector)}, staticRelayConnector{connection: connection}, &recordingRelayUpstream{})
	host.relayAllowed = gate.allowed

	// 未授权：serveOnce 直接拒绝，不建立连接，明确显示公网不可用。
	if err := host.serveOnce(context.Background()); !errors.Is(err, errRelayNotEntitled) {
		t.Fatalf("denied serveOnce error = %v, want errRelayNotEntitled", err)
	}
	if status := host.status(); status.State != relayOffline || !strings.Contains(status.Message, "公网不可用") {
		t.Fatalf("denied status = %+v, want offline with 公网不可用", status)
	}

	// 授权：正常上线。
	gate.set(true)
	done := make(chan error, 1)
	go func() { done <- host.serveOnce(context.Background()) }()
	waitForRelayState(t, host, relayOnline)
	connection.closeReceive()
	<-done
}

func TestRelayGateDenialKeepsMinBackoff(t *testing.T) {
	vector := loadRelayVector(t)
	clock := &manualRelayClock{}
	host := newRelayHost(staticRelayIdentitySource{identity: vectorRelayHostIdentity(t, vector)}, staticRelayConnector{connection: newFakeRelayHostConnection()}, &recordingRelayUpstream{})
	host.relayAllowed = func() bool { return false }
	host.reconnectMin = 100 * time.Millisecond
	host.reconnectMax = 800 * time.Millisecond
	host.sleepAfter = clock.sleepAfter

	host.start()
	// 门禁拒绝应保持最小退避，快速重查订阅状态。
	for i := 0; i < 3; i++ {
		waitForPendingWaits(t, clock, 1)
		clock.advance()
	}
	host.stop()

	delays := clock.snapshot()
	if len(delays) < 3 {
		t.Fatalf("recorded delays = %v, want >= 3", delays)
	}
	for i, d := range delays {
		if d != 100*time.Millisecond {
			t.Fatalf("delay[%d] = %v, want 100ms (gate denial must not grow backoff)", i, d)
		}
	}
}

func TestRelayDropClosesActiveConnection(t *testing.T) {
	vector := loadRelayVector(t)
	connection := newFakeRelayHostConnection()
	host := newRelayHost(staticRelayIdentitySource{identity: vectorRelayHostIdentity(t, vector)}, staticRelayConnector{connection: connection}, &recordingRelayUpstream{})
	host.relayAllowed = func() bool { return true }

	done := make(chan error, 1)
	go func() { done <- host.serveOnce(context.Background()) }()
	waitForRelayState(t, host, relayOnline)

	host.drop()
	<-done
	if status := host.status(); status.State != relayOffline {
		t.Fatalf("status after drop = %q, want offline", status.State)
	}
}

func TestSubscriptionChangePreservesPairingAndLAN(t *testing.T) {
	account, store := signedInAccountManager(t)
	setupServer := &fakeRemoteSetupServer{
		challenge: remoteSetupChallenge{ID: "challenge-1", Token: "single-use", ExpiresAt: time.Now().Add(5 * time.Minute)},
		result: remoteSetupResult{State: remoteSetupCompleted, Pairing: pairedDevice{
			PairingID:               "pairing-1",
			DeviceID:                "device-1",
			Name:                    "iPhone",
			DeviceIdentityPublicKey: bytes.Repeat([]byte{0x07}, 32),
		}},
	}
	remoteSetup := newRemoteSetupManager(account, setupServer, store, time.Now)
	if _, err := remoteSetup.start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := remoteSetup.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := len(remoteSetup.relayPairings()); n != 1 {
		t.Fatalf("paired relayPairings = %d, want 1", n)
	}

	stream := &fakeEntitlementStream{}
	entitlement := newEntitlementManager(stream, nil)
	remote := &remoteManager{enabled: true}
	relay := newRelayHost(
		accountRelayIdentitySource{account: account, pairings: remoteSetup},
		staticRelayConnector{connection: newFakeRelayHostConnection()},
		&recordingRelayUpstream{},
	)
	relay.relayAllowed = entitlement.relayAllowed
	app := &App{account: account, remoteSetup: remoteSetup, remote: remote, entitlement: entitlement, relay: relay}

	// 登录后订阅 entitlement，进入 active，Relay 门禁放行。
	entitlement.start()
	stream.push(entitlementUpdate{AccountID: "account-a", State: entitlementActive, ExpiresAt: time.Now().Add(time.Hour)})
	if got := app.EntitlementStatus().State; got != entitlementActive {
		t.Fatalf("active state = %q", got)
	}
	if !relay.relayAllowed() {
		t.Fatal("active subscription must allow Relay")
	}

	// 订阅变化：active → grace → expired → revoked。
	for _, st := range []entitlementState{entitlementGrace, entitlementExpired, entitlementRevoked} {
		stream.push(entitlementUpdate{AccountID: "account-a", State: st})
	}
	if got := app.EntitlementStatus().State; got != entitlementRevoked {
		t.Fatalf("final state = %q, want revoked", got)
	}
	if relay.relayAllowed() {
		t.Fatal("revoked subscription must not allow Relay")
	}

	// Subscription 变化不删除 Pairing，也不关闭免费 LAN。
	if devices := remoteSetup.status().Devices; len(devices) != 1 || devices[0].PairingID != "pairing-1" {
		t.Fatalf("subscription change dropped Pairing: %+v", devices)
	}
	if got := remoteSetup.relayPairings(); len(got) != 1 {
		t.Fatalf("relayPairings after subscription change = %d, want 1", len(got))
	}
	if !remote.status().Enabled {
		t.Fatal("subscription change closed free LAN")
	}
}

func TestAccountSignOutResetsEntitlement(t *testing.T) {
	account, store := signedInAccountManager(t)
	setupServer := &fakeRemoteSetupServer{
		challenge: remoteSetupChallenge{ID: "challenge-1", Token: "single-use", ExpiresAt: time.Now().Add(5 * time.Minute)},
		result: remoteSetupResult{State: remoteSetupCompleted, Pairing: pairedDevice{
			PairingID:               "pairing-1",
			DeviceID:                "device-1",
			Name:                    "iPhone",
			DeviceIdentityPublicKey: bytes.Repeat([]byte{0x07}, 32),
		}},
	}
	remoteSetup := newRemoteSetupManager(account, setupServer, store, time.Now)
	if _, err := remoteSetup.start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := remoteSetup.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	stream := &fakeEntitlementStream{}
	entitlement := newEntitlementManager(stream, nil)
	remote := &remoteManager{enabled: true}
	relay := newRelayHost(
		accountRelayIdentitySource{account: account, pairings: remoteSetup},
		staticRelayConnector{connection: newFakeRelayHostConnection()},
		&recordingRelayUpstream{},
	)
	relay.relayAllowed = entitlement.relayAllowed
	app := &App{account: account, remoteSetup: remoteSetup, remote: remote, entitlement: entitlement, relay: relay}

	entitlement.start()
	stream.push(entitlementUpdate{AccountID: "account-a", State: entitlementActive, ExpiresAt: time.Now().Add(time.Hour)})
	if got := app.EntitlementStatus().State; got != entitlementActive {
		t.Fatalf("active state = %q", got)
	}

	// Account 退出：entitlement 必须回到 unknown，绝不误报 active。
	if got := app.SignOutAccount().State; got != accountStateSignedOut {
		t.Fatalf("sign-out state = %q, want signed-out", got)
	}
	status := app.EntitlementStatus()
	if status.State != entitlementUnknown || status.RelayAllowed {
		t.Fatalf("entitlement after sign-out = %+v, want unknown not active", status)
	}

	// 退出不删除 Pairing，也不关闭免费 LAN。
	if got := remoteSetup.relayPairings(); len(got) != 1 {
		t.Fatalf("relayPairings after sign-out = %d, want 1", len(got))
	}
	if !remote.status().Enabled {
		t.Fatal("sign-out closed free LAN")
	}
}
